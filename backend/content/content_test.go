package content

import (
	"os"
	"path/filepath"
	"testing"
)

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

// TestIdleConfigMatchesLegacyHardcodes pins the offline-gain pack to the values
// previously hardcoded in characterPlugin.handleSelectCharacter, including the
// PerMinute arithmetic used for gold/exp accrual.
func TestIdleConfigMatchesLegacyHardcodes(t *testing.T) {
	cfg, err := LoadIdle()
	if err != nil {
		t.Fatalf("LoadIdle: %v", err)
	}

	if cfg.MinSeconds != 10 || cfg.CapSeconds != 86400 {
		t.Errorf("min/cap: got %v/%v, want 10/86400", cfg.MinSeconds, cfg.CapSeconds)
	}
	if cfg.LootTableID != "dummy_drops" {
		t.Errorf("loot table: got %q, want dummy_drops", cfg.LootTableID)
	}
	if cfg.RollChance != 0.50 || cfg.MaxRolls != 10 {
		t.Errorf("roll: got chance=%v max=%v, want 0.5/10", cfg.RollChance, cfg.MaxRolls)
	}
	if cfg.GoldPerMin != (IdleGainTerm{Base: 10, Stat: "STR", StatCoeff: 0.5}) {
		t.Errorf("gold_per_min: got %+v, want {10 STR 0.5}", cfg.GoldPerMin)
	}
	if cfg.ExpPerMin != (IdleGainTerm{Base: 15, Stat: "INT", StatCoeff: 0.8}) {
		t.Errorf("exp_per_min: got %+v, want {15 INT 0.8}", cfg.ExpPerMin)
	}

	// PerMinute must reproduce the old inline `(base + stat*coeff)` exactly.
	stats := map[string]float64{"STR": 20, "INT": 12}
	if got, want := cfg.GoldPerMin.PerMinute(stats), 10.0+20*0.5; got != want {
		t.Errorf("gold PerMinute: got %v, want %v", got, want)
	}
	if got, want := cfg.ExpPerMin.PerMinute(stats), 15.0+12*0.8; got != want {
		t.Errorf("exp PerMinute: got %v, want %v", got, want)
	}
}

// TestLootFallbackMatchesLegacyHardcodes pins the dummy_drops fallback table to
// the values previously hardcoded in loot.RollLoot (10..50 gold at 100%, one
// dragon_sword_0 at 100%).
func TestLootFallbackMatchesLegacyHardcodes(t *testing.T) {
	tables, err := LoadLootTables()
	if err != nil {
		t.Fatalf("LoadLootTables: %v", err)
	}

	dummy, ok := tables["dummy_drops"]
	if !ok {
		t.Fatal("dummy_drops fallback table missing")
	}
	want := []LootEntry{
		{ItemDefinitionID: "gold", Rate: 10000, MinQuantity: 10, MaxQuantity: 50},
		{ItemDefinitionID: "dragon_sword_0", Rate: 10000, MinQuantity: 1, MaxQuantity: 1},
	}
	if len(dummy.Entries) != len(want) {
		t.Fatalf("dummy_drops: got %d entries, want %d", len(dummy.Entries), len(want))
	}
	for i, e := range want {
		if dummy.Entries[i] != e {
			t.Errorf("entry %d: got %+v, want %+v", i, dummy.Entries[i], e)
		}
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

// TestContentOverride proves ZZRPG_CONTENT_DIR overrides embedded packs per file:
// a mobs.json placed on disk is used, while an unoverridden pack (classes) still
// falls back to the embedded default.
func TestContentOverride(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "mobs"), 0o755); err != nil {
		t.Fatal(err)
	}
	custom := `{"mobs":{"9999":{"level":50,"defense":1,"dex":1,"max_hp":1,"max_mp":1,"loot_table_id":"x","quest_tag":"y"}},"pvp":{"loot_table_id":"p","quest_tag":"q"}}`
	if err := os.WriteFile(filepath.Join(dir, "mobs", "mobs.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	SetOverrideDir(dir)
	defer SetOverrideDir("")

	mobs, err := LoadMobs()
	if err != nil {
		t.Fatalf("LoadMobs: %v", err)
	}
	if mobs.Mobs["9999"].Level != 50 {
		t.Errorf("expected the overridden mob (level 50), got %+v", mobs.Mobs["9999"])
	}

	// classes.json is not present on disk -> embedded fallback (WARRIOR STR = 15).
	classes, err := LoadClasses()
	if err != nil {
		t.Fatalf("LoadClasses: %v", err)
	}
	if classes["WARRIOR"]["STR"] != 15 {
		t.Errorf("non-overridden pack should fall back to embedded, got %v", classes["WARRIOR"])
	}
}

func TestSkillsPack(t *testing.T) {
	sk, err := LoadSkills()
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	fb, ok := sk["fireball"]
	if !ok {
		t.Fatal("fireball missing")
	}
	if fb.Class != "MAGE" || fb.Multiplier != 2.0 || fb.FlatDamage != 20 || fb.ManaCost != 25 {
		t.Errorf("unexpected fireball def: %+v", fb)
	}
	if sk["slash"].Class != "" {
		t.Errorf("slash should be usable by any class, got %q", sk["slash"].Class)
	}
}
