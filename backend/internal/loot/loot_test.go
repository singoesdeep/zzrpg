package loot

import (
	"context"
	"testing"
)

type mockLootRepository struct {
	tables map[string]*LootTable
}

func newMockLootRepository() *mockLootRepository {
	return &mockLootRepository{tables: make(map[string]*LootTable)}
}

func (m *mockLootRepository) CreateLootTable(ctx context.Context, lt *LootTable) error {
	m.tables[lt.ID] = lt
	return nil
}

func (m *mockLootRepository) GetLootTable(ctx context.Context, id string) (*LootTable, error) {
	lt, ok := m.tables[id]
	if !ok {
		return nil, ErrLootTableNotFound
	}
	return lt, nil
}

func (m *mockLootRepository) ListLootTables(ctx context.Context) ([]LootTable, error) {
	var list []LootTable
	for _, lt := range m.tables {
		list = append(list, *lt)
	}
	return list, nil
}

func TestLootRollingLogic(t *testing.T) {
	repo := newMockLootRepository()
	service := NewLootService(repo)

	// Create test loot table
	lt := &LootTable{
		ID:          "test_table",
		Description: "Drops sword and gold",
		Entries: []LootEntry{
			{ItemDefinitionID: "gold", Rate: 10000, MinQuantity: 100, MaxQuantity: 200},   // 100% drop rate
			{ItemDefinitionID: "dragon_sword_0", Rate: 0, MinQuantity: 1, MaxQuantity: 1}, // 0% drop rate
		},
	}
	_ = service.CreateLootTable(context.Background(), lt)

	// Roll loot
	drops, err := service.RollLoot(context.Background(), "test_table")
	if err != nil {
		t.Fatalf("failed to roll loot: %v", err)
	}

	// Verify drops
	if len(drops) != 1 {
		t.Fatalf("expected exactly 1 drop (gold), got %d", len(drops))
	}

	if drops[0].ItemDefinitionID != "gold" {
		t.Errorf("expected drop item definition to be gold, got %s", drops[0].ItemDefinitionID)
	}

	if drops[0].Quantity < 100 || drops[0].Quantity > 200 {
		t.Errorf("expected gold quantity between 100 and 200, got %d", drops[0].Quantity)
	}

	// Test fallback for dummy_drops
	dummyDrops, err := service.RollLoot(context.Background(), "dummy_drops")
	if err != nil {
		t.Fatalf("failed to roll fallback loot: %v", err)
	}

	if len(dummyDrops) != 2 {
		t.Errorf("expected 2 drops for dummy fallback, got %d", len(dummyDrops))
	}
}
