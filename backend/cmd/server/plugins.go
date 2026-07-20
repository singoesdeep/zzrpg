package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/singoesdeep/zzrpg/backend/content"
	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/eventlog"
	"github.com/singoesdeep/zzrpg/backend/engine/eventstream"
	"github.com/singoesdeep/zzrpg/backend/engine/outbox"
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/engine/store"
	"github.com/singoesdeep/zzrpg/backend/pkg/metrics"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/singoesdeep/zzrpg/backend/internal/auth"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/combat"
	"github.com/singoesdeep/zzrpg/backend/internal/database"
	"github.com/singoesdeep/zzrpg/backend/internal/idle"
	"github.com/singoesdeep/zzrpg/backend/internal/inventory"
	"github.com/singoesdeep/zzrpg/backend/internal/items"
	"github.com/singoesdeep/zzrpg/backend/internal/killreward"
	"github.com/singoesdeep/zzrpg/backend/internal/loot"
	"github.com/singoesdeep/zzrpg/backend/internal/quests"
	"github.com/singoesdeep/zzrpg/backend/internal/session"
	"github.com/singoesdeep/zzrpg/backend/internal/skills"
	"github.com/singoesdeep/zzrpg/backend/internal/socket"
	"github.com/singoesdeep/zzrpg/backend/internal/statclient"
	"github.com/singoesdeep/zzrpg/backend/pkg/cache"
)

// readyStr renders a dependency's reachability for the readiness payload.
func readyStr(ok bool) string {
	if ok {
		return "up"
	}
	return "down"
}

// nodeID returns a stable-per-process identifier for this node, used to tag and
// de-duplicate events on the cross-node stream.
func nodeID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "node"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

// skillResolver adapts the skills service to combat.SkillResolver (consumer-owned
// interface), mapping a skill definition to the effect combat applies.
type skillResolver struct{ svc *skills.Service }

func (a skillResolver) Resolve(id string) (combat.SkillEffect, bool) {
	d, ok := a.svc.Resolve(id)
	if !ok {
		return combat.SkillEffect{}, false
	}
	return combat.SkillEffect{
		Multiplier: d.Multiplier,
		FlatDamage: d.FlatDamage,
		ManaCost:   d.ManaCost,
		ClassReq:   d.Class,
	}, true
}

// statHolder wraps the embedded stat client so it can live in the registry even
// when it is nil (the client fails to load and callers fall back). Storing a
// possibly-nil interface directly in the registry would make type-assertion on
// resolve ambiguous, so we box it.
type statHolder struct {
	client statclient.Client
}

// adminOnly composes JWT auth with an admin-role check for mutating
// administrative endpoints. Read-only catalog listings stay under plain JWT.
func adminOnly(jwtSecret string, h http.Handler) http.Handler {
	return auth.AuthMiddleware(jwtSecret)(auth.RequireAdmin(h))
}

// ---------------------------------------------------------------------------
// core: infrastructure (db, cache, stat client, hub, WS + docs routes)
// ---------------------------------------------------------------------------

type corePlugin struct {
	db              *database.DB
	cache           cache.Cache
	closeCache      func() error
	stat            *statHolder
	hub             *socket.Hub
	router          *socket.MessageRouter
	sessionReg      *session.Registry
	outboxRelay     *outbox.Relay
	outboxRetention time.Duration
	eventConsumer   *eventstream.Consumer
	closeStream     func() error
}

func (p *corePlugin) Meta() plugin.Meta { return plugin.Meta{Name: "core"} }

func (p *corePlugin) Init(ic plugin.InitContext) error {
	cfg := ic.Config()
	log := ic.Logger()
	reg := ic.Registry()
	mux := ic.Mux()
	ctx := ic.Context()

	db, err := database.NewConnectionPool(cfg, log)
	if err != nil {
		return fmt.Errorf("database connection pool: %w", err)
	}
	p.db = db
	if err := db.RunMigrations(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	// zzstat is the single source of truth for all stat/combat math — there is no
	// Go fallback — so it is a hard dependency: refuse to start without it.
	statClient, err := statclient.NewClient(cfg.ZzstatGRPCURL)
	if err != nil {
		return fmt.Errorf("load embedded Rust zzstat library (required for stat/combat math): %w", err)
	}
	log.Info("Successfully initialized embedded statclient loading Rust zzstat shared library")
	p.stat = &statHolder{client: statClient}

	// Cache (Redis) with graceful degradation: if Redis is unreachable the app
	// still runs, going straight to the database.
	var appCache cache.Cache = cache.Noop{}
	if c, closeCache, err := cache.NewRedis(ctx, cfg.RedisURL); err != nil {
		log.Warn("Redis unavailable; caching disabled (falling back to direct DB reads)", "error", err)
	} else {
		log.Info("Connected to Redis for caching", "url", cfg.RedisURL)
		appCache = c
		p.closeCache = closeCache
	}
	p.cache = appCache

	p.hub = socket.NewHub()
	p.hub.SetEventBus(ic.Bus())
	p.router = socket.NewMessageRouter()
	p.sessionReg = session.NewRegistry()

	// Transactional outbox relay: dispatches events written in-tx (e.g. reward
	// grants) onto the bus after commit.
	p.outboxRelay = outbox.NewRelay(p.db.Store, ic.Bus(), log)
	p.outboxRetention = cfg.OutboxRetention

	// Register every domain's event decoders on the shared registry, used by both
	// the outbox relay and the cross-node event stream to rebuild typed events.
	decoders := p.outboxRelay.Registry()
	character.RegisterEventDecoders(decoders)
	combat.RegisterEventDecoders(decoders)
	quests.RegisterEventDecoders(decoders)
	inventory.RegisterEventDecoders(decoders)
	loot.RegisterEventDecoders(decoders)
	if err := registry.Provide(reg, "eventDecoders", decoders); err != nil {
		return err
	}

	p.registerOutboxMetrics(reg)
	p.setupEventStream(ic, decoders)

	if err := registry.Provide(reg, "db", p.db); err != nil {
		return err
	}
	if err := registry.Provide(reg, "session", p.sessionReg); err != nil {
		return err
	}
	if err := registry.Provide(reg, "cache", p.cache); err != nil {
		return err
	}
	if err := registry.Provide(reg, "stat", p.stat); err != nil {
		return err
	}
	if err := registry.Provide(reg, "hub", p.hub); err != nil {
		return err
	}
	if err := registry.Provide(reg, "msgRouter", p.router); err != nil {
		return err
	}

	p.registerHTTPEndpoints(mux, log)
	p.registerWebSocket(mux, reg, cfg.JWTSecret)

	return nil
}

func (p *corePlugin) registerOutboxMetrics(reg *registry.Registry) {
	if m, err := registry.Resolve[*metrics.Metrics](reg, "metrics"); err == nil {
		m.Registerer().MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "outbox_undispatched",
			Help: "Number of outbox rows written but not yet dispatched by the relay.",
		}, func() float64 {
			qctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var n float64
			_ = p.db.Pool.QueryRow(qctx, `SELECT count(*) FROM outbox WHERE published_at IS NULL`).Scan(&n)
			return n
		}))
	}
}

func (p *corePlugin) setupEventStream(ic plugin.InitContext, decoders *outbox.Registry) {
	cfg := ic.Config()
	log := ic.Logger()
	ctx := ic.Context()
	nid := nodeID()
	if streamClient, err := eventstream.Dial(ctx, cfg.RedisURL); err != nil {
		log.Warn("Cross-node event streaming disabled; running single-node", "error", err)
	} else if fb, ok := ic.Bus().(*bus.Fanout); ok {
		pub := eventstream.NewPublisher(streamClient, "", nid)
		fb.SetForwarder(func(fctx context.Context, ev bus.Event) {
			if err := pub.Publish(fctx, ev); err != nil {
				log.Error("event fan-out publish failed", "event", ev.Name(), "error", err)
			}
		})
		p.eventConsumer = eventstream.NewConsumer(streamClient, fb.PublishLocal, decoders, "", nid, log)
		p.closeStream = streamClient.Close
		log.Info("Cross-node event streaming enabled", "node", nid)
	} else {
		_ = streamClient.Close()
		log.Warn("Kernel bus is not a Fanout; cross-node streaming disabled")
	}
}

func (p *corePlugin) registerHTTPEndpoints(mux *http.ServeMux, log *slog.Logger) {
	// Health check.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := p.db.Pool.Ping(ctx); err != nil {
			log.Error("Healthcheck failed: postgres is unreachable", "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"DOWN", "database":"UNREACHABLE"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP", "database":"OK"}`))
	})

	// Readiness probe.
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		dbReady := p.db.Pool.Ping(ctx) == nil
		redisReady := p.cache.Ping(ctx) == nil

		status := http.StatusOK
		if !dbReady {
			status = http.StatusServiceUnavailable
		}
		body, _ := json.Marshal(map[string]interface{}{
			"ready":    dbReady,
			"database": readyStr(dbReady),
			"redis":    readyStr(redisReady),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})

	// Swagger API docs.
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
}

func (p *corePlugin) registerWebSocket(mux *http.ServeMux, reg *registry.Registry, jwtSecret string) {
	p.router.Handle("CHAT", func(client *socket.Client, msg socket.WSMessage) {
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
		p.hub.Broadcast <- broadMsg
	})

	disconnect := func(client *socket.Client) {
		if client.CharacterID > 0 {
			if cs, err := registry.Resolve[character.CharacterService](reg, "character"); err == nil {
				_ = cs.UpdateLastActive(context.Background(), client.CharacterID)
			}
			p.sessionReg.EndSession(client.CharacterID)
		}
	}
	mux.HandleFunc("/ws", socket.ServeWS(p.hub, jwtSecret, p.router.Dispatch, disconnect))
}

func (p *corePlugin) Start(rc plugin.RunContext) error {
	go p.hub.Run()
	go p.outboxRelay.Run(rc.Context(), time.Second)
	go p.outboxRelay.RunPruner(rc.Context(), 10*time.Minute, p.outboxRetention)
	if p.eventConsumer != nil {
		go p.eventConsumer.Run(rc.Context())
	}
	return nil
}

func (p *corePlugin) Stop(ctx context.Context) error {
	if p.closeStream != nil {
		_ = p.closeStream()
	}
	if p.closeCache != nil {
		_ = p.closeCache()
	}
	if p.stat != nil && p.stat.client != nil {
		if err := p.stat.client.Close(); err != nil {
			return fmt.Errorf("close statclient: %w", err)
		}
	}
	if p.db != nil {
		p.db.Close()
	}
	return nil
}

// ---------------------------------------------------------------------------
// auth
// ---------------------------------------------------------------------------

type authPlugin struct{ plugin.Base }

func (authPlugin) Meta() plugin.Meta { return plugin.Meta{Name: "auth", Requires: []string{"core"}} }

func (authPlugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	mux := ic.Mux()
	cfg := ic.Config()

	db := registry.MustResolve[*database.DB](reg, "db")
	userRepo := auth.NewUserRepository(db.Store)
	authService := auth.NewAuthService(userRepo, cfg.JWTSecret,
		auth.WithRefreshStore(auth.NewPgRefreshStore(db.Store)),
		auth.WithTokenTTLs(cfg.AccessTokenTTL, cfg.RefreshTokenTTL),
	)

	mux.HandleFunc("/api/v1/auth/register", auth.RegisterHandler(authService))
	mux.HandleFunc("/api/v1/auth/login", auth.LoginHandler(authService))
	mux.HandleFunc("/api/v1/auth/refresh", auth.RefreshHandler(authService))
	mux.HandleFunc("/api/v1/auth/logout", auth.LogoutHandler(authService))
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

	return nil
}

// ---------------------------------------------------------------------------
// items
// ---------------------------------------------------------------------------

type itemsPlugin struct{ plugin.Base }

func (itemsPlugin) Meta() plugin.Meta { return plugin.Meta{Name: "items", Requires: []string{"core"}} }

func (itemsPlugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	mux := ic.Mux()
	jwt := ic.Config().JWTSecret

	db := registry.MustResolve[*database.DB](reg, "db")
	itemRepo := items.NewItemRepository(db.Store)
	itemService := items.NewItemService(itemRepo)

	mux.Handle("POST /api/v1/admin/items", adminOnly(jwt, items.CreateHandler(itemService)))
	mux.Handle("PUT /api/v1/admin/items/{id}", adminOnly(jwt, items.UpdateHandler(itemService)))
	mux.Handle("GET /api/v1/admin/items", auth.AuthMiddleware(jwt)(items.ListHandler(itemService)))
	mux.Handle("GET /api/v1/admin/items/{id}", auth.AuthMiddleware(jwt)(items.GetHandler(itemService)))
	mux.Handle("DELETE /api/v1/admin/items/{id}", adminOnly(jwt, items.DeleteHandler(itemService)))

	return nil
}

// ---------------------------------------------------------------------------
// character (owns SELECT_CHARACTER + offline gains, stat-recalc subscriptions)
// ---------------------------------------------------------------------------

type characterPlugin struct {
	charService character.CharacterService
	hub         *socket.Hub
	eventBus    bus.EventBus
	sessionReg  *session.Registry
	store       store.Store
	decoders    *outbox.Registry
}

func (p *characterPlugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "character", Requires: []string{"core"}}
}

func (p *characterPlugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	mux := ic.Mux()
	cfg := ic.Config()
	log := ic.Logger()

	db := registry.MustResolve[*database.DB](reg, "db")
	stat := registry.MustResolve[*statHolder](reg, "stat")
	p.sessionReg = registry.MustResolve[*session.Registry](reg, "session")
	p.store = db.Store
	p.decoders = registry.MustResolve[*outbox.Registry](reg, "eventDecoders")

	p.eventBus = ic.Bus()
	charRepo := character.NewCharacterRepository(db.Store)
	p.charService = character.NewCharacterService(charRepo, stat.client, nil, ic.Bus(), ic.Hooks())
	if err := registry.Provide(reg, "character", p.charService); err != nil {
		return err
	}

	p.hub = registry.MustResolve[*socket.Hub](reg, "hub")
	router := registry.MustResolve[*socket.MessageRouter](reg, "msgRouter")
	router.Handle("SELECT_CHARACTER", p.handleSelectCharacter)

	// Character endpoints (protected by JWT).
	mux.Handle("POST /api/v1/characters", auth.AuthMiddleware(cfg.JWTSecret)(character.CreateHandler(p.charService)))
	mux.Handle("GET /api/v1/characters", auth.AuthMiddleware(cfg.JWTSecret)(character.ListHandler(p.charService)))
	mux.Handle("GET /api/v1/characters/{id}", auth.AuthMiddleware(cfg.JWTSecret)(character.GetHandler(p.charService)))
	mux.Handle("GET /api/v1/characters/{id}/stats", auth.AuthMiddleware(cfg.JWTSecret)(character.GetStatsHandler(p.charService)))

	// Stat recalculation on equip/unequip.
	eventBus := ic.Bus()
	eventBus.Subscribe(inventory.EventItemEquipped, func(ctx context.Context, ev bus.Event) {
		e, ok := ev.(inventory.ItemEquipped)
		if !ok {
			return
		}
		log.Info("Item equipped event received, triggering stat recalculation", "character_id", e.CharacterID)
		if err := p.charService.RecalculateStats(ctx, int64(e.CharacterID)); err != nil {
			log.Error("Failed to recalculate stats on equip", "character_id", e.CharacterID, "error", err)
		}
	})
	eventBus.Subscribe(inventory.EventItemUnequipped, func(ctx context.Context, ev bus.Event) {
		e, ok := ev.(inventory.ItemUnequipped)
		if !ok {
			return
		}
		log.Info("Item unequipped event received, triggering stat recalculation", "character_id", e.CharacterID)
		if err := p.charService.RecalculateStats(ctx, int64(e.CharacterID)); err != nil {
			log.Error("Failed to recalculate stats on unequip", "character_id", e.CharacterID, "error", err)
		}
	})

	return nil
}

func (p *characterPlugin) Start(plugin.RunContext) error { return nil }

func (p *characterPlugin) Stop(context.Context) error { return nil }

// handleSelectCharacter starts an in-memory combat session, announces login via
// domain event, and acknowledges the selection.
func (p *characterPlugin) handleSelectCharacter(client *socket.Client, msg socket.WSMessage) {
	var payload socket.SelectCharPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return
	}
	p.hub.AssociateCharacter(client, payload.CharacterID)

	// Start active in-memory combat session for health tracking.
	char, err := p.charService.GetByID(context.Background(), payload.CharacterID)
	if err == nil {
		p.sessionReg.StartSession(payload.CharacterID, char.Stats.DerivedStats["HP"], char.Stats.DerivedStats["MP"])

		if p.eventBus != nil {
			_ = p.eventBus.Publish(context.Background(), character.CharacterLoggedIn{
				CharacterID:  payload.CharacterID,
				LastActiveAt: char.LastActiveAt,
			})
		}

		// Replay the character's history since it was last active so the
		// reconnecting client can catch up on what happened while away (e.g. the
		// offline rewards just granted above are recorded in the event_log).
		if p.decoders != nil {
			recorded, rerr := eventlog.Replay(context.Background(), p.store, p.decoders,
				eventlog.CharacterStream(payload.CharacterID), char.LastActiveAt)
			if rerr == nil && len(recorded) > 0 {
				events := make([]map[string]interface{}, 0, len(recorded))
				for _, r := range recorded {
					events = append(events, map[string]interface{}{
						"type":        r.Event.Name(),
						"occurred_at": r.OccurredAt,
						"payload":     r.Event,
					})
				}
				awayPkt, _ := json.Marshal(map[string]interface{}{
					"type":    "AWAY_EVENTS",
					"payload": map[string]interface{}{"events": events},
				})
				client.Send <- awayPkt
			}
		}

		// Refresh LastActiveAt so it ticks from now.
		_ = p.charService.UpdateLastActive(context.Background(), payload.CharacterID)
	}

	ack, _ := json.Marshal(map[string]interface{}{
		"type": "SELECT_CHARACTER_ACK",
		"payload": map[string]interface{}{
			"character_id": payload.CharacterID,
			"status":       "ACTIVE",
		},
	})
	client.Send <- ack
}

// ---------------------------------------------------------------------------
// inventory (resolves the character <-> inventory construction cycle)
// ---------------------------------------------------------------------------

type inventoryPlugin struct{ plugin.Base }

func (inventoryPlugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "inventory", Requires: []string{"core", "character"}}
}

func (inventoryPlugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	mux := ic.Mux()
	jwt := ic.Config().JWTSecret

	db := registry.MustResolve[*database.DB](reg, "db")
	charService := registry.MustResolve[character.CharacterService](reg, "character")

	invRepo := inventory.NewInventoryRepository(db.Store)
	invService := inventory.NewInventoryService(invRepo, charService, ic.Bus())
	if err := registry.Provide(reg, "inventory", invService); err != nil {
		return err
	}

	// Resolve the circular startup reference now that both services exist.
	charService.SetEquipmentProvider(invService)

	mux.Handle("GET /api/v1/characters/{id}/inventory", auth.AuthMiddleware(jwt)(inventory.GetInventoryHandler(invService)))
	mux.Handle("POST /api/v1/inventory/move", auth.AuthMiddleware(jwt)(inventory.MoveItemHandler(invService)))
	mux.Handle("POST /api/v1/admin/inventory/add", adminOnly(jwt, inventory.AddAdminItemHandler(invService)))

	return nil
}

// ---------------------------------------------------------------------------
// loot
// ---------------------------------------------------------------------------

type lootPlugin struct{ plugin.Base }

func (lootPlugin) Meta() plugin.Meta { return plugin.Meta{Name: "loot", Requires: []string{"core"}} }

func (lootPlugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	mux := ic.Mux()
	jwt := ic.Config().JWTSecret

	db := registry.MustResolve[*database.DB](reg, "db")
	appCache := registry.MustResolve[cache.Cache](reg, "cache")

	// Loot tables are static config read on every kill, so wrap the repository
	// in a read-through cache.
	var lootRepo loot.LootRepository = loot.NewLootRepository(db.Store)
	lootRepo = loot.NewCachedRepository(lootRepo, appCache, 10*time.Minute)
	lootService := loot.NewLootService(lootRepo, loot.WithHooks(ic.Hooks()))
	if err := registry.Provide(reg, "loot", lootService); err != nil {
		return err
	}

	mux.Handle("POST /api/v1/admin/loot", adminOnly(jwt, loot.CreateLootTableHandler(lootService)))
	mux.Handle("GET /api/v1/admin/loot", auth.AuthMiddleware(jwt)(loot.ListLootTablesHandler(lootService)))

	return nil
}

// ---------------------------------------------------------------------------
// quests
// ---------------------------------------------------------------------------

type questsPlugin struct{ plugin.Base }

func (questsPlugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "quests", Requires: []string{"core", "character", "inventory"}}
}

func (questsPlugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	mux := ic.Mux()
	jwt := ic.Config().JWTSecret

	db := registry.MustResolve[*database.DB](reg, "db")
	charService := registry.MustResolve[character.CharacterService](reg, "character")
	invService := registry.MustResolve[inventory.InventoryService](reg, "inventory")

	questRepo := quests.NewQuestRepository(db.Store)
	questService := quests.NewQuestService(questRepo, charService, invService, ic.Bus(), ic.Hooks())
	if err := registry.Provide(reg, "quests", questService); err != nil {
		return err
	}

	mux.Handle("POST /api/v1/admin/quests", adminOnly(jwt, quests.CreateDefinitionHandler(questService)))
	mux.Handle("GET /api/v1/quests", auth.AuthMiddleware(jwt)(quests.ListDefinitionsHandler(questService)))
	mux.Handle("POST /api/v1/characters/{id}/quests/accept", auth.AuthMiddleware(jwt)(quests.AcceptQuestHandler(questService)))
	mux.Handle("GET /api/v1/characters/{id}/quests", auth.AuthMiddleware(jwt)(quests.GetQuestLogHandler(questService)))
	mux.Handle("POST /api/v1/admin/quests/progress", adminOnly(jwt, quests.UpdateQuestProgressHandler(questService)))

	return nil
}

// ---------------------------------------------------------------------------
// combat (owns COMBAT_ATTACK)
// ---------------------------------------------------------------------------

type combatPlugin struct{ plugin.Base }

func (combatPlugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "combat", Requires: []string{"core", "character", "inventory", "loot", "quests"}}
}

func (combatPlugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()

	charService := registry.MustResolve[character.CharacterService](reg, "character")
	invService := registry.MustResolve[inventory.InventoryService](reg, "inventory")
	lootService := registry.MustResolve[loot.LootService](reg, "loot")
	questService := registry.MustResolve[quests.QuestService](reg, "quests")
	stat := registry.MustResolve[*statHolder](reg, "stat")
	hub := registry.MustResolve[*socket.Hub](reg, "hub")
	router := registry.MustResolve[*socket.MessageRouter](reg, "msgRouter")
	sessionReg := registry.MustResolve[*session.Registry](reg, "session")

	skillService := skills.NewService()
	ic.Mux().Handle("GET /api/v1/skills", auth.AuthMiddleware(ic.Config().JWTSecret)(skills.ListHandler(skillService)))

	// Unified combatant resolution: mobs (content pack) then characters, so combat
	// treats every fighter as a creature.
	mobs := content.MustLoadMobs()
	creatures := compositeCreatureResolver{
		mobCreatureResolver{mobs},
		charCreatureResolver{charService, mobs.PvP},
	}

	rewarder := killreward.New(creatures, charService, questService, lootService, invService, ic.Bus())
	combatService := combat.NewCombatService(creatures, stat.client, sessionReg, rewarder, ic.Bus(), ic.Hooks(), skillResolver{skillService})

	router.Handle("COMBAT_ATTACK", func(client *socket.Client, msg socket.WSMessage) {
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
	})

	return nil
}

// ---------------------------------------------------------------------------
// idle (standalone event-driven offline progression plugin)
// ---------------------------------------------------------------------------

type idlePlugin struct{ plugin.Base }

func (idlePlugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "idle", Requires: []string{"core", "character", "inventory", "loot"}}
}

func (p *idlePlugin) Init(plugin.InitContext) error { return nil }

func (p *idlePlugin) Start(rc plugin.RunContext) error {
	reg := rc.Registry()
	chars := registry.MustResolve[character.CharacterService](reg, "character")
	lootSvc := registry.MustResolve[loot.LootService](reg, "loot")
	invSvc := registry.MustResolve[inventory.InventoryService](reg, "inventory")
	hub := registry.MustResolve[*socket.Hub](reg, "hub")

	idleSvc := idle.NewService(chars, lootSvc, invSvc)

	rc.Bus().Subscribe(character.EventCharacterLoggedIn, func(ctx context.Context, ev bus.Event) {
		e, ok := ev.(character.CharacterLoggedIn)
		if !ok {
			return
		}
		char, err := chars.GetByID(ctx, e.CharacterID)
		if err != nil {
			return
		}
		grant, granted, err := idleSvc.GrantOffline(ctx, e.CharacterID, char.Stats.BaseStats, e.LastActiveAt)
		if err != nil || !granted {
			return
		}

		gainsSummary, _ := json.Marshal(map[string]interface{}{
			"type": "OFFLINE_GAINS",
			"payload": map[string]interface{}{
				"elapsed_seconds": grant.ElapsedSeconds,
				"gained_gold":     grant.Gold,
				"gained_exp":      grant.Exp,
				"leveled_up":      grant.LeveledUp,
				"new_level":       grant.NewLevel,
				"loot":            grant.Loot,
			},
		})
		if client, exists := hub.GetClientByCharacterID(e.CharacterID); exists {
			client.Send <- gainsSummary
		}

		if rc.Bus() != nil {
			_ = rc.Bus().Publish(ctx, character.OfflineGainsGranted{
				CharacterID:    e.CharacterID,
				ElapsedSeconds: grant.ElapsedSeconds,
				Gold:           grant.Gold,
				Exp:            grant.Exp,
				LeveledUp:      grant.LeveledUp,
				NewLevel:       grant.NewLevel,
				Loot:           grant.Loot,
			})
		}
	})

	return nil
}
