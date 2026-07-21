// Package buildings is a CONTENT plugin, not core: it demonstrates the pattern
// gamekit/idle is built for — a game developer adds a new idle activity type
// (buildings, upgradeable per character) entirely from a plugin, without
// touching idlekit or the engine. It registers per-building Producers on the
// shared "idleActivities" registry idlekit exposes, and injects each building's
// level into activity State via idlekit's HookState filter — the extension seam
// designed exactly for this.
package buildings

import (
	"context"

	eidle "github.com/singoesdeep/zzrpg/sdk/engine/idle"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	gidle "github.com/singoesdeep/zzrpg/gamekit/idle"
)

// Catalog is the building content: id -> economics. A real game loads this from
// JSON; the pilot hardcodes two buildings to keep the example legible.
type Catalog struct {
	Buildings map[string]Building
}

// Building is one building type: what it produces per level, and what the next
// level costs in gold.
type Building struct {
	Resource     string
	RatePerLevel float64 // resource per level per minute
	BaseCost     int64   // gold cost of level 1; cost doubles per level
}

func defaultCatalog() Catalog {
	return Catalog{Buildings: map[string]Building{
		"sawmill": {Resource: "wood", RatePerLevel: 2, BaseCost: 50},
		"farm":    {Resource: "food", RatePerLevel: 3, BaseCost: 40},
	}}
}

// UpgradeCost is the gold cost to go from level to level+1 (doubling curve).
func (b Building) UpgradeCost(level int32) int64 {
	cost := b.BaseCost
	for i := int32(0); i < level; i++ {
		cost *= 2
	}
	return cost
}

// Levels is the component: a character's building levels, id -> level.
type Levels struct {
	Levels map[string]int32 `json:"levels"`
}

// producer is the engine/idle.Producer for one building: it only runs once
// levelled (Unlocked), and scales output by the level HookState injected.
type producer struct {
	id string
	b  Building
}

func (p producer) stateKey() string { return p.id + "_level" }

func (p producer) Unlocked(s eidle.State) bool { return s.Get(p.stateKey()) > 0 }

func (p producer) Produce(min float64, s eidle.State, _ func() float64) eidle.Output {
	var o eidle.Output
	o.Add(p.b.Resource, int64(s.Get(p.stateKey())*p.b.RatePerLevel*min))
	return o
}

// register adds one Producer per catalog entry to the shared activity registry
// — the only touchpoint with idlekit's world.
func (c Catalog) register(activities *eidle.Registry) {
	for id, b := range c.Buildings {
		activities.Register(id, producer{id: id, b: b})
	}
}

// stateFilter builds the HookState filter: for the entity in the event, it
// reads the levels component and sets every building's "<id>_level" input —
// the seam that lets this plugin's own data reach the engine's accrual without
// idlekit or the engine knowing buildings exist.
func (c Catalog) stateFilter(levels component.Store[Levels]) func(context.Context, gidle.StateEvent) gidle.StateEvent {
	return func(ctx context.Context, se gidle.StateEvent) gidle.StateEvent {
		lv, _, err := levels.Get(ctx, se.EntityID)
		if err != nil {
			return se
		}
		if se.State.Vars == nil {
			se.State.Vars = map[string]float64{}
		}
		for id := range c.Buildings {
			se.State.Vars[id+"_level"] = float64(lv.Levels[id])
		}
		return se
	}
}
