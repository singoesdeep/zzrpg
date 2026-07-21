package buildings

import (
	"context"
	"testing"

	eidle "github.com/singoesdeep/zzrpg/sdk/engine/idle"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	gidle "github.com/singoesdeep/zzrpg/gamekit/idle"
)

func TestProducerScalesByInjectedLevel(t *testing.T) {
	p := producer{id: "sawmill", b: defaultCatalog().Buildings["sawmill"]}

	if p.Unlocked(eidle.State{}) {
		t.Fatal("level-0 sawmill should be locked")
	}
	st := eidle.State{Vars: map[string]float64{"sawmill_level": 4}}
	if !p.Unlocked(st) {
		t.Fatal("level-4 sawmill should be unlocked")
	}
	// wood = level 4 * rate 2 * 10min = 80.
	out := p.Produce(10, st, nil)
	if out.Amounts["wood"] != 80 {
		t.Fatalf("wood = %d, want 80", out.Amounts["wood"])
	}
}

func TestUpgradeCostDoubles(t *testing.T) {
	b := defaultCatalog().Buildings["sawmill"] // BaseCost 50
	cases := map[int32]int64{0: 50, 1: 100, 2: 200, 3: 400}
	for level, want := range cases {
		if got := b.UpgradeCost(level); got != want {
			t.Fatalf("UpgradeCost(%d) = %d, want %d", level, got, want)
		}
	}
}

// TestStateFilterInjectsPerBuildingLevels proves the extension seam end to end:
// the filter reads THIS plugin's own component and sets one State var per
// catalog entry — exactly what idlekit's engine consumes with no changes.
func TestStateFilterInjectsPerBuildingLevels(t *testing.T) {
	ctx := context.Background()
	levels := component.NewMemStore[Levels]("idlekit_building")
	_ = levels.Set(ctx, 1, Levels{Levels: map[string]int32{"sawmill": 2, "farm": 5}})

	filter := defaultCatalog().stateFilter(levels)
	se := filter(ctx, gidle.StateEvent{EntityID: 1, State: eidle.State{Vars: map[string]float64{"power": 10}}})

	if se.State.Vars["sawmill_level"] != 2 || se.State.Vars["farm_level"] != 5 {
		t.Fatalf("injected vars = %+v, want sawmill_level=2 farm_level=5", se.State.Vars)
	}
	if se.State.Vars["power"] != 10 {
		t.Fatal("filter must not clobber existing vars")
	}
}
