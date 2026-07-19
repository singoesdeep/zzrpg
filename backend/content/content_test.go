package content

import "testing"

// TestDerivedStatsMatchLegacyCoefficients pins the content values to the
// coefficients that were previously hardcoded in statclient (primary+secondary)
// and character.FallbackDerivedStats (primary only). If the JSON drifts, this
// fails at the content layer rather than surfacing as a subtle combat change.
func TestDerivedStatsMatchLegacyCoefficients(t *testing.T) {
	ds, err := LoadDerivedStats()
	if err != nil {
		t.Fatalf("LoadDerivedStats: %v", err)
	}

	want := map[string]map[string]float64{ // stat -> source ("" = const) -> factor
		"HP":        {"CON": 15},
		"MP":        {"INT": 10},
		"ATTACK":    {"STR": 2, "DEX": 0.5},
		"DEFENSE":   {"CON": 1, "STR": 0.2},
		"CRIT_RATE": {"": 5},
	}

	got := map[string]map[string]float64{}
	for _, group := range []map[string][]StatTerm{ds.Primary, ds.Secondary} {
		for stat, terms := range group {
			if got[stat] == nil {
				got[stat] = map[string]float64{}
			}
			for _, term := range terms {
				got[stat][term.Source] = term.Factor
			}
		}
	}

	for stat, terms := range want {
		for src, factor := range terms {
			if got[stat][src] != factor {
				t.Errorf("%s term source=%q: got %v, want %v", stat, src, got[stat][src], factor)
			}
		}
	}
	for stat, terms := range got {
		if len(terms) != len(want[stat]) {
			t.Errorf("%s: got %d terms, want %d", stat, len(terms), len(want[stat]))
		}
	}
}

// TestMobsMatchLegacyHardcodes pins the mob pack to the values previously
// hardcoded in combat (dummy 9999 stats) and killreward (table/quest tags).
func TestMobsMatchLegacyHardcodes(t *testing.T) {
	mobs, err := LoadMobs()
	if err != nil {
		t.Fatalf("LoadMobs: %v", err)
	}

	dummy, ok := mobs.Mobs["9999"]
	if !ok {
		t.Fatal("mob 9999 (training dummy) missing")
	}
	want := MobDef{
		Level: 10, Defense: 40, Dex: 10, MaxHP: 1000, MaxMP: 100,
		LootTableID: "dummy_drops", QuestTag: "wolf",
	}
	if dummy != want {
		t.Errorf("dummy mob: got %+v, want %+v", dummy, want)
	}

	if mobs.PvP.LootTableID != "player_drops" || mobs.PvP.QuestTag != "player" {
		t.Errorf("pvp defaults: got %+v, want {player_drops player}", mobs.PvP)
	}
}

func TestClassesMatchLegacyStats(t *testing.T) {
	classes, err := LoadClasses()
	if err != nil {
		t.Fatalf("LoadClasses: %v", err)
	}

	want := ClassDefs{
		"WARRIOR":  {"STR": 15, "INT": 5, "DEX": 10, "CON": 15},
		"MAGE":     {"STR": 5, "INT": 20, "DEX": 10, "CON": 10},
		"ASSASSIN": {"STR": 10, "INT": 5, "DEX": 20, "CON": 10},
		"SURA":     {"STR": 12, "INT": 12, "DEX": 10, "CON": 11},
	}

	if len(classes) != len(want) {
		t.Fatalf("got %d classes, want %d", len(classes), len(want))
	}
	for name, stats := range want {
		for stat, val := range stats {
			if classes[name][stat] != val {
				t.Errorf("%s.%s: got %v, want %v", name, stat, classes[name][stat], val)
			}
		}
	}
}
