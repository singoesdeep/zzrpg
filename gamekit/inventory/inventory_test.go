package inventory_test

import (
	"context"
	"testing"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/inventory"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

func newSvc(h *hooks.Hooks) *inventory.Service {
	return inventory.NewService(component.NewMemStore[inventory.Inventory]("inventory"), h)
}

func TestAddStackAndRemove(t *testing.T) {
	ctx := context.Background()
	s := newSvc(nil)

	_ = s.AddItem(ctx, 1, inventory.Item{ItemID: "wood", Quantity: 10})
	_ = s.AddItem(ctx, 1, inventory.Item{ItemID: "wood", Quantity: 5}) // stacks
	if n, _ := s.Count(ctx, 1, "wood"); n != 15 {
		t.Fatalf("expected 15 wood, got %d", n)
	}

	ok, _ := s.RemoveItem(ctx, 1, "wood", 20) // more than held
	if ok {
		t.Fatal("remove of 20 from 15 should fail")
	}
	ok, _ = s.RemoveItem(ctx, 1, "wood", 15) // exact → stack removed
	if !ok {
		t.Fatal("remove of 15 should succeed")
	}
	if n, _ := s.Count(ctx, 1, "wood"); n != 0 {
		t.Fatalf("expected 0 wood, got %d", n)
	}
}

func TestAddItemHook(t *testing.T) {
	ctx := context.Background()
	h := hooks.New(nil)
	// a "double drops" plugin
	hooks.AddFilter(h, inventory.HookAddItem, 10, func(_ context.Context, it inventory.Item) inventory.Item {
		it.Quantity *= 2
		return it
	})
	s := newSvc(h)
	_ = s.AddItem(ctx, 1, inventory.Item{ItemID: "gem", Quantity: 3})
	if n, _ := s.Count(ctx, 1, "gem"); n != 6 {
		t.Fatalf("expected hook to double to 6, got %d", n)
	}
}
