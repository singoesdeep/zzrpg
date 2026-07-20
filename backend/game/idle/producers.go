package idle

import (
	"github.com/singoesdeep/zzrpg/backend/content"
	eidle "github.com/singoesdeep/zzrpg/backend/engine/idle"
)

// State variable names read by the producers.
const (
	VarPower         = "power"          // combat-power scalar (stages)
	VarLevel         = "level"          // character level (stage gating)
	VarSkillLevel    = "skill_level"    // lifeskill level (gathering)
	VarBuildingLevel = "building_level" // generator level (RTS)
)

// Output ledger keys the game Service knows how to apply.
const (
	AmountGold      = "gold"
	AmountExp       = "exp"
	AmountLootRolls = "loot_rolls" // number of rolls owed against the stage's loot table
)

// StageProducer adapts a combat Stage to the engine idle framework: output
// scales with the character's combat power relative to the stage difficulty.
type StageProducer struct{ S content.Stage }

func (p StageProducer) Unlocked(s eidle.State) bool {
	return StageUnlocked(p.S, s.Get(VarPower), int32(s.Get(VarLevel)))
}

func (p StageProducer) Produce(elapsedMin float64, s eidle.State, _ func() float64) eidle.Output {
	g := StageGains(p.S, s.Get(VarPower), int32(s.Get(VarLevel)), elapsedMin)
	var o eidle.Output
	o.Add(AmountGold, g.Gold)
	o.Add(AmountExp, g.Exp)
	o.Add(AmountLootRolls, int64(g.Rolls))
	return o
}

// LifeskillProducer adapts a gathering Lifeskill: output scales with the
// character's level in that skill and is emitted as concrete resource drops.
type LifeskillProducer struct{ L content.Lifeskill }

func (p LifeskillProducer) Unlocked(eidle.State) bool { return true }

func (p LifeskillProducer) Produce(elapsedMin float64, s eidle.State, rng func() float64) eidle.Output {
	g := LifeskillGains(p.L, int32(s.Get(VarSkillLevel)), elapsedMin, rng, 0)
	var o eidle.Output
	o.Add(p.L.ID+"_xp", g.XP)
	for _, gathered := range g.Gathers {
		o.AddDrop(gathered.ItemDefinitionID, gathered.Quantity)
	}
	return o
}

// GeneratorProducer adapts an RTS resource generator: a flat/level-scaled rate,
// with no dependency on combat power or a lifeskill — the pure-resource case.
type GeneratorProducer struct{ G content.Generator }

func (p GeneratorProducer) Unlocked(eidle.State) bool { return true }

func (p GeneratorProducer) Produce(elapsedMin float64, s eidle.State, _ func() float64) eidle.Output {
	rate := p.G.BasePerMin + p.G.PerLevel*s.Get(p.G.LevelVar)
	var o eidle.Output
	o.Add(p.G.Resource, int64(rate*elapsedMin))
	return o
}

// StageAssignment is a convenience constructor for a combat-stage assignment.
func StageAssignment(id string) Assignment { return Assignment{Type: ActivityStage, ID: id} }

// LifeskillAssignment is a convenience constructor for a lifeskill assignment.
func LifeskillAssignment(id string) Assignment {
	return Assignment{Type: ActivityLifeskill, ID: id}
}

// BuildState assembles the accrual State from a character's scaling inputs so
// callers need not import the engine framework directly.
func BuildState(power float64, level, skillLevel, buildingLevel int32) eidle.State {
	return eidle.State{Vars: map[string]float64{
		VarPower:         power,
		VarLevel:         float64(level),
		VarSkillLevel:    float64(skillLevel),
		VarBuildingLevel: float64(buildingLevel),
	}}
}

// BuildRegistry registers every stage, lifeskill, and generator in the catalog
// as a Producer keyed by its content id, ready for the accrual driver.
func (c *Catalog) BuildRegistry() *eidle.Registry {
	r := eidle.NewRegistry()
	for id, s := range c.stages {
		r.Register(id, StageProducer{S: s})
	}
	for id, l := range c.lifeskills {
		r.Register(id, LifeskillProducer{L: l})
	}
	for id, g := range c.generators {
		r.Register(id, GeneratorProducer{G: g})
	}
	return r
}
