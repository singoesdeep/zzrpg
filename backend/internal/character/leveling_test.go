package character

import "testing"

// These tests exercise the progression rules that previously lived inside the
// AddRewards SQL transaction and could only be tested against a live database.

func TestApplyExperience(t *testing.T) {
	// Level 1 requires 1*1*100 = 100 EXP; level 2 requires 400.
	cases := []struct {
		name            string
		level           int32
		curExp, gainExp int64
		wantLevel       int32
		wantExp         int64
		wantLeveledUp   bool
	}{
		{"no level up", 1, 0, 50, 1, 50, false},
		{"exact single level", 1, 0, 100, 2, 0, true},
		{"carry remainder", 1, 0, 150, 2, 50, true},
		{"multi level", 1, 0, 100 + 400, 3, 0, true},
		{"no gain", 5, 30, 0, 5, 30, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotLevel, gotExp, gotUp := ApplyExperience(c.level, c.curExp, c.gainExp)
			if gotLevel != c.wantLevel || gotExp != c.wantExp || gotUp != c.wantLeveledUp {
				t.Fatalf("ApplyExperience(%d,%d,%d) = (%d,%d,%v), want (%d,%d,%v)",
					c.level, c.curExp, c.gainExp, gotLevel, gotExp, gotUp,
					c.wantLevel, c.wantExp, c.wantLeveledUp)
			}
		})
	}
}

func TestApplyLevelUpStatGains(t *testing.T) {
	base := map[string]float64{"STR": 10, "INT": 5, "DEX": 8, "CON": 12}
	ApplyLevelUpStatGains(base, 3) // +2 per level * 3 = +6 each

	for _, s := range []string{"STR", "INT", "DEX", "CON"} {
		want := map[string]float64{"STR": 16, "INT": 11, "DEX": 14, "CON": 18}[s]
		if base[s] != want {
			t.Errorf("%s = %v, want %v", s, base[s], want)
		}
	}

	// No-op when no levels were gained.
	before := map[string]float64{"STR": 10}
	ApplyLevelUpStatGains(before, 0)
	if before["STR"] != 10 {
		t.Errorf("expected no change for 0 levels, got %v", before["STR"])
	}
}

func TestFallbackDerivedStats(t *testing.T) {
	base := map[string]float64{"STR": 15, "INT": 5, "DEX": 10, "CON": 15}
	got := FallbackDerivedStats(base)
	want := map[string]float64{"HP": 225, "MP": 50, "ATTACK": 30, "DEFENSE": 15, "CRIT_RATE": 5}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}
