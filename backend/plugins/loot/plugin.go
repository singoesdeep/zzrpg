package loot

import (
	"time"

	"github.com/singoesdeep/zzrpg/backend/game/auth"
	"github.com/singoesdeep/zzrpg/backend/game/loot"
	"github.com/singoesdeep/zzrpg/backend/platform/database"
	authplugin "github.com/singoesdeep/zzrpg/backend/plugins/auth"
	"github.com/singoesdeep/zzrpg/sdk/engine/admin"
	"github.com/singoesdeep/zzrpg/sdk/engine/outbox"
	"github.com/singoesdeep/zzrpg/sdk/engine/plugin"
	"github.com/singoesdeep/zzrpg/sdk/engine/registry"
	"github.com/singoesdeep/zzrpg/sdk/pkg/cache"
)

type Plugin struct{ plugin.Base }

func (Plugin) AdminInfo() admin.Info {
	return admin.Info{
		Title:       "Loot Tables",
		Description: "Probability-based loot tables and item drop roll engines",
		Icon:        "fa-coins",
		Category:    "Economy",
		Endpoints:   []string{"POST /api/v1/admin/loot", "GET /api/v1/admin/loot"},
	}
}

func (Plugin) Meta() plugin.Meta { return plugin.Meta{Name: "loot", Requires: []string{"core"}} }

func (Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	// Own this domain's event decoders (moved out of core).
	if decoders, err := registry.Resolve[*outbox.Registry](reg, "eventDecoders"); err == nil {
		loot.RegisterEventDecoders(decoders)
	}
	mux := ic.Mux()
	jwt := ic.Config().JWTSecret

	db := registry.MustResolve[*database.DB](reg, "db")
	appCache := registry.MustResolve[cache.Cache](reg, "cache")

	var lootRepo loot.LootRepository = loot.NewLootRepository(db.Store)
	lootRepo = loot.NewCachedRepository(lootRepo, appCache, 10*time.Minute)
	lootService := loot.NewLootService(lootRepo, loot.WithHooks(ic.Hooks()))
	if err := registry.Provide(reg, "loot", lootService); err != nil {
		return err
	}

	mux.Handle("POST /api/v1/admin/loot", authplugin.AdminOnly(jwt, loot.CreateLootTableHandler(lootService)))
	mux.Handle("GET /api/v1/admin/loot", auth.AuthMiddleware(jwt)(loot.ListLootTablesHandler(lootService)))

	return nil
}
