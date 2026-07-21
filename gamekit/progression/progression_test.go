package progression_test

import (
	"context"
	"testing"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/progression"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

var curve = progression.Curve{Base: 50, Exp: 2} // level N needs 50*N^2

func TestApply_MultiLevel(t *testing.T) {
	// from level 1: 50 (→2) + 200 (→3) + 40 leftover = 290
	lvl, xp, gained := progression.Apply(curve, 1, 0, 290)
	if lvl != 3 || xp != 40 || gained != 2 {
		t.Fatalf("lvl=%d xp=%d gained=%d, want 3/40/2", lvl, xp, gained)
	}
}

func TestGrantXP_PersistsAndFiresLevelUp(t *testing.T) {
	ctx := context.Background()
	h := hooks.New(nil)
	var levelups []int32
	hooks.AddAction(h, progression.HookLevelUp, 10, func(_ context.Context, lu progression.LevelUp) error {
		levelups = append(levelups, lu.NewLevel)
		return nil
	})
	svc := progression.NewService(component.NewMemStore[progression.Progression]("progression"), curve, h)

	p, gained, err := svc.GrantXP(ctx, 1, 290)
	if err != nil || p.Level != 3 || gained != 2 {
		t.Fatalf("grant: %+v gained=%d err=%v", p, gained, err)
	}
	if len(levelups) != 2 || levelups[0] != 2 || levelups[1] != 3 {
		t.Fatalf("expected level-up actions for 2 then 3, got %v", levelups)
	}
}

func TestGrantXP_HookBoostsXP(t *testing.T) {
	ctx := context.Background()
	h := hooks.New(nil)
	hooks.AddFilter(h, progression.HookXP, 10, func(_ context.Context, xp int64) int64 { return xp * 2 })
	svc := progression.NewService(component.NewMemStore[progression.Progression]("progression"), curve, h)

	// 25 xp doubled to 50 → exactly level 2
	p, gained, _ := svc.GrantXP(ctx, 1, 25)
	if p.Level != 2 || gained != 1 {
		t.Fatalf("xp boost should have leveled up: %+v gained=%d", p, gained)
	}
}
