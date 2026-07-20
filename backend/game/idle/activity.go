package idle

import (
	"sort"

	"github.com/singoesdeep/zzrpg/backend/content"
)

// ActivityType is the kind of thing a character does while idle: fighting at a
// combat Stage (rewards scale with combat power) or gathering with a Lifeskill
// (rewards scale with that skill's level).
type ActivityType string

const (
	ActivityStage     ActivityType = "stage"
	ActivityLifeskill ActivityType = "lifeskill"
)

// Assignment is what a character is currently doing offline.
type Assignment struct {
	Type ActivityType `json:"type"`
	ID   string       `json:"id"`
}

// Catalog is the loaded, indexed idle-activity content: the combat stages (with
// the shared power-scalar weights) and the gathering lifeskills. It is read-only
// after construction and safe for concurrent reads.
type Catalog struct {
	weights        map[string]float64
	stages         map[string]content.Stage
	lifeskills     map[string]content.Lifeskill
	generators     map[string]content.Generator
	stageOrder     []string
	lifeskillOrder []string
	generatorOrder []string
}

// NewCatalog loads the embedded stage, lifeskill, and generator packs.
func NewCatalog() *Catalog {
	sp := content.MustLoadStages()
	lp := content.MustLoadLifeskills()
	gp := content.MustLoadGenerators()

	c := &Catalog{
		weights:    sp.PowerWeights,
		stages:     make(map[string]content.Stage, len(sp.Stages)),
		lifeskills: make(map[string]content.Lifeskill, len(lp.Lifeskills)),
		generators: make(map[string]content.Generator, len(gp.Generators)),
	}
	for _, s := range sp.Stages {
		c.stages[s.ID] = s
		c.stageOrder = append(c.stageOrder, s.ID)
	}
	for _, l := range lp.Lifeskills {
		c.lifeskills[l.ID] = l
		c.lifeskillOrder = append(c.lifeskillOrder, l.ID)
	}
	for _, g := range gp.Generators {
		c.generators[g.ID] = g
		c.generatorOrder = append(c.generatorOrder, g.ID)
	}
	return c
}

// Stage looks up a combat stage by id.
func (c *Catalog) Stage(id string) (content.Stage, bool) { s, ok := c.stages[id]; return s, ok }

// Lifeskill looks up a lifeskill by id.
func (c *Catalog) Lifeskill(id string) (content.Lifeskill, bool) {
	l, ok := c.lifeskills[id]
	return l, ok
}

// Generator looks up an RTS resource generator by id.
func (c *Catalog) Generator(id string) (content.Generator, bool) {
	g, ok := c.generators[id]
	return g, ok
}

// Power reduces a character's derived stats to a single combat-power scalar
// using the content-defined weights (sum of stat*weight). Missing stats count
// as zero, so the formula is fully data-driven.
func (c *Catalog) Power(derived map[string]float64) float64 {
	var p float64
	for stat, w := range c.weights {
		p += derived[stat] * w
	}
	return p
}

// StageOutcome is the computed (not yet applied) result of idling at a stage.
type StageOutcome struct {
	Locked      bool    // requirements not met — no gains
	Efficiency  float64 // reward multiplier from the power/difficulty ratio
	KillsPerMin float64
	Gold        int64
	Exp         int64
	Rolls       int // number of loot rolls earned (one per kill)
}

// efficiency maps the power/difficulty ratio through the stage's curve.
func efficiency(power, difficulty float64, curve content.EfficiencyCurve) float64 {
	if difficulty <= 0 {
		return curve.CapRatio
	}
	ratio := power / difficulty
	if ratio < curve.FloorRatio {
		return 0
	}
	if curve.CapRatio > 0 && ratio > curve.CapRatio {
		return curve.CapRatio
	}
	return ratio
}

// StageUnlocked reports whether a character of the given level and power meets a
// stage's requirements.
func StageUnlocked(s content.Stage, power float64, level int32) bool {
	return level >= s.Requires.MinLevel && power >= s.Requires.Power
}

// StageGains computes offline gains for elapsedMin minutes at a stage given the
// character's combat power and level. A locked stage yields nothing.
func StageGains(s content.Stage, power float64, level int32, elapsedMin float64) StageOutcome {
	if elapsedMin <= 0 || !StageUnlocked(s, power, level) {
		return StageOutcome{Locked: !StageUnlocked(s, power, level)}
	}
	eff := efficiency(power, s.DifficultyPower, s.Efficiency)
	kpm := s.BaseKillsPerMin * eff
	totalKills := kpm * elapsedMin
	return StageOutcome{
		Efficiency:  eff,
		KillsPerMin: kpm,
		Gold:        int64(totalKills * s.KillReward.Gold),
		Exp:         int64(totalKills * s.KillReward.Exp),
		Rolls:       int(totalKills),
	}
}

// Gathered is a quantity of one resource produced by a lifeskill.
type Gathered struct {
	NodeID           string
	ItemDefinitionID string
	Quantity         int
}

// LifeskillOutcome is the computed result of idling at a lifeskill.
type LifeskillOutcome struct {
	Units   int        // total resources gathered
	XP      int64      // lifeskill xp earned
	Gathers []Gathered // per-resource breakdown, sorted by node id
}

// LifeskillGains computes offline gathering for elapsedMin minutes at skillLevel.
// Yield scales with the skill level; each gathered unit is assigned to one of the
// unlocked nodes by relative weight, drawn from rng (rng() in [0,1)). maxUnits
// caps the number of units (<=0 means uncapped).
func LifeskillGains(l content.Lifeskill, skillLevel int32, elapsedMin float64, rng func() float64, maxUnits int) LifeskillOutcome {
	if elapsedMin <= 0 {
		return LifeskillOutcome{}
	}
	ypm := l.YieldPerMin.Base + l.YieldPerMin.PerLevel*float64(skillLevel)
	units := int(ypm * elapsedMin)
	if units < 0 {
		units = 0
	}
	if maxUnits > 0 && units > maxUnits {
		units = maxUnits
	}

	// Unlocked nodes and their cumulative weights.
	var nodes []content.LifeskillNode
	var totalWeight int
	for _, n := range l.Nodes {
		if skillLevel >= n.RequiresLevel && n.Weight > 0 {
			nodes = append(nodes, n)
			totalWeight += n.Weight
		}
	}

	out := LifeskillOutcome{Units: units, XP: int64(float64(units) * l.XPPerUnit)}
	if units == 0 || totalWeight == 0 {
		out.Units = 0
		out.XP = 0
		return out
	}

	byNode := make(map[string]*Gathered)
	for i := 0; i < units; i++ {
		pick := int(rng() * float64(totalWeight))
		if pick >= totalWeight {
			pick = totalWeight - 1
		}
		var chosen content.LifeskillNode
		acc := 0
		for _, n := range nodes {
			acc += n.Weight
			if pick < acc {
				chosen = n
				break
			}
		}
		g := byNode[chosen.ID]
		if g == nil {
			g = &Gathered{NodeID: chosen.ID, ItemDefinitionID: chosen.ItemDefinitionID}
			byNode[chosen.ID] = g
		}
		g.Quantity++
	}

	for _, g := range byNode {
		out.Gathers = append(out.Gathers, *g)
	}
	sort.Slice(out.Gathers, func(i, j int) bool { return out.Gathers[i].NodeID < out.Gathers[j].NodeID })
	return out
}
