package quests

import (
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/internal/auth"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/database"
	"github.com/singoesdeep/zzrpg/backend/internal/inventory"
	"github.com/singoesdeep/zzrpg/backend/internal/quests"
	authplugin "github.com/singoesdeep/zzrpg/backend/plugins/auth"
)

type Plugin struct{ plugin.Base }

func (Plugin) AdminInfo() plugin.AdminInfo {
	return plugin.AdminInfo{
		Title:       "Quest System",
		Description: "Multi-step quest chains, objective progress tracking, and reward distribution",
		Icon:        "fa-scroll",
		Category:    "Gameplay",
		Endpoints:   []string{"POST /api/v1/admin/quests", "GET /api/v1/quests", "POST /api/v1/characters/{id}/quests/accept", "GET /api/v1/characters/{id}/quests"},
	}
}

func (Plugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "quests", Requires: []string{"core", "character", "inventory"}}
}

func (Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	mux := ic.Mux()
	jwt := ic.Config().JWTSecret

	db := registry.MustResolve[*database.DB](reg, "db")
	charService := registry.MustResolve[character.CharacterService](reg, "character")
	invService := registry.MustResolve[inventory.InventoryService](reg, "inventory")

	questRepo := quests.NewQuestRepository(db.Store)
	questService := quests.NewQuestService(questRepo, charService, invService, ic.Bus(), ic.Hooks())
	if err := registry.Provide(reg, "quests", questService); err != nil {
		return err
	}

	mux.Handle("POST /api/v1/admin/quests", authplugin.AdminOnly(jwt, quests.CreateDefinitionHandler(questService)))
	mux.Handle("GET /api/v1/quests", auth.AuthMiddleware(jwt)(quests.ListDefinitionsHandler(questService)))
	mux.Handle("POST /api/v1/characters/{id}/quests/accept", auth.AuthMiddleware(jwt)(quests.AcceptQuestHandler(questService)))
	mux.Handle("GET /api/v1/characters/{id}/quests", auth.AuthMiddleware(jwt)(quests.GetQuestLogHandler(questService)))
	mux.Handle("POST /api/v1/admin/quests/progress", authplugin.AdminOnly(jwt, quests.UpdateQuestProgressHandler(questService)))

	return nil
}
