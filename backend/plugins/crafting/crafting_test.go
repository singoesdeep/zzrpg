package crafting

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/singoesdeep/zzrpg/sdk/engine/registry"
	"github.com/singoesdeep/zzrpg/sdk/engine/store"

	"github.com/singoesdeep/zzrpg/backend/game/inventory"
)

// fakeStore satisfies store.Store; only Exec (item-def seeding) is exercised.
type fakeStore struct{}

func (fakeStore) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (fakeStore) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }
func (fakeStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (f fakeStore) WithinTx(ctx context.Context, fn func(store.Querier) error) error {
	return fn(f)
}

type mockWallet struct{ bal map[string]int64 }

func (m *mockWallet) Balances(context.Context, int64) (map[string]int64, error) { return m.bal, nil }
func (m *mockWallet) Credit(_ context.Context, _ int64, res string, amt int64) error {
	m.bal[res] += amt
	return nil
}

type mockGold struct{ gold, spent int64 }

func (m *mockGold) SpendGold(_ context.Context, _ int64, amt int64) (bool, error) {
	if m.gold < amt {
		return false, nil
	}
	m.gold -= amt
	m.spent += amt
	return true, nil
}

type mockInv struct{ added []*inventory.InventoryItem }

func (m *mockInv) AddItem(_ context.Context, it *inventory.InventoryItem) error {
	m.added = append(m.added, it)
	return nil
}

const recipes = `{
  "plank": {"id":"plank","name":"Plank","cost":{"wood":10},"gold_cost":0,"output":{"item_id":"crafted_plank","name":"Plank","slot_type":"NONE","quantity":1}},
  "shield": {"id":"shield","name":"Shield","cost":{"wood":20,"metal":10},"gold_cost":50,"output":{"item_id":"crafted_shield","name":"Shield","slot_type":"SHIELD","quantity":1}}
}`

func newSvc(t *testing.T, w *mockWallet, g *mockGold, inv *mockInv) *Service {
	svc, err := NewService(context.Background(), fakeStore{}, registry.New(), w, g, inv, []byte(recipes))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func TestCraft_ResourceSink(t *testing.T) {
	ctx := context.Background()
	w := &mockWallet{bal: map[string]int64{"wood": 25}}
	g := &mockGold{}
	inv := &mockInv{}
	svc := newSvc(t, w, g, inv)

	res, err := svc.Craft(ctx, 1, "plank")
	if err != nil {
		t.Fatalf("craft plank: %v", err)
	}
	if res.ItemID != "crafted_plank" || w.bal["wood"] != 15 { // 25 - 10 consumed
		t.Fatalf("plank craft wrong: item=%s wood=%d", res.ItemID, w.bal["wood"])
	}
	if len(inv.added) != 1 || inv.added[0].ItemDefinitionID != "crafted_plank" {
		t.Fatalf("crafted item not granted: %+v", inv.added)
	}
}

func TestCraft_InsufficientResources(t *testing.T) {
	w := &mockWallet{bal: map[string]int64{"wood": 5}}
	svc := newSvc(t, w, &mockGold{}, &mockInv{})
	if _, err := svc.Craft(context.Background(), 1, "plank"); !errors.Is(err, ErrInsufficientRes) {
		t.Fatalf("expected ErrInsufficientRes, got %v", err)
	}
}

func TestCraft_MixedCostAndInsufficientGold(t *testing.T) {
	ctx := context.Background()
	// enough resources, but no gold for the shield (costs 50 gold)
	w := &mockWallet{bal: map[string]int64{"wood": 100, "metal": 100}}
	poor := &mockGold{gold: 10}
	svc := newSvc(t, w, poor, &mockInv{})
	if _, err := svc.Craft(ctx, 1, "shield"); !errors.Is(err, ErrInsufficientGold) {
		t.Fatalf("expected ErrInsufficientGold, got %v", err)
	}
	if w.bal["wood"] != 100 || poor.spent != 0 {
		t.Fatalf("nothing should be spent on a failed craft: wood=%d spent=%d", w.bal["wood"], poor.spent)
	}

	// fund gold and succeed: consumes wood 20 + metal 10 + gold 50
	rich := &mockGold{gold: 200}
	inv := &mockInv{}
	svc2 := newSvc(t, w, rich, inv)
	res, err := svc2.Craft(ctx, 1, "shield")
	if err != nil {
		t.Fatalf("craft shield: %v", err)
	}
	if res.ItemID != "crafted_shield" || w.bal["wood"] != 80 || w.bal["metal"] != 90 || rich.spent != 50 {
		t.Fatalf("shield craft wrong: %+v wood=%d metal=%d spent=%d", res, w.bal["wood"], w.bal["metal"], rich.spent)
	}
}

func TestRecipes_Affordability(t *testing.T) {
	w := &mockWallet{bal: map[string]int64{"wood": 10}} // enough for plank, not shield
	views, err := newSvc(t, w, &mockGold{}, &mockInv{}).Recipes(context.Background(), 1)
	if err != nil {
		t.Fatalf("recipes: %v", err)
	}
	got := map[string]bool{}
	for _, v := range views {
		got[v.ID] = v.Affordable
	}
	if !got["plank"] || got["shield"] {
		t.Fatalf("affordability wrong: %+v", got)
	}
}
