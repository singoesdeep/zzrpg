package idle_test

import (
	"context"
	"testing"

	"github.com/singoesdeep/zzrpg/backend/content"
	"github.com/singoesdeep/zzrpg/backend/game/idle"
)

func TestXPForLevel(t *testing.T) {
	curve := content.LifeskillCurve{Base: 50, Exp: 2.0} // 50 * N^2
	if got := idle.XPForLevel(curve, 1); got != 50 {
		t.Fatalf("level 1 = %d, want 50", got)
	}
	if got := idle.XPForLevel(curve, 3); got != 450 { // 50*9
		t.Fatalf("level 3 = %d, want 450", got)
	}
	if got := idle.XPForLevel(curve, 0); got != 50 { // clamped to 1
		t.Fatalf("level 0 clamps to 1 (=50), got %d", got)
	}
}

func TestApplyLifeskillXP(t *testing.T) {
	curve := content.LifeskillCurve{Base: 50, Exp: 2.0}

	// Not enough to level: level 1 needs 50.
	lvl, xp, up := idle.ApplyLifeskillXP(curve, 1, 0, 30)
	if up || lvl != 1 || xp != 30 {
		t.Fatalf("30 xp: lvl=%d xp=%d up=%v, want 1/30/false", lvl, xp, up)
	}

	// Exactly one level: 50 -> level 2, 0 leftover.
	lvl, xp, up = idle.ApplyLifeskillXP(curve, 1, 0, 50)
	if !up || lvl != 2 || xp != 0 {
		t.Fatalf("50 xp: lvl=%d xp=%d up=%v, want 2/0/true", lvl, xp, up)
	}

	// Multi-level: from level 1, 50 (->2) + 200 (->3) + 40 leftover = 290.
	lvl, xp, up = idle.ApplyLifeskillXP(curve, 1, 0, 290)
	if !up || lvl != 3 || xp != 40 {
		t.Fatalf("290 xp: lvl=%d xp=%d up=%v, want 3/40/true", lvl, xp, up)
	}
}

func TestMemRepos_Roundtrip(t *testing.T) {
	ctx := context.Background()

	// Assignment: default absent, then set/get.
	ar := idle.NewMemAssignmentRepo()
	if _, ok, _ := ar.Get(ctx, 1); ok {
		t.Fatal("expected no assignment initially")
	}
	_ = ar.Set(ctx, 1, idle.StageAssignment("goblin_forest"))
	if a, ok, _ := ar.Get(ctx, 1); !ok || a.ID != "goblin_forest" || a.Type != idle.ActivityStage {
		t.Fatalf("assignment roundtrip failed: %+v ok=%v", a, ok)
	}

	// Lifeskill: default level 1, then upsert.
	lr := idle.NewMemLifeskillRepo()
	if s, _ := lr.Get(ctx, 1, "mining"); s.Level != 1 || s.XP != 0 {
		t.Fatalf("default lifeskill should be level 1/0, got %+v", s)
	}
	_ = lr.Upsert(ctx, 1, idle.LifeskillState{SkillID: "mining", Level: 7, XP: 120})
	if s, _ := lr.Get(ctx, 1, "mining"); s.Level != 7 || s.XP != 120 {
		t.Fatalf("lifeskill upsert failed: %+v", s)
	}

	// Building: default 0, then set/list.
	br := idle.NewMemBuildingRepo()
	if lvl, _ := br.Get(ctx, 1, "lumber_mill"); lvl != 0 {
		t.Fatalf("default building level should be 0, got %d", lvl)
	}
	_ = br.Set(ctx, 1, "lumber_mill", 3)
	if lvls, _ := br.Levels(ctx, 1); lvls["lumber_mill"] != 3 {
		t.Fatalf("building levels failed: %+v", lvls)
	}

	// Wallet: credit accumulates.
	wr := idle.NewMemWalletRepo()
	_ = wr.Credit(ctx, 1, "wood", 10)
	_ = wr.Credit(ctx, 1, "wood", 5)
	if b, _ := wr.Balances(ctx, 1); b["wood"] != 15 {
		t.Fatalf("wallet credit accumulation failed: %+v", b)
	}
}
