package loot

import (
	"context"
	"reflect"
	"testing"

	"github.com/singoesdeep/zzrpg/backend/engine/hooks"
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

func (m *mockLootRepository) ListLootTables(ctx context.Context, limit, offset int) ([]LootTable, error) {
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

// TestLootRollingIsDeterministicWithSeed proves the injected RNG (WithSeed) makes
// loot rolls reproducible: two services seeded identically must produce the same
// sequence of drop/quantity outcomes across both the drop-chance and quantity
// rolls. This is the payoff of making the RNG injectable.
func TestLootRollingIsDeterministicWithSeed(t *testing.T) {
	// A 50% table with a wide quantity range exercises both RNG draws per roll.
	build := func() LootService {
		s := NewLootService(newMockLootRepository(), WithSeed(42))
		_ = s.CreateLootTable(context.Background(), &LootTable{
			ID: "t",
			Entries: []LootEntry{
				{ItemDefinitionID: "gold", Rate: 5000, MinQuantity: 1, MaxQuantity: 1000},
			},
		})
		return s
	}

	// Encode each roll's outcome: -1 for no drop, else the rolled quantity.
	sequence := func(s LootService) []int32 {
		out := make([]int32, 0, 30)
		for i := 0; i < 30; i++ {
			drops, err := s.RollLoot(context.Background(), "t")
			if err != nil {
				t.Fatalf("RollLoot: %v", err)
			}
			if len(drops) == 0 {
				out = append(out, -1)
			} else {
				out = append(out, drops[0].Quantity)
			}
		}
		return out
	}

	a, b := sequence(build()), sequence(build())
	if !reflect.DeepEqual(a, b) {
		t.Errorf("same seed produced different sequences:\n a=%v\n b=%v", a, b)
	}
	// Guard against a degenerate all-same sequence that would pass trivially.
	varied := false
	for _, v := range a {
		if v != a[0] {
			varied = true
			break
		}
	}
	if !varied {
		t.Fatalf("sequence did not vary, RNG not exercised: %v", a)
	}
}

// TestLootRollHookFilter proves a plugin filter can adjust rolled drops: a
// HookRoll filter appends a bonus item and it appears in the result.
func TestLootRollHookFilter(t *testing.T) {
	hks := hooks.New(nil)
	hooks.AddFilter(hks, HookRoll, 10, func(_ context.Context, r LootRoll) LootRoll {
		r.Items = append(r.Items, DroppedItem{ItemDefinitionID: "event_token", Quantity: 1})
		return r
	})
	service := NewLootService(newMockLootRepository(), WithSeed(1), WithHooks(hks))

	drops, err := service.RollLoot(context.Background(), "dummy_drops")
	if err != nil {
		t.Fatalf("roll: %v", err)
	}
	var found bool
	for _, d := range drops {
		if d.ItemDefinitionID == "event_token" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the hook-added bonus drop, got %+v", drops)
	}
}
