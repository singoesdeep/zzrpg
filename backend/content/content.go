// Package content holds the game's data-driven content packs (formulas, class
// definitions, ...) and loads them from embedded JSON. Moving these values out
// of Go source is the first step of the data-driven engine: game designers tune
// numbers in content/ rather than editing code, and there is a single source of
// truth for each rule instead of duplicated literals.
//
// Overriding without recompiling: set ZZRPG_CONTENT_DIR to a directory and any
// pack file present there (by its relative path, e.g. "mobs/mobs.json") is loaded
// instead of the embedded default. Files absent from the directory fall back to
// the embedded pack, so an operator or plugin can override just the files they
// care about. The override directory is read at package init, so it applies even
// to packs loaded into package-level vars at startup.
package content

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed formulas/derived_stats.json formulas/combat.json classes/classes.json mobs/mobs.json idle/offline.json loot/tables.json skills/skills.json
var files embed.FS

// overrideDir, when non-empty, is searched (by relative path) before the embedded
// pack. Initialised from ZZRPG_CONTENT_DIR so it is set before other packages load
// their content into package-level vars.
var overrideDir = os.Getenv("ZZRPG_CONTENT_DIR")

// SetOverrideDir sets the on-disk override directory (mainly for tests). Loaders
// called after this see the new value.
func SetOverrideDir(dir string) { overrideDir = dir }

// readContent returns a pack file, preferring an override in overrideDir over the
// embedded default. A file missing on disk falls back to embedded; any other disk
// error (e.g. permissions) is surfaced.
func readContent(name string) ([]byte, error) {
	if overrideDir != "" {
		b, err := os.ReadFile(filepath.Join(overrideDir, name))
		if err == nil {
			return b, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read override %s: %w", name, err)
		}
	}
	return files.ReadFile(name)
}

// StatTerm is one additive term of a derived stat. A term with an empty Source
// is a flat constant of Factor; otherwise it scales the named base stat by
// Factor.
type StatTerm struct {
	Source string  `json:"source"`
	Factor float64 `json:"factor"`
}

// DerivedStats describes how derived stats (HP, ATTACK, ...) are computed from
// base stats. Primary terms drive both the zzstat resolver and the Go fallback;
// Secondary terms drive only the zzstat resolver (the fallback intentionally
// uses primary terms only — see character.FallbackDerivedStats).
type DerivedStats struct {
	Primary   map[string][]StatTerm `json:"primary"`
	Secondary map[string][]StatTerm `json:"secondary"`
}

// ClassDefs maps a class name to its starting base stats.
type ClassDefs map[string]map[string]float64

// MobDef describes a non-player combat target: its combat stats plus the loot
// table it drops and the quest tag a kill counts toward.
type MobDef struct {
	Level       int32   `json:"level"`
	Defense     float64 `json:"defense"`
	Dex         float64 `json:"dex"`
	MaxHP       float64 `json:"max_hp"`
	MaxMP       float64 `json:"max_mp"`
	LootTableID string  `json:"loot_table_id"`
	QuestTag    string  `json:"quest_tag"`
}

// PvPDef holds the loot table and quest tag used when the defender is a player
// (a real character) rather than a defined mob.
type PvPDef struct {
	LootTableID string `json:"loot_table_id"`
	QuestTag    string `json:"quest_tag"`
}

// Mobs is the mob content pack: definitions keyed by string mob ID, plus the
// PvP defaults.
type Mobs struct {
	Mobs map[string]MobDef `json:"mobs"`
	PvP  PvPDef            `json:"pvp"`
}

// IdleGainTerm computes a per-minute idle reward: a flat Base plus the named
// base Stat scaled by StatCoeff. An empty Stat yields a flat Base.
type IdleGainTerm struct {
	Base      float64 `json:"base"`
	Stat      string  `json:"stat"`
	StatCoeff float64 `json:"stat_coeff"`
}

// PerMinute evaluates the term against a character's base stats. The arithmetic
// order (stat*coeff, then +base) is preserved so the result is bit-identical to
// the previously hardcoded offline-gain formula.
func (t IdleGainTerm) PerMinute(baseStats map[string]float64) float64 {
	return t.Base + baseStats[t.Stat]*t.StatCoeff
}

// IdleConfig describes offline ("idle") reward accrual: the min elapsed time
// before gains apply, the cap on elapsed time, the per-minute gold/exp terms,
// and the loot roll parameters (one roll per elapsed minute up to MaxRolls,
// each dropping from LootTableID with probability RollChance).
type IdleConfig struct {
	MinSeconds  float64      `json:"min_seconds"`
	CapSeconds  float64      `json:"cap_seconds"`
	GoldPerMin  IdleGainTerm `json:"gold_per_min"`
	ExpPerMin   IdleGainTerm `json:"exp_per_min"`
	LootTableID string       `json:"loot_table_id"`
	RollChance  float64      `json:"roll_chance"`
	MaxRolls    int          `json:"max_rolls"`
}

// LootEntry is one drop rule: an item that drops with probability Rate/10000,
// in a quantity uniformly drawn from [MinQuantity, MaxQuantity]. Mirrors
// loot.LootEntry as plain data (content must not import the loot package).
type LootEntry struct {
	ItemDefinitionID string `json:"item_definition_id"`
	Rate             int32  `json:"rate"`
	MinQuantity      int32  `json:"min_quantity"`
	MaxQuantity      int32  `json:"max_quantity"`
}

// LootTable is an ordered list of drop rules.
type LootTable struct {
	Entries []LootEntry `json:"entries"`
}

// LootTables is the fallback loot pack keyed by table ID, used when the DB has
// no row for a table (e.g. the training dummy's drops).
type LootTables map[string]LootTable

// Skill is a data-driven ability definition. An empty Class means any class may
// use it. Multiplier scales the attacker's damage and FlatDamage is added on top;
// ManaCost is spent from the attacker's session on use.
type Skill struct {
	Name       string  `json:"name"`
	Class      string  `json:"class"`
	Multiplier float64 `json:"multiplier"`
	FlatDamage float64 `json:"flat_damage"`
	ManaCost   float64 `json:"mana_cost"`
}

// Skills is the skill content pack, keyed by skill ID.
type Skills map[string]Skill

// LoadSkills reads the embedded skill-definition pack.
func LoadSkills() (Skills, error) {
	raw, err := readContent("skills/skills.json")
	if err != nil {
		return nil, fmt.Errorf("read skills.json: %w", err)
	}
	var s Skills
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse skills.json: %w", err)
	}
	return s, nil
}

// MustLoadSkills is LoadSkills but panics on error.
func MustLoadSkills() Skills {
	s, err := LoadSkills()
	if err != nil {
		panic(err)
	}
	return s
}

// LoadDerivedStats reads the embedded derived-stat formula pack.
func LoadDerivedStats() (*DerivedStats, error) {
	raw, err := readContent("formulas/derived_stats.json")
	if err != nil {
		return nil, fmt.Errorf("read derived_stats.json: %w", err)
	}
	var ds DerivedStats
	if err := json.Unmarshal(raw, &ds); err != nil {
		return nil, fmt.Errorf("parse derived_stats.json: %w", err)
	}
	return &ds, nil
}

// LoadClasses reads the embedded class-definition pack.
func LoadClasses() (ClassDefs, error) {
	raw, err := readContent("classes/classes.json")
	if err != nil {
		return nil, fmt.Errorf("read classes.json: %w", err)
	}
	var cd ClassDefs
	if err := json.Unmarshal(raw, &cd); err != nil {
		return nil, fmt.Errorf("parse classes.json: %w", err)
	}
	return cd, nil
}

// CombatFormulaJSON returns the raw combat-formula JSON, fed to
// zzstat's EvaluateCombatEx. Panics if the embedded file is missing (a
// build-time programmer error).
func CombatFormulaJSON() string {
	raw, err := readContent("formulas/combat.json")
	if err != nil {
		panic(fmt.Errorf("read combat.json: %w", err))
	}
	return string(raw)
}

// LoadMobs reads the embedded mob-definition pack.
func LoadMobs() (*Mobs, error) {
	raw, err := readContent("mobs/mobs.json")
	if err != nil {
		return nil, fmt.Errorf("read mobs.json: %w", err)
	}
	var m Mobs
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse mobs.json: %w", err)
	}
	return &m, nil
}

// LoadLootTables reads the embedded fallback loot-table pack.
func LoadLootTables() (LootTables, error) {
	raw, err := readContent("loot/tables.json")
	if err != nil {
		return nil, fmt.Errorf("read tables.json: %w", err)
	}
	var t LootTables
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("parse tables.json: %w", err)
	}
	return t, nil
}

// MustLoadLootTables is LoadLootTables but panics on error.
func MustLoadLootTables() LootTables {
	t, err := LoadLootTables()
	if err != nil {
		panic(err)
	}
	return t
}

// LoadIdle reads the embedded offline/idle reward config pack.
func LoadIdle() (*IdleConfig, error) {
	raw, err := readContent("idle/offline.json")
	if err != nil {
		return nil, fmt.Errorf("read offline.json: %w", err)
	}
	var c IdleConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse offline.json: %w", err)
	}
	return &c, nil
}

// MustLoadIdle is LoadIdle but panics on error.
func MustLoadIdle() *IdleConfig {
	c, err := LoadIdle()
	if err != nil {
		panic(err)
	}
	return c
}

// MustLoadMobs is LoadMobs but panics on error.
func MustLoadMobs() *Mobs {
	m, err := LoadMobs()
	if err != nil {
		panic(err)
	}
	return m
}

// MustLoadDerivedStats is LoadDerivedStats but panics on error. The content is
// embedded, so a failure is a build-time programmer error, surfaced at startup.
func MustLoadDerivedStats() *DerivedStats {
	ds, err := LoadDerivedStats()
	if err != nil {
		panic(err)
	}
	return ds
}

// MustLoadClasses is LoadClasses but panics on error.
func MustLoadClasses() ClassDefs {
	cd, err := LoadClasses()
	if err != nil {
		panic(err)
	}
	return cd
}
