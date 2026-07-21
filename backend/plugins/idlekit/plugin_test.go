package idlekit

import (
	"testing"

	eidle "github.com/singoesdeep/zzrpg/sdk/engine/idle"
)

// TestExampleActivitiesProduce pins the shipped activities' math: training turns
// power+level into gold+exp, gathering turns time+level into ore.
func TestExampleActivitiesProduce(t *testing.T) {
	st := eidle.State{Vars: map[string]float64{"power": 100, "level": 3}}

	// training over 10 min: gold = (100*0.1 + 3)*10 = 130, exp = (3*2)*10 = 60.
	out := training{}.Produce(10, st, nil)
	if out.Amounts["gold"] != 130 || out.Amounts["exp"] != 60 {
		t.Fatalf("training out = %+v, want gold 130 exp 60", out.Amounts)
	}

	// gathering over 10 min: ore = (1 + 3*0.1)*10 = 13.
	out = gathering{}.Produce(10, st, nil)
	if out.Amounts["ore"] != 13 {
		t.Fatalf("gathering ore = %d, want 13", out.Amounts["ore"])
	}
}

func TestPowerSumsDerivedStats(t *testing.T) {
	if got := power(map[string]float64{"ATTACK": 30, "DEFENSE": 15, "HP": 200}); got != 245 {
		t.Fatalf("power = %v, want 245", got)
	}
	if power(nil) != 0 {
		t.Fatal("power(nil) should be 0")
	}
}
