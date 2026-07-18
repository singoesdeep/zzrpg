package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/singoesdeep/zzrpg/backend/internal/database"
	"github.com/singoesdeep/zzrpg/backend/pkg/config"
	"github.com/singoesdeep/zzrpg/backend/pkg/logger"
)

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

	// 4. Setup multiplexer / router
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
