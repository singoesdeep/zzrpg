// Command citygame is a second, completely different game built on the same
// engine — a standalone idle city builder — assembled from just the core
// infrastructure plugin and the city plugin. It registers NO character, combat,
// or stat plugins, so it boots WITHOUT the Rust zzstat library: proof that the
// engine + plugin surface supports building an unrelated game plugin-only,
// without touching engine/ or platform/ code.
package main

import (
	"context"
	stdlog "log"
	"os"
	"os/signal"
	"syscall"

	"github.com/singoesdeep/zzrpg/backend/engine/kernel"
	"github.com/singoesdeep/zzrpg/backend/pkg/config"
	"github.com/singoesdeep/zzrpg/backend/pkg/logger"

	"github.com/singoesdeep/zzrpg/backend/plugins/city"
	"github.com/singoesdeep/zzrpg/backend/plugins/core"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		stdlog.Fatalf("failed to load configuration: %v", err)
	}

	log := logger.NewLogger(cfg.Env)
	log.Info("Starting city-builder game...", "env", cfg.Env, "port", cfg.Port)

	k := kernel.New(cfg, log)
	k.Register(
		core.NewPlugin(),
		&city.Plugin{},
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := k.Run(ctx); err != nil {
		log.Error("engine terminated with error", "error", err)
		os.Exit(1)
	}
}
