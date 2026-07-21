// Command gamedemo runs a game built entirely on the gamekit framework:
// core (infrastructure) + the gamedemo plugin. It registers no combat, no auth,
// and no stat library — proof that the framework's toolkits (entity, component,
// stats, progression, inventory, System scheduler) compose a runnable game.
package main

import (
	"context"
	stdlog "log"
	"os"
	"os/signal"
	"syscall"

	"github.com/singoesdeep/zzrpg/sdk/engine/kernel"
	"github.com/singoesdeep/zzrpg/sdk/pkg/config"
	"github.com/singoesdeep/zzrpg/sdk/pkg/logger"

	"github.com/singoesdeep/zzrpg/backend/plugins/core"
	"github.com/singoesdeep/zzrpg/backend/plugins/gamedemo"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		stdlog.Fatalf("failed to load configuration: %v", err)
	}
	log := logger.NewLogger(cfg.Env)
	log.Info("Starting gamekit demo...", "env", cfg.Env, "port", cfg.Port)

	k := kernel.New(cfg, log)
	k.Register(core.NewPlugin(), &gamedemo.Plugin{})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := k.Run(ctx); err != nil {
		log.Error("engine terminated with error", "error", err)
		os.Exit(1)
	}
}
