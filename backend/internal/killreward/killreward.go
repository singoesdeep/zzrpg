// Package killreward implements the death-progression side effects of a kill:
// quest progress, loot rolling, and applying drops (gold to the killer's
// balance, items to their inventory). It is the concrete implementation of
// combat.KillRewarder, kept in its own package so the combat service does not
// depend on the quest, loot, or inventory services directly.
package killreward

import (
	"context"
	"strconv"

	"github.com/singoesdeep/zzrpg/backend/content"
	"github.com/singoesdeep/zzrpg/backend/internal/inventory"
	"github.com/singoesdeep/zzrpg/backend/internal/loot"
)

// mobDefs is the mob content pack, loaded once from embedded content. It drives
// which loot table a kill rolls and which quest tag it counts toward.
var mobDefs = content.MustLoadMobs()

// The interfaces below are consumer-owned: killreward declares the minimal
// surface it needs from each collaborator, so it depends on those behaviours
// rather than importing the character and quest service packages. This drops the
// killreward→character and killreward→quests package edges entirely.

// CharacterRewarder credits gold/exp to a character.
type CharacterRewarder interface {
	AddRewards(ctx context.Context, charID int64, gold int64, exp int64) (bool, int32, error)
}

// QuestProgressor advances a character's quest progress for an action.
type QuestProgressor interface {
	UpdateQuestProgress(ctx context.Context, charID int32, actionType string, target string, amount int32) error
}

// LootRoller rolls a loot table.
type LootRoller interface {
	RollLoot(ctx context.Context, tableID string) ([]loot.DroppedItem, error)
}

// InventoryWriter grants an item to a character's inventory.
type InventoryWriter interface {
	AddItem(ctx context.Context, item *inventory.InventoryItem) error
}

// Service orchestrates kill rewards across the character, quest, loot, and
// inventory services.
type Service struct {
	charSvc      CharacterRewarder
	questSvc     QuestProgressor
	lootSvc      LootRoller
	inventorySvc InventoryWriter
}

// New builds a Service. Any of questSvc, lootSvc, or inventorySvc may be nil,
// in which case the corresponding step is skipped (matching the prior inline
// behaviour in combat).
func New(
	charSvc CharacterRewarder,
	questSvc QuestProgressor,
	lootSvc LootRoller,
	inventorySvc InventoryWriter,
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
	// Resolve loot table + quest tag from the mob pack; fall back to the PvP
	// defaults when the victim is a real character rather than a defined mob.
	tableID := mobDefs.PvP.LootTableID
	questTag := mobDefs.PvP.QuestTag
	if mob, ok := mobDefs.Mobs[strconv.FormatInt(victimID, 10)]; ok {
		tableID = mob.LootTableID
		questTag = mob.QuestTag
	}

	if s.questSvc != nil {
		_ = s.questSvc.UpdateQuestProgress(ctx, int32(killerID), "KILL_MOB", questTag, 1)
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
