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

	"github.com/singoesdeep/zzrpg/backend/plugins/auth"
	"github.com/singoesdeep/zzrpg/backend/plugins/character"
	"github.com/singoesdeep/zzrpg/backend/plugins/combat"
	"github.com/singoesdeep/zzrpg/backend/plugins/core"
	"github.com/singoesdeep/zzrpg/backend/plugins/crafting"
	"github.com/singoesdeep/zzrpg/backend/plugins/idle"
	"github.com/singoesdeep/zzrpg/backend/plugins/idlekit"
	"github.com/singoesdeep/zzrpg/backend/plugins/inventory"
	"github.com/singoesdeep/zzrpg/backend/plugins/items"
	"github.com/singoesdeep/zzrpg/backend/plugins/loot"
	"github.com/singoesdeep/zzrpg/backend/plugins/quests"
	"github.com/singoesdeep/zzrpg/backend/plugins/stat"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		stdlog.Fatalf("failed to load configuration: %v", err)
	}

	log := logger.NewLogger(cfg.Env)
	log.Info("Starting zzrpg backend...", "env", cfg.Env, "port", cfg.Port)

	k := kernel.New(cfg, log)

	k.Register(
		core.NewPlugin(),
		&stat.Plugin{}, // optional: omit for a game that needs no stat math
		&auth.Plugin{},
		&items.Plugin{},
		&character.Plugin{},
		&inventory.Plugin{},
		&loot.Plugin{},
		&quests.Plugin{},
		&combat.Plugin{},
		&idle.Plugin{},
		&idlekit.Plugin{}, // pilot: idle rebuilt on gamekit, runs alongside for comparison
		&crafting.Plugin{},
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := k.Run(ctx); err != nil {
		log.Error("engine terminated with error", "error", err)
		os.Exit(1)
	}
}
