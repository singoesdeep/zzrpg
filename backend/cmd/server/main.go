package main

import (
	"context"
	"embed"
	stdlog "log"
	"os"
	"os/signal"
	"syscall"

	"github.com/singoesdeep/zzrpg/backend/engine/kernel"
	"github.com/singoesdeep/zzrpg/backend/pkg/config"
	"github.com/singoesdeep/zzrpg/backend/pkg/logger"
)

//go:embed api/*
var apiFS embed.FS

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		// The structured logger depends on cfg.Env, which isn't available yet,
		// so use the standard logger for this one fatal startup error.
		stdlog.Fatalf("failed to load configuration: %v", err)
	}

	log := logger.NewLogger(cfg.Env)
	log.Info("Starting zzrpg backend...", "env", cfg.Env, "port", cfg.Port)

	k := kernel.New(cfg, log)

	k.Register(
		&corePlugin{},
		authPlugin{},
		itemsPlugin{},
		&characterPlugin{},
		inventoryPlugin{},
		lootPlugin{},
		questsPlugin{},
		combatPlugin{},
		&idlePlugin{},
	)

	// Cancel the run context on SIGINT/SIGTERM for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := k.Run(ctx); err != nil {
		log.Error("engine terminated with error", "error", err)
		os.Exit(1)
	}
}
