// Package idle is gamekit's idle-accrual toolkit: the base a game builds an idle
// system on. It marries the engine's game-agnostic Producer framework
// (engine/idle: an Activity that turns elapsed time + entity State into an
// opaque Output) with gamekit's entities, scheduler, and reward toolkits.
//
// The framework provides the mechanism and the integration — an Assignment
// component, an accrual Engine, a ready TickSystem with offline catch-up, and a
// default Output router into economy/progression/inventory. The CONTENT — the
// concrete activities (combat stages, gathering lifeskills, buildings, RTS
// generators) — is written by game developers as `engine/idle.Producer`
// implementations registered on the Engine. Buildings and lifeskills are
// plugins, not core concepts.
package idle

import (
	"context"
	"time"

	eidle "github.com/singoesdeep/zzrpg/sdk/engine/idle"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/economy"
	"github.com/singoesdeep/zzrpg/gamekit/inventory"
	"github.com/singoesdeep/zzrpg/gamekit/progression"
	"github.com/singoesdeep/zzrpg/gamekit/world"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

// AssignmentComponent is the component name/index an idle entity is queried by.
const AssignmentComponent = "idle_assignment"

// HookOutput is a Filter over an activity's Output before it is applied — the
// seam a plugin uses to add a rested bonus, an event multiplier, or a cap,
// without the activity or the engine knowing about it.
const HookOutput = "idle.output"

// Assignment is the component: which registered activity an entity is running.
type Assignment struct {
	ActivityID string `json:"activity_id"`
}

// StateFunc builds the numeric inputs an activity scales its output by, read
// from the entity's live game state (power, skill level, building level, …). It
// is the game-specific read-bridge into the accrual.
type StateFunc func(ctx context.Context, entityID int64) (eidle.State, error)

// ApplyFunc consumes an activity's Output for an entity (credit gold, grant xp,
// drop loot, or reflect onto a legacy aggregate). DefaultApplier covers the
// common gamekit routing; a game supplies its own to reach elsewhere.
type ApplyFunc func(ctx context.Context, entityID int64, out eidle.Output) error

// Engine drives idle accrual: it resolves an entity's assigned activity, builds
// its state, produces the output, and applies it — with a HookOutput filter in
// between.
type Engine struct {
	registry *eidle.Registry
	assign   component.Store[Assignment]
	stateFor StateFunc
	apply    ApplyFunc
	hooks    *hooks.Hooks
	rng      func() float64
}

// Deps configure the engine. Registry and Assign are required; StateFor defaults
// to empty state, Apply to a no-op, Hooks/Rng may be nil.
type Deps struct {
	Registry *eidle.Registry
	Assign   component.Store[Assignment]
	StateFor StateFunc
	Apply    ApplyFunc
	Hooks    *hooks.Hooks
	Rng      func() float64
}

// NewEngine builds the accrual engine.
func NewEngine(d Deps) *Engine {
	e := &Engine{registry: d.Registry, assign: d.Assign, stateFor: d.StateFor, apply: d.Apply, hooks: d.Hooks, rng: d.Rng}
	if e.stateFor == nil {
		e.stateFor = func(context.Context, int64) (eidle.State, error) { return eidle.State{}, nil }
	}
	if e.apply == nil {
		e.apply = func(context.Context, int64, eidle.Output) error { return nil }
	}
	return e
}

// Activities lists the registered activity ids (for an "activities" endpoint).
func (e *Engine) Activities() []string { return e.registry.IDs() }

// Assign points an entity at a registered activity (error if unknown).
func (e *Engine) Assign(ctx context.Context, entityID int64, activityID string) error {
	if _, ok := e.registry.Get(activityID); !ok {
		return &UnknownActivityError{ID: activityID}
	}
	return e.assign.Set(ctx, entityID, Assignment{ActivityID: activityID})
}

// Current returns an entity's active activity id, if assigned.
func (e *Engine) Current(ctx context.Context, entityID int64) (string, bool, error) {
	a, ok, err := e.assign.Get(ctx, entityID)
	if err != nil || !ok {
		return "", false, err
	}
	return a.ActivityID, a.ActivityID != "", nil
}

// Accrue runs the entity's assigned activity over elapsedMin minutes: it builds
// the state, produces the output (unless the activity is locked), filters it
// through HookOutput, and applies it. Returns the applied output and whether
// anything ran.
func (e *Engine) Accrue(ctx context.Context, entityID int64, elapsedMin float64) (eidle.Output, bool, error) {
	id, ok, err := e.Current(ctx, entityID)
	if err != nil || !ok {
		return eidle.Output{}, false, err
	}
	p, ok := e.registry.Get(id)
	if !ok {
		return eidle.Output{}, false, nil
	}
	st, err := e.stateFor(ctx, entityID)
	if err != nil {
		return eidle.Output{}, false, err
	}
	if !p.Unlocked(st) {
		return eidle.Output{}, false, nil
	}
	out := p.Produce(elapsedMin, st, e.rng)
	if e.hooks != nil {
		out = hooks.ApplyFilters(e.hooks, ctx, HookOutput, out)
	}
	if err := e.apply(ctx, entityID, out); err != nil {
		return eidle.Output{}, false, err
	}
	return out, true, nil
}

// UnknownActivityError is returned by Assign for an unregistered activity id.
type UnknownActivityError struct{ ID string }

func (e *UnknownActivityError) Error() string { return "idle: unknown activity " + e.ID }

// System adapts the Engine to a gamekit TickSystem: it ticks every assigned
// entity on its interval (and on demand for offline catch-up), clamping elapsed
// time to the accrual window.
type System struct {
	engine *Engine
	every  time.Duration
	minSec float64
	capSec float64
}

// NewSystem builds the tick system. minSeconds gates tiny windows (nothing
// accrues below it); capSeconds bounds a long offline window (<=0 = uncapped).
func NewSystem(engine *Engine, every time.Duration, minSeconds, capSeconds float64) System {
	return System{engine: engine, every: every, minSec: minSeconds, capSec: capSeconds}
}

func (System) Name() string              { return "idle" }
func (s System) Interval() time.Duration { return s.every }
func (System) Query() []string           { return []string{AssignmentComponent} }

func (s System) Tick(ctx context.Context, id int64, _ *world.World, elapsed time.Duration) error {
	min, ok := eidle.Window(elapsed.Seconds(), s.minSec, s.capSec)
	if !ok {
		return nil
	}
	_, _, err := s.engine.Accrue(ctx, id, min)
	return err
}

// DefaultApplier routes an activity's Output through gamekit's built-in reward
// toolkits: the amount named expKey grants progression XP, every other amount is
// earned into the economy wallet as that currency, and each Drop is added to the
// inventory. A game that must reach a legacy aggregate (e.g. a character row)
// supplies its own ApplyFunc instead.
func DefaultApplier(econ *economy.Service, prog *progression.Service, inv *inventory.Service, expKey string) ApplyFunc {
	return func(ctx context.Context, id int64, out eidle.Output) error {
		for name, amount := range out.Amounts {
			if amount == 0 {
				continue
			}
			if name == expKey {
				if prog != nil {
					if _, _, err := prog.GrantXP(ctx, id, amount); err != nil {
						return err
					}
				}
				continue
			}
			if econ != nil {
				if _, err := econ.Earn(ctx, id, name, amount); err != nil {
					return err
				}
			}
		}
		if inv != nil {
			for _, d := range out.Drops {
				if err := inv.AddItem(ctx, id, inventory.Item{ItemID: d.ID, Quantity: int32(d.Quantity)}); err != nil {
					return err
				}
			}
		}
		return nil
	}
}
