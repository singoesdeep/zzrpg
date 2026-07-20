package items

import (
	"github.com/singoesdeep/zzrpg/backend/engine/admin"
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/internal/auth"
	"github.com/singoesdeep/zzrpg/backend/internal/database"
	"github.com/singoesdeep/zzrpg/backend/internal/items"
	authplugin "github.com/singoesdeep/zzrpg/backend/plugins/auth"
)

type Plugin struct{ plugin.Base }

func (Plugin) AdminInfo() admin.Info {
	return admin.Info{
		Title:       "Items Catalog",
		Description: "Data-driven item definitions, base stat modifiers, and rarity tiers",
		Icon:        "fa-shield-halved",
		Category:    "Content",
		Endpoints:   []string{"POST /api/v1/admin/items", "PUT /api/v1/admin/items/{id}", "GET /api/v1/admin/items", "DELETE /api/v1/admin/items/{id}"},
	}
}

func (Plugin) Meta() plugin.Meta { return plugin.Meta{Name: "items", Requires: []string{"core"}} }

func (Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	mux := ic.Mux()
	jwt := ic.Config().JWTSecret

	db := registry.MustResolve[*database.DB](reg, "db")
	itemRepo := items.NewItemRepository(db.Store)
	itemService := items.NewItemService(itemRepo)

	mux.Handle("POST /api/v1/admin/items", authplugin.AdminOnly(jwt, items.CreateHandler(itemService)))
	mux.Handle("PUT /api/v1/admin/items/{id}", authplugin.AdminOnly(jwt, items.UpdateHandler(itemService)))
	mux.Handle("GET /api/v1/admin/items", auth.AuthMiddleware(jwt)(items.ListHandler(itemService)))
	mux.Handle("GET /api/v1/admin/items/{id}", auth.AuthMiddleware(jwt)(items.GetHandler(itemService)))
	mux.Handle("DELETE /api/v1/admin/items/{id}", authplugin.AdminOnly(jwt, items.DeleteHandler(itemService)))

	return nil
}
