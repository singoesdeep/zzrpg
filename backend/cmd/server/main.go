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
	"github.com/singoesdeep/zzrpg/backend/internal/database"
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

	// Initialize Character components
	charRepo := character.NewCharacterRepository(db.Pool)
	charService := character.NewCharacterService(charRepo)

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

	mux.HandleFunc("GET /swagger", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		data, err := apiFS.ReadFile("api/swagger.html")
		if err != nil {
			log.Error("Failed to read swagger.html", "error", err)
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
