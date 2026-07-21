package gamedemo

import (
	"context"
	"testing"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/stats"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

func TestCombat_AttackAppliesDamageAndChainsHooks(t *testing.T) {
	ctx := context.Background()

	statsStore := component.NewMemStore[stats.Stats]("stats")
	statsSvc := stats.NewService(statsStore, stats.NewFormulaResolver(map[string]stats.Formulas{
		"": {Primary: map[string][]stats.Term{
			"ATTACK":  {{Source: "STR", Factor: 2}},
			"DEFENSE": {{Source: "CON", Factor: 1}},
		}},
	}), nil, nil)
	healthStore := component.NewMemStore[Health]("health")

	h := hooks.New(nil)
	var killed *Kill
	hooks.AddAction(h, HookKill, 10, func(_ context.Context, k Kill) error { killed = &k; return nil })
	hooks.AddFilter(h, HookDamage, 10, func(_ context.Context, d Damage) Damage { d.Amount += 5; return d })

	c := NewCombat(statsSvc, healthStore, h)

	_, _ = statsSvc.SetBase(ctx, 1, map[string]float64{"STR": 15}) // ATTACK 30
	_, _ = statsSvc.SetBase(ctx, 2, map[string]float64{"CON": 3})  // DEFENSE 3
	_ = healthStore.Set(ctx, 2, Health{Current: 30, Max: 30})

	res, err := c.Attack(ctx, 1, 2)
	if err != nil {
		t.Fatalf("attack: %v", err)
	}
	// damage = ATTACK 30 - DEFENSE 3 + weapon 5 = 32 ; HP 30 → dead
	if res.Damage != 32 || res.DefenderHP != 0 || !res.Killed {
		t.Fatalf("attack result wrong: %+v", res)
	}
	if killed == nil || killed.AttackerID != 1 || killed.VictimID != 2 {
		t.Fatalf("HookKill did not fire correctly: %+v", killed)
	}
}
