package template_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/entity"
	"github.com/singoesdeep/zzrpg/gamekit/progression"
	"github.com/singoesdeep/zzrpg/gamekit/stats"
	"github.com/singoesdeep/zzrpg/gamekit/template"
)

// This test composes two very different entities — an RPG warrior and a city —
// from the same generic template machinery, wiring the stats and progression
// toolkits as component initializers.
func TestComposer_SpawnDifferentKinds(t *testing.T) {
	ctx := context.Background()
	entities := entity.NewMemRepo()

	statsStore := component.NewMemStore[stats.Stats]("stats")
	statsSvc := stats.NewService(statsStore, stats.NewFormulaResolver(map[string]stats.Formulas{
		"": {Primary: map[string][]stats.Term{"HP": {{Source: "CON", Factor: 15}}}},
	}), nil, nil)
	progStore := component.NewMemStore[progression.Progression]("progression")

	c := template.NewComposer(entities)
	// stats initializer: raw is a base-stat map
	c.RegisterComponent("stats", func(ctx context.Context, id int64, raw json.RawMessage) error {
		var base map[string]float64
		if err := json.Unmarshal(raw, &base); err != nil {
			return err
		}
		_, err := statsSvc.SetBase(ctx, id, base)
		return err
	})
	// progression initializer: raw is a Progression
	c.RegisterComponent("progression", func(ctx context.Context, id int64, raw json.RawMessage) error {
		var p progression.Progression
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return progStore.Set(ctx, id, p)
	})

	c.LoadTemplates(map[string]map[string]json.RawMessage{
		"warrior": {
			"stats":       json.RawMessage(`{"STR":15,"CON":15}`),
			"progression": json.RawMessage(`{"level":1,"xp":0}`),
		},
		"city": { // no combat stats, just a development stat — same machinery
			"stats": json.RawMessage(`{"CON":2}`),
		},
	})

	warrior, err := c.Spawn(ctx, "warrior", 1)
	if err != nil {
		t.Fatalf("spawn warrior: %v", err)
	}
	if st, ok, _ := statsSvc.Get(ctx, warrior.ID); !ok || st.Derived["HP"] != 225 { // CON15*15
		t.Fatalf("warrior HP wrong: %+v ok=%v", st, ok)
	}
	if p, _, _ := progStore.Get(ctx, warrior.ID); p.Level != 1 {
		t.Fatalf("warrior progression not attached: %+v", p)
	}

	city, err := c.Spawn(ctx, "city", 1)
	if err != nil {
		t.Fatalf("spawn city: %v", err)
	}
	if st, ok, _ := statsSvc.Get(ctx, city.ID); !ok || st.Derived["HP"] != 30 { // CON2*15
		t.Fatalf("city stats wrong: %+v", st)
	}
	// the city has no progression component
	if _, ok, _ := progStore.Get(ctx, city.ID); ok {
		t.Fatal("city should not have a progression component")
	}

	if _, err := c.Spawn(ctx, "dragon", 1); err == nil {
		t.Fatal("expected error spawning an unknown kind")
	}
}
