package loot

import (
	"context"
	"errors"
	"math/rand"
	"testing"

	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

func table(entries ...Entry) EntriesFor {
	return func(context.Context, string) ([]Entry, error) { return entries, nil }
}

func TestRollDeterministicWithSeed(t *testing.T) {
	ctx := context.Background()
	entries := table(Entry{ItemID: "sword", Rate: RateScale, Min: 1, Max: 1}) // always drops, qty 1
	r := NewRoller(entries, WithSeed(42))

	drops, err := r.Roll(ctx, "chest")
	if err != nil {
		t.Fatalf("roll: %v", err)
	}
	if len(drops) != 1 || drops[0] != (Drop{ItemID: "sword", Quantity: 1}) {
		t.Fatalf("drops = %+v, want one sword", drops)
	}
}

func TestRollNeverDropsAtZeroRate(t *testing.T) {
	ctx := context.Background()
	entries := table(Entry{ItemID: "sword", Rate: 0, Min: 1, Max: 1})
	r := NewRoller(entries, WithRand(rand.New(rand.NewSource(1))))

	for i := 0; i < 50; i++ {
		drops, _ := r.Roll(ctx, "chest")
		if len(drops) != 0 {
			t.Fatalf("rate-0 entry dropped: %+v", drops)
		}
	}
}

func TestRollQuantityWithinRange(t *testing.T) {
	ctx := context.Background()
	entries := table(Entry{ItemID: "gold", Rate: RateScale, Min: 5, Max: 10})
	r := NewRoller(entries, WithRand(rand.New(rand.NewSource(7))))

	for i := 0; i < 100; i++ {
		drops, _ := r.Roll(ctx, "chest")
		if len(drops) != 1 {
			t.Fatalf("expected exactly one drop, got %+v", drops)
		}
		q := drops[0].Quantity
		if q < 5 || q > 10 {
			t.Fatalf("quantity %d out of [5,10]", q)
		}
	}
}

func TestHookRollCanAppendAndRemove(t *testing.T) {
	ctx := context.Background()
	h := hooks.New(nil)
	hooks.AddFilter(h, HookRoll, 10, func(_ context.Context, roll Roll) Roll {
		roll.Items = append(roll.Items, Drop{ItemID: "bonus_gem", Quantity: 1})
		return roll
	})
	entries := table(Entry{ItemID: "sword", Rate: RateScale, Min: 1, Max: 1})
	r := NewRoller(entries, WithSeed(1), WithHooks(h))

	drops, _ := r.Roll(ctx, "chest")
	if len(drops) != 2 || drops[1].ItemID != "bonus_gem" {
		t.Fatalf("drops = %+v, want sword + bonus_gem", drops)
	}
}

func TestEntriesForErrorPropagates(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("table not found")
	r := NewRoller(func(context.Context, string) ([]Entry, error) { return nil, wantErr })

	if _, err := r.Roll(ctx, "missing"); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}
