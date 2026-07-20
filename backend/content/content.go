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

//go:embed formulas/derived_stats.json formulas/combat.json classes/classes.json mobs/mobs.json idle/offline.json idle/stages.json idle/lifeskills.json idle/generators.json idle/lifeskill_curve.json loot/tables.json skills/skills.json
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
// base stats. Both Primary and Secondary terms drive the zzstat resolver, which
// is the single source of truth for all stat math (there is no Go fallback).
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

// StageRequirement gates access to an idle stage: the character must be at least
// MinLevel and have at least Power (the combat-power scalar). A zero field is no
// constraint.
type StageRequirement struct {
	MinLevel int32   `json:"min_level"`
	Power    float64 `json:"power"`
}

// KillReward is the per-kill gold/exp a stage grants.
type KillReward struct {
	Gold float64 `json:"gold"`
	Exp  float64 `json:"exp"`
}

// EfficiencyCurve maps a character's power/difficulty ratio to a reward
// multiplier: below FloorRatio the character is too weak to make progress
// (efficiency 0); at or above it efficiency equals the ratio, clamped to
// CapRatio so massive over-levelling gives diminishing returns.
type EfficiencyCurve struct {
	FloorRatio float64 `json:"floor_ratio"`
	CapRatio   float64 `json:"cap_ratio"`
}

// Stage is a combat idle location. Offline gains scale with how the character's
// combat power compares to DifficultyPower (see EfficiencyCurve).
type Stage struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Requires        StageRequirement `json:"requires"`
	DifficultyPower float64          `json:"difficulty_power"`
	BaseKillsPerMin float64          `json:"base_kills_per_min"`
	KillReward      KillReward       `json:"kill_reward"`
	LootTableID     string           `json:"loot_table_id"`
	Efficiency      EfficiencyCurve  `json:"efficiency"`
}

// StagePack is the combat-idle content: a shared power-scalar weighting over
// derived stats plus the ordered stage list.
type StagePack struct {
	PowerWeights map[string]float64 `json:"power_weights"`
	Stages       []Stage            `json:"stages"`
}

// LifeskillYield is a gathering rate: Base units per minute plus PerLevel per
// skill level.
type LifeskillYield struct {
	Base     float64 `json:"base"`
	PerLevel float64 `json:"per_level"`
}

// LifeskillNode is one gatherable resource, unlocked at RequiresLevel and drawn
// with the given relative Weight among the unlocked nodes.
type LifeskillNode struct {
	ID               string `json:"id"`
	RequiresLevel    int32  `json:"requires_level"`
	ItemDefinitionID string `json:"item_definition_id"`
	Weight           int    `json:"weight"`
}

// Lifeskill is a gathering profession (mining, fishing, …). Offline yield scales
// with the character's level in this skill, not combat power — a separate
// progression axis.
type Lifeskill struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	YieldPerMin LifeskillYield  `json:"yield_per_min"`
	XPPerUnit   float64         `json:"xp_per_unit"`
	Nodes       []LifeskillNode `json:"nodes"`
}

// LifeskillPack is the gathering-idle content.
type LifeskillPack struct {
	Lifeskills []Lifeskill `json:"lifeskills"`
}

// LoadStages reads the embedded combat-idle stage pack.
func LoadStages() (*StagePack, error) {
	raw, err := readContent("idle/stages.json")
	if err != nil {
		return nil, fmt.Errorf("read stages.json: %w", err)
	}
	var p StagePack
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parse stages.json: %w", err)
	}
	return &p, nil
}

// MustLoadStages is LoadStages but panics on error.
func MustLoadStages() *StagePack {
	p, err := LoadStages()
	if err != nil {
		panic(err)
	}
	return p
}

// LoadLifeskills reads the embedded gathering-idle lifeskill pack.
func LoadLifeskills() (*LifeskillPack, error) {
	raw, err := readContent("idle/lifeskills.json")
	if err != nil {
		return nil, fmt.Errorf("read lifeskills.json: %w", err)
	}
	var p LifeskillPack
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parse lifeskills.json: %w", err)
	}
	return &p, nil
}

// MustLoadLifeskills is LoadLifeskills but panics on error.
func MustLoadLifeskills() *LifeskillPack {
	p, err := LoadLifeskills()
	if err != nil {
		panic(err)
	}
	return p
}

// Generator is an RTS-style passive resource producer: it emits Resource at
// BasePerMin plus PerLevel per unit of the State variable named LevelVar (e.g.
// "building_level"). It needs neither combat power nor a lifeskill level, which
// is what makes the idle framework general enough for pure resource games.
type Generator struct {
	ID         string  `json:"id"`
	Resource   string  `json:"resource"`
	BasePerMin float64 `json:"base_per_min"`
	PerLevel   float64 `json:"per_level"`
	LevelVar   string  `json:"level_var"`
}

// GeneratorPack is the RTS resource-generator content.
type GeneratorPack struct {
	Generators []Generator `json:"generators"`
}

// LoadGenerators reads the embedded RTS resource-generator pack.
func LoadGenerators() (*GeneratorPack, error) {
	raw, err := readContent("idle/generators.json")
	if err != nil {
		return nil, fmt.Errorf("read generators.json: %w", err)
	}
	var p GeneratorPack
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parse generators.json: %w", err)
	}
	return &p, nil
}

// MustLoadGenerators is LoadGenerators but panics on error.
func MustLoadGenerators() *GeneratorPack {
	p, err := LoadGenerators()
	if err != nil {
		panic(err)
	}
	return p
}

// LifeskillCurve parameterises the xp-per-level curve for lifeskills:
// xp to advance from level N is Base * N^Exp.
type LifeskillCurve struct {
	Base float64 `json:"base"`
	Exp  float64 `json:"exp"`
}

// LoadLifeskillCurve reads the embedded lifeskill leveling curve.
func LoadLifeskillCurve() (LifeskillCurve, error) {
	raw, err := readContent("idle/lifeskill_curve.json")
	if err != nil {
		return LifeskillCurve{}, fmt.Errorf("read lifeskill_curve.json: %w", err)
	}
	var c LifeskillCurve
	if err := json.Unmarshal(raw, &c); err != nil {
		return LifeskillCurve{}, fmt.Errorf("parse lifeskill_curve.json: %w", err)
	}
	return c, nil
}

// MustLoadLifeskillCurve is LoadLifeskillCurve but panics on error.
func MustLoadLifeskillCurve() LifeskillCurve {
	c, err := LoadLifeskillCurve()
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
