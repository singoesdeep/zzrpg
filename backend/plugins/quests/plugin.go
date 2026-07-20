package quests

import (
	"github.com/singoesdeep/zzrpg/backend/engine/admin"
	"github.com/singoesdeep/zzrpg/backend/engine/outbox"
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/game/auth"
	"github.com/singoesdeep/zzrpg/backend/game/character"
	"github.com/singoesdeep/zzrpg/backend/game/inventory"
	"github.com/singoesdeep/zzrpg/backend/game/quests"
	"github.com/singoesdeep/zzrpg/backend/platform/database"
	authplugin "github.com/singoesdeep/zzrpg/backend/plugins/auth"
)

type Plugin struct{ plugin.Base }

func (Plugin) AdminInfo() admin.Info {
	return admin.Info{
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
	// Own this domain's event decoders (moved out of core).
	if decoders, err := registry.Resolve[*outbox.Registry](reg, "eventDecoders"); err == nil {
		quests.RegisterEventDecoders(decoders)
	}
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
