package stats_test

import (
	"context"
	"testing"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/stats"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

func resolver() stats.StatResolver {
	return stats.NewFormulaResolver(map[string]stats.Formulas{
		"": {
			Primary: map[string][]stats.Term{
				"HP":     {{Source: "CON", Factor: 15}},
				"ATTACK": {{Source: "STR", Factor: 2}},
			},
			Secondary: map[string][]stats.Term{
				"ATTACK": {{Source: "DEX", Factor: 0.5}},
			},
		},
	})
}

func TestSetBase_DerivesStats(t *testing.T) {
	ctx := context.Background()
	svc := stats.NewService(component.NewMemStore[stats.Stats]("stats"), resolver(), nil, nil)

	st, err := svc.SetBase(ctx, 1, map[string]float64{"STR": 10, "CON": 5, "DEX": 4})
	if err != nil {
		t.Fatalf("SetBase: %v", err)
	}
	// HP = CON*15 = 75 ; ATTACK = STR*2 + DEX*0.5 = 22
	if st.Derived["HP"] != 75 || st.Derived["ATTACK"] != 22 {
		t.Fatalf("derived wrong: %+v", st.Derived)
	}
	got, ok, _ := svc.Get(ctx, 1)
	if !ok || got.Base["STR"] != 10 || got.Derived["HP"] != 75 {
		t.Fatalf("persisted stats wrong: %+v", got)
	}
}

func TestDeriveHook_InjectsBonus(t *testing.T) {
	ctx := context.Background()
	h := hooks.New(nil)
	// a plugin adds +100 HP (e.g. a global buff)
	hooks.AddFilter(h, stats.HookDerive, 10, func(_ context.Context, d map[string]float64) map[string]float64 {
		d["HP"] += 100
		return d
	})
	svc := stats.NewService(component.NewMemStore[stats.Stats]("stats"), resolver(), nil, h)

	st, _ := svc.SetBase(ctx, 1, map[string]float64{"CON": 5})
	if st.Derived["HP"] != 175 { // 75 + 100 from the hook
		t.Fatalf("expected hook to add 100 HP (175), got %v", st.Derived["HP"])
	}
}
