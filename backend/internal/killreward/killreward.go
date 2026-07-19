// Package killreward implements the death-progression side effects of a kill:
// quest progress, loot rolling, and applying drops (gold to the killer's
// balance, items to their inventory). It is the concrete implementation of
// combat.KillRewarder, kept in its own package so the combat service does not
// depend on the quest, loot, or inventory services directly.
package killreward

import (
	"context"

	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/inventory"
	"github.com/singoesdeep/zzrpg/backend/internal/loot"
	"github.com/singoesdeep/zzrpg/backend/internal/quests"
)

// Service orchestrates kill rewards across the character, quest, loot, and
// inventory services.
type Service struct {
	charSvc      character.CharacterService
	questSvc     quests.QuestService
	lootSvc      loot.LootService
	inventorySvc inventory.InventoryService
}

// New builds a Service. Any of questSvc, lootSvc, or inventorySvc may be nil,
// in which case the corresponding step is skipped (matching the prior inline
// behaviour in combat).
func New(
	charSvc character.CharacterService,
	questSvc quests.QuestService,
	lootSvc loot.LootService,
	inventorySvc inventory.InventoryService,
) *Service {
	return &Service{
		charSvc:      charSvc,
		questSvc:     questSvc,
		lootSvc:      lootSvc,
		inventorySvc: inventorySvc,
	}
}

// RewardKill advances kill quests, rolls the appropriate loot table, and applies
// the drops. It returns the rolled loot so the caller can surface it in the
// attack response. The killer/victim identity determines the loot table and the
// quest target tag (dummy 9999 => "dummy_drops"/"wolf"; otherwise PvP).
func (s *Service) RewardKill(ctx context.Context, killerID, victimID int64) []loot.DroppedItem {
	var tableID string
	if victimID == 9999 {
		tableID = "dummy_drops"
		if s.questSvc != nil {
			_ = s.questSvc.UpdateQuestProgress(ctx, int32(killerID), "KILL_MOB", "wolf", 1)
		}
	} else {
		tableID = "player_drops" // or default PvP table
		if s.questSvc != nil {
			_ = s.questSvc.UpdateQuestProgress(ctx, int32(killerID), "KILL_MOB", "player", 1)
		}
	}

	if s.lootSvc == nil {
		return nil
	}

	drops, err := s.lootSvc.RollLoot(ctx, tableID)
	if err != nil {
		return nil
	}

	// Process drops: add gold or items.
	for _, drop := range drops {
		if drop.ItemDefinitionID == "gold" {
			if s.charSvc != nil {
				_, _, _ = s.charSvc.AddRewards(ctx, killerID, int64(drop.Quantity), 0)
			}
		} else if s.inventorySvc != nil {
			invItem := &inventory.InventoryItem{
				CharacterID:      int32(killerID),
				ItemDefinitionID: drop.ItemDefinitionID,
				Quantity:         drop.Quantity,
				Durability:       100,
			}
			_ = s.inventorySvc.AddItem(ctx, invItem)
		}
	}

	return drops
}
