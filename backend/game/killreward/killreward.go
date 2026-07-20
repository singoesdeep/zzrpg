package killreward

import (
	"context"

	"github.com/singoesdeep/zzrpg/backend/game/creature"
	"github.com/singoesdeep/zzrpg/backend/game/inventory"
	"github.com/singoesdeep/zzrpg/backend/game/loot"
	"github.com/singoesdeep/zzrpg/sdk/engine/bus"
)

// CreatureResolver resolves a creature ID to a Creature model containing stats
// and reward metadata.
type CreatureResolver interface {
	Resolve(ctx context.Context, id int64) (creature.Creature, bool, error)
}

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
	creatures    CreatureResolver
	charSvc      CharacterRewarder
	questSvc     QuestProgressor
	lootSvc      LootRoller
	inventorySvc InventoryWriter
	eventBus     bus.EventBus
}

// New builds a Service. Any of creatures, questSvc, lootSvc, inventorySvc, or
// eventBus may be nil, in which case the corresponding step is skipped.
func New(
	creatures CreatureResolver,
	charSvc CharacterRewarder,
	questSvc QuestProgressor,
	lootSvc LootRoller,
	inventorySvc InventoryWriter,
	eventBus bus.EventBus,
) *Service {
	return &Service{
		creatures:    creatures,
		charSvc:      charSvc,
		questSvc:     questSvc,
		lootSvc:      lootSvc,
		inventorySvc: inventorySvc,
		eventBus:     eventBus,
	}
}

// RewardKill advances kill quests, rolls the appropriate loot table, and applies
// the drops. It returns the rolled loot so the caller can surface it in the
// attack response. The victim's resolved creature metadata determines the loot
// table and the quest target tag.
func (s *Service) RewardKill(ctx context.Context, killerID, victimID int64) []loot.DroppedItem {
	var tableID, questTag string
	if s.creatures != nil {
		if victim, ok, err := s.creatures.Resolve(ctx, victimID); err == nil && ok {
			tableID = victim.LootTableID
			questTag = victim.QuestTag
		}
	}

	if s.questSvc != nil && questTag != "" {
		_ = s.questSvc.UpdateQuestProgress(ctx, int32(killerID), "KILL_MOB", questTag, 1)
	}

	if s.lootSvc == nil || tableID == "" {
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

	// Announce the drop so consumers (UI, analytics, collection tracking) can
	// react. Additive and async; does not affect the returned loot.
	if len(drops) > 0 && s.eventBus != nil {
		_ = s.eventBus.Publish(ctx, loot.LootDropped{CharacterID: killerID, TableID: tableID, Items: drops})
	}

	return drops
}
