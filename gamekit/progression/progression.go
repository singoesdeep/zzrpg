// Package progression is the gamekit leveling toolkit: an xp/level component and
// a data-driven curve, with hooks so plugins can boost xp or react to level-ups.
// It is generic — a character levels up, but so could a city or a building.
package progression

import (
	"context"
	"math"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

const (
	// HookXP is a Filter over the xp amount before it is applied (xp boosts).
	HookXP = "progression.xp"
	// HookLevelUp is an Action fired once per level gained.
	HookLevelUp = "progression.levelup"
)

// Progression is the component stored per entity.
type Progression struct {
	Level int32 `json:"level"`
	XP    int64 `json:"xp"`
}

// Curve parameterises xp-per-level: xp to advance from level N is Base * N^Exp.
type Curve struct {
	Base float64 `json:"base"`
	Exp  float64 `json:"exp"`
}

// XPForLevel is the xp required to advance from the given level.
func XPForLevel(c Curve, level int32) int64 {
	if level < 1 {
		level = 1
	}
	return int64(c.Base * math.Pow(float64(level), c.Exp))
}

// Apply adds gained xp to a level/xp pair and returns the result and how many
// levels were gained. Pure domain rule.
func Apply(c Curve, level int32, xp, gained int64) (newLevel int32, newXP int64, levelsGained int32) {
	newLevel, newXP = level, xp+gained
	if newLevel < 1 {
		newLevel = 1
	}
	for {
		req := XPForLevel(c, newLevel)
		if req > 0 && newXP >= req {
			newXP -= req
			newLevel++
			levelsGained++
		} else {
			break
		}
	}
	return newLevel, newXP, levelsGained
}

// LevelUp is the payload passed to HookLevelUp actions.
type LevelUp struct {
	EntityID int64
	NewLevel int32
}

// Service manages the progression component and its hooks.
type Service struct {
	store component.Store[Progression]
	curve Curve
	hooks *hooks.Hooks
}

// NewService builds a progression service. hooks may be nil.
func NewService(store component.Store[Progression], curve Curve, h *hooks.Hooks) *Service {
	return &Service{store: store, curve: curve, hooks: h}
}

// Get returns an entity's progression, defaulting to level 1 / 0 xp.
func (s *Service) Get(ctx context.Context, entityID int64) (Progression, error) {
	p, ok, err := s.store.Get(ctx, entityID)
	if err != nil {
		return Progression{}, err
	}
	if !ok {
		return Progression{Level: 1}, nil
	}
	return p, nil
}

// GrantXP applies xp (after the HookXP filter), persists the result, and fires
// HookLevelUp once per level gained. Returns the new progression and levels
// gained.
func (s *Service) GrantXP(ctx context.Context, entityID int64, amount int64) (Progression, int32, error) {
	if s.hooks != nil {
		amount = hooks.ApplyFilters(s.hooks, ctx, HookXP, amount)
	}
	cur, err := s.Get(ctx, entityID)
	if err != nil {
		return Progression{}, 0, err
	}
	level, xp, gained := Apply(s.curve, cur.Level, cur.XP, amount)
	next := Progression{Level: level, XP: xp}
	if err := s.store.Set(ctx, entityID, next); err != nil {
		return Progression{}, 0, err
	}
	if s.hooks != nil {
		for i := int32(0); i < gained; i++ {
			_ = hooks.DoAction(s.hooks, ctx, HookLevelUp, LevelUp{EntityID: entityID, NewLevel: cur.Level + i + 1})
		}
	}
	return next, gained, nil
}
