package loot

import (
	"time"

	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/internal/auth"
	"github.com/singoesdeep/zzrpg/backend/internal/database"
	"github.com/singoesdeep/zzrpg/backend/internal/loot"
	authplugin "github.com/singoesdeep/zzrpg/backend/plugins/auth"
	"github.com/singoesdeep/zzrpg/backend/pkg/cache"
)

type Plugin struct{ plugin.Base }

func (Plugin) Meta() plugin.Meta { return plugin.Meta{Name: "loot", Requires: []string{"core"}} }

func (Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
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
