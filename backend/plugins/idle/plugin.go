package idle

import (
	"context"
	"encoding/json"

	"github.com/singoesdeep/zzrpg/backend/engine/admin"
	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/game/auth"
	"github.com/singoesdeep/zzrpg/backend/game/character"
	"github.com/singoesdeep/zzrpg/backend/game/idle"
	"github.com/singoesdeep/zzrpg/backend/game/inventory"
	"github.com/singoesdeep/zzrpg/backend/game/loot"
	"github.com/singoesdeep/zzrpg/backend/platform/database"
	"github.com/singoesdeep/zzrpg/backend/platform/socket"
)

type Plugin struct {
	plugin.Base
	svc   *idle.Service
	chars character.CharacterService
	hub   *socket.Hub
}

func (*Plugin) AdminInfo() admin.Info {
	return admin.Info{
		Title:       "Idle Progression",
		Description: "Content-driven offline/idle activities: combat stages, gathering lifeskills, and RTS resource generators",
		Icon:        "fa-moon",
		Category:    "Economy",
		Endpoints: []string{
			"GET /api/v1/characters/{id}/idle/state",
			"POST /api/v1/characters/{id}/idle/assign",
			"EVENT: CharacterLoggedIn -> OFFLINE_GAINS",
		},
	}
}

func (*Plugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "idle", Requires: []string{"core", "character", "inventory", "loot"}}
}

func (p *Plugin) Init(ic plugin.InitContext) error {
	reg := ic.Registry()
	p.chars = registry.MustResolve[character.CharacterService](reg, "character")
	lootSvc := registry.MustResolve[loot.LootService](reg, "loot")
	invSvc := registry.MustResolve[inventory.InventoryService](reg, "inventory")
	p.hub = registry.MustResolve[*socket.Hub](reg, "hub")
	db := registry.MustResolve[*database.DB](reg, "db")

	p.svc = idle.NewService(idle.Deps{
		Chars:       p.chars,
		Loot:        lootSvc,
		Inv:         invSvc,
		Assignments: idle.NewAssignmentRepo(db.Store),
		Lifeskills:  idle.NewLifeskillRepo(db.Store),
		Buildings:   idle.NewBuildingRepo(db.Store),
		Wallet:      idle.NewWalletRepo(db.Store),
	})
	if err := registry.Provide(reg, "idle", p.svc); err != nil {
		return err
	}

	jwt := ic.Config().JWTSecret
	mux := ic.Mux()
	mux.Handle("GET /api/v1/characters/{id}/idle/state", auth.AuthMiddleware(jwt)(idle.StateHandler(p.svc, p.chars)))
	mux.Handle("GET /api/v1/characters/{id}/idle/activities", auth.AuthMiddleware(jwt)(idle.ActivitiesHandler(p.svc, p.chars)))
	mux.Handle("POST /api/v1/characters/{id}/idle/assign", auth.AuthMiddleware(jwt)(idle.AssignHandler(p.svc, p.chars)))
	mux.Handle("POST /api/v1/characters/{id}/idle/buildings/{gen}/upgrade", auth.AuthMiddleware(jwt)(idle.UpgradeBuildingHandler(p.svc, p.chars)))
	return nil
}

func (p *Plugin) Start(rc plugin.RunContext) error {
	// Activation gating is handled by the plugin-scoped bus, so this handler
	// automatically stops firing while the idle plugin is deactivated.
	rc.Bus().Subscribe(character.EventCharacterLoggedIn, func(ctx context.Context, ev bus.Event) {
		e, ok := ev.(character.CharacterLoggedIn)
		if !ok {
			return
		}
		char, err := p.chars.GetByID(ctx, e.CharacterID)
		if err != nil {
			return
		}
		// The active focus (stage / lifeskill) and building/skill levels come from
		// the character's persisted idle state; a character with no assignment
		// falls back to the starter stage. Output scales with combat power.
		power := p.svc.Power(char.Stats.DerivedStats)
		grant, granted, err := p.svc.Accrue(ctx, idle.AccrualRequest{
			CharacterID: e.CharacterID,
			Since:       e.LastActiveAt,
			Power:       power,
			Level:       char.Level,
		})
		if err != nil || !granted {
			return
		}

		gainsSummary, _ := json.Marshal(map[string]interface{}{
			"type": "OFFLINE_GAINS",
			"payload": map[string]interface{}{
				"elapsed_seconds":    grant.ElapsedSeconds,
				"gained_gold":        grant.Gold,
				"gained_exp":         grant.Exp,
				"leveled_up":         grant.LeveledUp,
				"new_level":          grant.NewLevel,
				"loot":               grant.Loot,
				"resources":          grant.Resources,
				"lifeskill_levelups": grant.LifeskillLevelUps,
			},
		})
		if client, exists := p.hub.GetClientByCharacterID(e.CharacterID); exists {
			client.Send <- gainsSummary
		}

		_ = rc.Bus().Publish(ctx, character.OfflineGainsGranted{
			CharacterID:    e.CharacterID,
			ElapsedSeconds: grant.ElapsedSeconds,
			Gold:           grant.Gold,
			Exp:            grant.Exp,
			LeveledUp:      grant.LeveledUp,
			NewLevel:       grant.NewLevel,
			Loot:           grant.Loot,
		})
	})
	return nil
}
