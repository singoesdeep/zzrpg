package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/singoesdeep/zzrpg/backend/internal/auth"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/combat"
	"github.com/singoesdeep/zzrpg/backend/internal/database"
	"github.com/singoesdeep/zzrpg/backend/internal/events"
	"github.com/singoesdeep/zzrpg/backend/internal/inventory"
	"github.com/singoesdeep/zzrpg/backend/internal/items"
	"github.com/singoesdeep/zzrpg/backend/internal/quests"
	"github.com/singoesdeep/zzrpg/backend/internal/socket"
	"github.com/singoesdeep/zzrpg/backend/internal/statclient"
	"github.com/singoesdeep/zzrpg/backend/pkg/config"
	"github.com/singoesdeep/zzrpg/backend/pkg/logger"
)

//go:embed api/*
var apiFS embed.FS

func main() {
	// 1. Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		panic("Failed to load configuration: " + err.Error())
	}

	// 2. Initialize logger
	log := logger.NewLogger(cfg.Env)
	log.Info("Starting zzrpg backend...", "env", cfg.Env, "port", cfg.Port)

	// 3. Initialize database connection pool
	db, err := database.NewConnectionPool(cfg, log)
	if err != nil {
		log.Error("Failed to initialize database connection pool", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Run database migrations
	if err := db.RunMigrations(context.Background()); err != nil {
		log.Error("Failed to run database migrations", "error", err)
		os.Exit(1)
	}

	// 4. Initialize Auth components
	userRepo := auth.NewUserRepository(db.Pool)
	authService := auth.NewAuthService(userRepo, cfg.JWTSecret)

	// Initialize statclient gRPC connection
	statClient, err := statclient.NewClient(cfg.ZzstatGRPCURL)
	if err != nil {
		log.Warn("Failed to connect to Rust zzstat service. Stat calculations will use fallback.", "error", err)
	} else {
		log.Info("Successfully initialized gRPC statclient connecting to Rust zzstat service")
		// Close connection on server shutdown
		defer func() {
			if err := statClient.Close(); err != nil {
				log.Error("Failed to close gRPC statclient connection", "error", err)
			}
		}()
	}

	// Initialize Character components (inject statClient, pass nil for equipProvider temporarily)
	charRepo := character.NewCharacterRepository(db.Pool)
	charService := character.NewCharacterService(charRepo, statClient, nil)

	// Initialize Item/Equipment definitions components
	itemRepo := items.NewItemRepository(db.Pool)
	itemService := items.NewItemService(itemRepo)

	// Initialize Inventory/Equipment components
	invRepo := inventory.NewInventoryRepository(db.Pool)
	invService := inventory.NewInventoryService(invRepo, charService, events.Global())

	// Resolve circular startup reference by setting equipment provider
	charService.SetEquipmentProvider(invService)

	// Initialize Quest components
	questRepo := quests.NewQuestRepository(db.Pool)
	questService := quests.NewQuestService(questRepo, charService, invService)

	// Initialize WebSocket components
	hub := socket.NewHub()
	go hub.Run()

	// Initialize Combat components
	combatService := combat.NewCombatService(charService, statClient, socket.GetRegistry(), questService)

	wsMsgHandler := func(client *socket.Client, msg socket.WSMessage) {
		switch msg.Type {
		case "CHAT":
			var payload socket.ChatPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				return
			}
			broadMsg, _ := json.Marshal(map[string]interface{}{
				"type": "CHAT",
				"payload": map[string]interface{}{
					"username": client.Username,
					"message":  payload.Message,
				},
			})
			hub.Broadcast <- broadMsg

		case "SELECT_CHARACTER":
			var payload socket.SelectCharPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				return
			}
			hub.AssociateCharacter(client, payload.CharacterID)

			// Start active in-memory combat session for health tracking
			char, err := charService.GetByID(context.Background(), payload.CharacterID)
			if err == nil {
				socket.GetRegistry().StartSession(payload.CharacterID, char.Stats.DerivedStats["HP"], char.Stats.DerivedStats["MP"])
			}

			ack, _ := json.Marshal(map[string]interface{}{
				"type": "SELECT_CHARACTER_ACK",
				"payload": map[string]interface{}{
					"character_id": payload.CharacterID,
					"status":       "ACTIVE",
				},
			})
			client.Send <- ack

		case "COMBAT_ATTACK":
			if client.CharacterID == 0 {
				errAck, _ := json.Marshal(map[string]interface{}{
					"type": "COMBAT_ERROR",
					"payload": map[string]interface{}{
						"message": "no character selected",
					},
				})
				client.Send <- errAck
				return
			}

			var payload combat.AttackRequest
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				return
			}
			payload.AttackerID = client.CharacterID

			res, err := combatService.ExecuteAttack(context.Background(), payload)
			if err != nil {
				errAck, _ := json.Marshal(map[string]interface{}{
					"type": "COMBAT_ERROR",
					"payload": map[string]interface{}{
						"message": err.Error(),
					},
				})
				client.Send <- errAck
				return
			}

			broadMsg, _ := json.Marshal(map[string]interface{}{
				"type":    "COMBAT_DAMAGE",
				"payload": res,
			})
			hub.Broadcast <- broadMsg
		}
	}

	// Subscribe to inventory events for stat recalculations
	events.Global().Subscribe(events.EventItemEquipped, func(ctx context.Context, ev events.Event) {
		payload, ok := ev.Payload.(inventory.EquippedItemEventPayload)
		if !ok {
			return
		}
		log.Info("Item equipped event received, triggering stat recalculation", "character_id", payload.CharacterID)
		if err := charService.RecalculateStats(ctx, int64(payload.CharacterID)); err != nil {
			log.Error("Failed to recalculate stats on equip", "character_id", payload.CharacterID, "error", err)
		}
	})

	events.Global().Subscribe(events.EventItemUnequipped, func(ctx context.Context, ev events.Event) {
		payload, ok := ev.Payload.(inventory.EquippedItemEventPayload)
		if !ok {
			return
		}
		log.Info("Item unequipped event received, triggering stat recalculation", "character_id", payload.CharacterID)
		if err := charService.RecalculateStats(ctx, int64(payload.CharacterID)); err != nil {
			log.Error("Failed to recalculate stats on unequip", "character_id", payload.CharacterID, "error", err)
		}
	})

	// 5. Setup multiplexer / router
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Check database health
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := db.Pool.Ping(ctx); err != nil {
			log.Error("Healthcheck failed: postgres is unreachable", "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"DOWN", "database":"UNREACHABLE"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP", "database":"OK"}`))
	})

	// Auth Endpoints
	mux.HandleFunc("/api/v1/auth/register", auth.RegisterHandler(authService))
	mux.HandleFunc("/api/v1/auth/login", auth.LoginHandler(authService))

	// Protected test endpoint
	mux.Handle("/api/v1/auth/me", auth.AuthMiddleware(cfg.JWTSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		userID := auth.UserIDFromContext(r.Context())
		username := auth.UsernameFromContext(r.Context())

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"user_id":  userID,
				"username": username,
			},
		})
	})))

	// Character Endpoints (Protected by JWT)
	mux.Handle("POST /api/v1/characters", auth.AuthMiddleware(cfg.JWTSecret)(character.CreateHandler(charService)))
	mux.Handle("GET /api/v1/characters", auth.AuthMiddleware(cfg.JWTSecret)(character.ListHandler(charService)))
	mux.Handle("GET /api/v1/characters/{id}", auth.AuthMiddleware(cfg.JWTSecret)(character.GetHandler(charService)))
	mux.Handle("GET /api/v1/characters/{id}/stats", auth.AuthMiddleware(cfg.JWTSecret)(character.GetStatsHandler(charService)))

	// Item Admin Endpoints (Protected by JWT)
	mux.Handle("POST /api/v1/admin/items", auth.AuthMiddleware(cfg.JWTSecret)(items.CreateHandler(itemService)))
	mux.Handle("PUT /api/v1/admin/items/{id}", auth.AuthMiddleware(cfg.JWTSecret)(items.UpdateHandler(itemService)))
	mux.Handle("GET /api/v1/admin/items", auth.AuthMiddleware(cfg.JWTSecret)(items.ListHandler(itemService)))
	mux.Handle("GET /api/v1/admin/items/{id}", auth.AuthMiddleware(cfg.JWTSecret)(items.GetHandler(itemService)))
	mux.Handle("DELETE /api/v1/admin/items/{id}", auth.AuthMiddleware(cfg.JWTSecret)(items.DeleteHandler(itemService)))

	// Inventory Endpoints (Protected by JWT)
	mux.Handle("GET /api/v1/characters/{id}/inventory", auth.AuthMiddleware(cfg.JWTSecret)(inventory.GetInventoryHandler(invService)))
	mux.Handle("POST /api/v1/inventory/move", auth.AuthMiddleware(cfg.JWTSecret)(inventory.MoveItemHandler(invService)))
	mux.Handle("POST /api/v1/admin/inventory/add", auth.AuthMiddleware(cfg.JWTSecret)(inventory.AddAdminItemHandler(invService)))

	// Quest Endpoints (Protected by JWT)
	mux.Handle("POST /api/v1/admin/quests", auth.AuthMiddleware(cfg.JWTSecret)(quests.CreateDefinitionHandler(questService)))
	mux.Handle("GET /api/v1/quests", auth.AuthMiddleware(cfg.JWTSecret)(quests.ListDefinitionsHandler(questService)))
	mux.Handle("POST /api/v1/characters/{id}/quests/accept", auth.AuthMiddleware(cfg.JWTSecret)(quests.AcceptQuestHandler(questService)))
	mux.Handle("GET /api/v1/characters/{id}/quests", auth.AuthMiddleware(cfg.JWTSecret)(quests.GetQuestLogHandler(questService)))
	mux.Handle("POST /api/v1/admin/quests/progress", auth.AuthMiddleware(cfg.JWTSecret)(quests.UpdateQuestProgressHandler(questService)))

	// WebSocket Endpoint
	mux.HandleFunc("/ws", socket.ServeWS(hub, cfg.JWTSecret, wsMsgHandler))

	// Swagger API Docs routes
	mux.HandleFunc("GET /api/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		data, err := apiFS.ReadFile("api/openapi.json")
		if err != nil {
			log.Error("Failed to read openapi.json", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	})

	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		data, err := apiFS.ReadFile("api/docs.html")
		if err != nil {
			log.Error("Failed to read docs.html", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	})

	// 5. Initialize Server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 6. Run server in background
	go func() {
		log.Info("HTTP server listening", "address", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("HTTP server failed to listen", "error", err)
			os.Exit(1)
		}
	}()

	// 7. Wait for interrupt signal to gracefully shut down the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("Shutdown signal received, shutting down server...")

	// The context is used to inform the server it has 10 seconds to finish
	// the request it is currently handling
	ctx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("Server forced to shutdown", "error", err)
	}

	log.Info("zzrpg backend stopped gracefully")
}
