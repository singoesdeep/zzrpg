package inventory

import (
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/internal/auth"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/database"
	"github.com/singoesdeep/zzrpg/backend/internal/inventory"
	authplugin "github.com/singoesdeep/zzrpg/backend/plugins/auth"
)

type Plugin struct{ plugin.Base }

func (Plugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "inventory", Requires: []string{"core", "character"}}
}

func (Plugin) Init(ic plugin.InitContext) error {
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

	charService.SetEquipmentProvider(invService)

	mux.Handle("GET /api/v1/characters/{id}/inventory", auth.AuthMiddleware(jwt)(inventory.GetInventoryHandler(invService)))
	mux.Handle("POST /api/v1/inventory/move", auth.AuthMiddleware(jwt)(inventory.MoveItemHandler(invService)))
	mux.Handle("POST /api/v1/admin/inventory/add", authplugin.AdminOnly(jwt, inventory.AddAdminItemHandler(invService)))

	return nil
}
