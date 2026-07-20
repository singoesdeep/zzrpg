package idle

import (
	"context"
	"encoding/json"

	"github.com/singoesdeep/zzrpg/backend/engine/admin"
	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/plugin"
	"github.com/singoesdeep/zzrpg/backend/engine/registry"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/idle"
	"github.com/singoesdeep/zzrpg/backend/internal/inventory"
	"github.com/singoesdeep/zzrpg/backend/internal/loot"
	"github.com/singoesdeep/zzrpg/backend/internal/socket"
)

type Plugin struct{ plugin.Base }

func (Plugin) AdminInfo() admin.Info {
	return admin.Info{
		Title:       "Idle Progression",
		Description: "Standalone event-driven offline progression calculating STR/INT scaled gold, exp, and loot",
		Icon:        "fa-moon",
		Category:    "Economy",
		Endpoints:   []string{"EVENT: CharacterLoggedIn -> OfflineGainsGranted"},
	}
}

func (Plugin) Meta() plugin.Meta {
	return plugin.Meta{Name: "idle", Requires: []string{"core", "character", "inventory", "loot"}}
}

func (Plugin) Init(plugin.InitContext) error { return nil }

func (p *Plugin) Start(rc plugin.RunContext) error {
	reg := rc.Registry()
	chars := registry.MustResolve[character.CharacterService](reg, "character")
	lootSvc := registry.MustResolve[loot.LootService](reg, "loot")
	invSvc := registry.MustResolve[inventory.InventoryService](reg, "inventory")
	hub := registry.MustResolve[*socket.Hub](reg, "hub")

	idleSvc := idle.NewService(chars, lootSvc, invSvc)

	rc.Bus().Subscribe(character.EventCharacterLoggedIn, func(ctx context.Context, ev bus.Event) {
		if mgr, err := registry.Resolve[*admin.StateManager](reg, "pluginManager"); err == nil && !mgr.IsActive("idle") {
			return
		}

		e, ok := ev.(character.CharacterLoggedIn)
		if !ok {
			return
		}
		char, err := chars.GetByID(ctx, e.CharacterID)
		if err != nil {
			return
		}
		grant, granted, err := idleSvc.GrantOffline(ctx, e.CharacterID, char.Stats.BaseStats, e.LastActiveAt)
		if err != nil || !granted {
			return
		}

		gainsSummary, _ := json.Marshal(map[string]interface{}{
			"type": "OFFLINE_GAINS",
			"payload": map[string]interface{}{
				"elapsed_seconds": grant.ElapsedSeconds,
				"gained_gold":     grant.Gold,
				"gained_exp":      grant.Exp,
				"leveled_up":      grant.LeveledUp,
				"new_level":       grant.NewLevel,
				"loot":            grant.Loot,
			},
		})
		if client, exists := hub.GetClientByCharacterID(e.CharacterID); exists {
			client.Send <- gainsSummary
		}

		if rc.Bus() != nil {
			_ = rc.Bus().Publish(ctx, character.OfflineGainsGranted{
				CharacterID:    e.CharacterID,
				ElapsedSeconds: grant.ElapsedSeconds,
				Gold:           grant.Gold,
				Exp:            grant.Exp,
				LeveledUp:      grant.LeveledUp,
				NewLevel:       grant.NewLevel,
				Loot:           grant.Loot,
			})
		}
	})

	return nil
}
