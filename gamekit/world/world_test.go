package world_test

import (
	"context"
	"testing"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/entity"
	"github.com/singoesdeep/zzrpg/gamekit/world"
)

// Two example component types — proving the framework imposes no game concepts.
type Stats struct{ Power float64 }
type Production struct{ RatePerMin float64 }

func TestComponentStore_Roundtrip(t *testing.T) {
	ctx := context.Background()
	s := component.NewMemStore[Stats]("stats")

	if _, ok, _ := s.Get(ctx, 1); ok {
		t.Fatal("expected no component initially")
	}
	_ = s.Set(ctx, 1, Stats{Power: 42})
	if v, ok, _ := s.Get(ctx, 1); !ok || v.Power != 42 {
		t.Fatalf("roundtrip failed: %+v ok=%v", v, ok)
	}
	if has, _ := s.Has(ctx, 1); !has {
		t.Fatal("Has should be true")
	}
	_ = s.Set(ctx, 3, Stats{Power: 7})
	if ids, _ := s.EntityIDs(ctx); len(ids) != 2 || ids[0] != 1 || ids[1] != 3 {
		t.Fatalf("EntityIDs wrong: %v", ids)
	}
	_ = s.Delete(ctx, 1)
	if has, _ := s.Has(ctx, 1); has {
		t.Fatal("Has should be false after delete")
	}
}

func TestWorld_ComponentIndexedQuery(t *testing.T) {
	ctx := context.Background()
	entities := entity.NewMemRepo()
	w := world.New(entities)

	statsStore := component.NewMemStore[Stats]("stats")
	prodStore := component.NewMemStore[Production]("production")
	w.Register(statsStore)
	w.Register(prodStore)

	// A character: has stats only.
	hero, _ := entities.Create(ctx, "character", 1)
	_ = statsStore.Set(ctx, hero.ID, Stats{Power: 100})

	// A city: has both stats (development) and production.
	city, _ := entities.Create(ctx, "city", 1)
	_ = statsStore.Set(ctx, city.ID, Stats{Power: 5})
	_ = prodStore.Set(ctx, city.ID, Production{RatePerMin: 12})

	// A pure generator: production only.
	gen, _ := entities.Create(ctx, "generator", 0)
	_ = prodStore.Set(ctx, gen.ID, Production{RatePerMin: 3})

	// Query: entities with a production component → city + generator.
	producers, err := w.Query(ctx, "production")
	if err != nil {
		t.Fatalf("query production: %v", err)
	}
	if len(producers) != 2 {
		t.Fatalf("expected 2 producers, got %v", producers)
	}

	// Query: entities with BOTH stats AND production → only the city.
	both, err := w.Query(ctx, "stats", "production")
	if err != nil {
		t.Fatalf("query both: %v", err)
	}
	if len(both) != 1 || both[0] != city.ID {
		t.Fatalf("expected only the city, got %v", both)
	}

	// Unknown component is an error.
	if _, err := w.Query(ctx, "nope"); err == nil {
		t.Fatal("expected error for unknown component")
	}

	// Typed iteration via With.
	pairs, err := world.With(ctx, prodStore)
	if err != nil {
		t.Fatalf("With: %v", err)
	}
	var total float64
	for _, p := range pairs {
		total += p.Value.RatePerMin
	}
	if total != 15 { // 12 + 3
		t.Fatalf("expected total rate 15, got %v", total)
	}
}
