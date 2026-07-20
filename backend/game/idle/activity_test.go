package idle

import (
	"testing"

	"github.com/singoesdeep/zzrpg/backend/content"
)

func TestCatalog_LoadsEmbeddedContent(t *testing.T) {
	c := NewCatalog()
	if _, ok := c.Stage("goblin_forest"); !ok {
		t.Fatal("expected goblin_forest stage in catalog")
	}
	if _, ok := c.Lifeskill("mining"); !ok {
		t.Fatal("expected mining lifeskill in catalog")
	}
}

func TestCatalog_Power(t *testing.T) {
	c := &Catalog{weights: map[string]float64{"ATTACK": 1.0, "DEFENSE": 0.5, "HP": 0.1}}
	derived := map[string]float64{"ATTACK": 100, "DEFENSE": 40, "HP": 300, "IGNORED": 999}
	// 100*1 + 40*0.5 + 300*0.1 = 100 + 20 + 30 = 150
	if got := c.Power(derived); got != 150 {
		t.Fatalf("Power = %v, want 150", got)
	}
}

func stage() content.Stage {
	return content.Stage{
		ID: "s", Requires: content.StageRequirement{MinLevel: 5, Power: 120},
		DifficultyPower: 150, BaseKillsPerMin: 10,
		KillReward: content.KillReward{Gold: 4, Exp: 6},
		Efficiency: content.EfficiencyCurve{FloorRatio: 0.6, CapRatio: 2.5},
	}
}

func TestStageGains_LockedBelowRequirements(t *testing.T) {
	s := stage()
	// under-level
	if out := StageGains(s, 500, 3, 60); !out.Locked || out.Gold != 0 {
		t.Fatalf("under-level should be locked with no gold, got %+v", out)
	}
	// under-power
	if out := StageGains(s, 50, 10, 60); !out.Locked {
		t.Fatalf("under-power should be locked, got %+v", out)
	}
}

func TestStageGains_FloorZeroesTooWeak(t *testing.T) {
	s := stage()
	// Isolate the efficiency floor from the unlock gate: meet the requirements
	// (level 10 >= 5, power 95 >= req 90) but keep the power/difficulty ratio
	// below the floor (95/200 = 0.475 < 0.6) so gains zero out.
	s.Requires.Power = 90
	s.DifficultyPower = 200
	out := StageGains(s, 95, 10, 60)
	if out.Locked {
		t.Fatalf("should be unlocked (meets req), got locked")
	}
	if out.Efficiency != 0 || out.Gold != 0 || out.Exp != 0 {
		t.Fatalf("below floor ratio should yield zero efficiency/gains, got %+v", out)
	}
}

func TestStageGains_ScalesWithPower(t *testing.T) {
	s := stage()
	weak := StageGains(s, 150, 10, 60)   // ratio 1.0
	strong := StageGains(s, 300, 10, 60) // ratio 2.0
	if !(strong.Gold > weak.Gold && strong.Exp > weak.Exp) {
		t.Fatalf("stronger character should earn more: weak=%+v strong=%+v", weak, strong)
	}
	// ratio 1.0: kpm = 10, kills over 60min = 600, gold = 600*4 = 2400
	if weak.Gold != 2400 || weak.Exp != 3600 {
		t.Fatalf("ratio 1.0 gains wrong: %+v", weak)
	}
}

func TestStageGains_CapRatio(t *testing.T) {
	s := stage() // cap 2.5
	capped := StageGains(s, 100000, 10, 1)
	if capped.Efficiency != 2.5 {
		t.Fatalf("efficiency should clamp to cap 2.5, got %v", capped.Efficiency)
	}
}

func lifeskill() content.Lifeskill {
	return content.Lifeskill{
		ID: "mining", YieldPerMin: content.LifeskillYield{Base: 3, PerLevel: 0.4}, XPPerUnit: 2,
		Nodes: []content.LifeskillNode{
			{ID: "copper", RequiresLevel: 1, ItemDefinitionID: "copper_ore", Weight: 70},
			{ID: "iron", RequiresLevel: 10, ItemDefinitionID: "iron_ore", Weight: 30},
		},
	}
}

func TestLifeskillGains_YieldScalesWithLevel(t *testing.T) {
	l := lifeskill()
	fixed := func() float64 { return 0.0 } // always first node
	low := LifeskillGains(l, 1, 60, fixed, 0)
	high := LifeskillGains(l, 20, 60, fixed, 0)
	// level 1: (3 + 0.4*1)*60 = 204 ; level 20: (3 + 0.4*20)*60 = 660
	if low.Units != 204 || high.Units != 660 {
		t.Fatalf("yield units wrong: low=%d high=%d", low.Units, high.Units)
	}
	if low.XP != 408 { // 204 * 2
		t.Fatalf("xp wrong: %d", low.XP)
	}
}

func TestLifeskillGains_NodeGatingByLevel(t *testing.T) {
	l := lifeskill()
	// rng biased to the high end so it would pick iron if unlocked.
	high := func() float64 { return 0.99 }
	// level 5: iron (requires 10) locked -> everything is copper.
	out := LifeskillGains(l, 5, 10, high, 0)
	for _, g := range out.Gathers {
		if g.ItemDefinitionID != "copper_ore" {
			t.Fatalf("iron should be locked at level 5, got %s", g.ItemDefinitionID)
		}
	}
	// level 15: iron unlocked; with rng 0.99 (top of range) it should land on iron.
	out = LifeskillGains(l, 15, 1, high, 0)
	foundIron := false
	for _, g := range out.Gathers {
		if g.ItemDefinitionID == "iron_ore" {
			foundIron = true
		}
	}
	if !foundIron {
		t.Fatalf("iron should be reachable at level 15, gathers=%+v", out.Gathers)
	}
}

func TestLifeskillGains_MaxUnitsCap(t *testing.T) {
	l := lifeskill()
	out := LifeskillGains(l, 20, 60, func() float64 { return 0 }, 50)
	if out.Units != 50 {
		t.Fatalf("units should cap at 50, got %d", out.Units)
	}
}
