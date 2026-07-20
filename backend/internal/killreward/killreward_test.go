package killreward_test

import (
	"context"
	"testing"

	"github.com/singoesdeep/zzrpg/backend/internal/creature"
	"github.com/singoesdeep/zzrpg/backend/internal/killreward"
	"github.com/singoesdeep/zzrpg/backend/internal/loot"
)

type mockCreatures struct {
	creatures map[int64]creature.Creature
}

func (m *mockCreatures) Resolve(ctx context.Context, id int64) (creature.Creature, bool, error) {
	c, ok := m.creatures[id]
	return c, ok, nil
}

type mockQuestProgress struct {
	updatedTarget string
}

func (m *mockQuestProgress) UpdateQuestProgress(ctx context.Context, charID int32, actionType string, target string, amount int32) error {
	m.updatedTarget = target
	return nil
}

type mockLootRoll struct {
	tableID string
	items   []loot.DroppedItem
}

func (m *mockLootRoll) RollLoot(ctx context.Context, tableID string) ([]loot.DroppedItem, error) {
	m.tableID = tableID
	return m.items, nil
}

func TestRewardKill_UsesCreatureMetadata(t *testing.T) {
	creatures := &mockCreatures{
		creatures: map[int64]creature.Creature{
			9999: {
				ID:          9999,
				Kind:        creature.KindMob,
				LootTableID: "goblin_drops",
				QuestTag:    "goblin",
			},
		},
	}
	quests := &mockQuestProgress{}
	loots := &mockLootRoll{items: []loot.DroppedItem{{ItemDefinitionID: "gold", Quantity: 100}}}

	svc := killreward.New(creatures, nil, quests, loots, nil, nil)
	drops := svc.RewardKill(context.Background(), 101, 9999)

	if quests.updatedTarget != "goblin" {
		t.Errorf("expected quest target 'goblin', got %q", quests.updatedTarget)
	}
	if loots.tableID != "goblin_drops" {
		t.Errorf("expected loot table 'goblin_drops', got %q", loots.tableID)
	}
	if len(drops) != 1 || drops[0].ItemDefinitionID != "gold" {
		t.Errorf("unexpected drops: %+v", drops)
	}
}
