// Package stats is the gamekit generic attribute engine: a "stats" component of
// arbitrary named attributes (STR, HP, population, armor, production_rate, …)
// attached to any entity, with derived stats computed by a pluggable resolver.
// It imposes no particular stat names, so it serves an RPG character, a city's
// development, or an RTS unit equally.
package stats

import (
	"context"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

// HookDerive is a Filter over the derived-stat map, letting plugins inject
// auras, buffs, or global modifiers before it is stored/used.
const HookDerive = "stats.derive"

// Stats is the component stored per entity: the authored Base attributes and the
// last computed Derived attributes.
type Stats struct {
	Base    map[string]float64 `json:"base"`
	Derived map[string]float64 `json:"derived"`
}

// StatResolver computes derived stats from base stats for an entity kind. The
// formula resolver here is the default; a game may plug in another (e.g. the
// Rust zzstat resolver) by implementing this interface.
type StatResolver interface {
	Derive(ctx context.Context, kind string, base map[string]float64) (map[string]float64, error)
}

// EntityKinder resolves an entity id to its kind, so derivation can be
// kind-specific. Satisfied by the entity repo.
type EntityKinder interface {
	Kind(ctx context.Context, entityID int64) (string, error)
}

// Service manages the stats component: authoring base stats, recomputing derived
// stats through the resolver + HookDerive filter, and reading them.
type Service struct {
	store    component.Store[Stats]
	resolver StatResolver
	kinder   EntityKinder
	hooks    *hooks.Hooks
}

// NewService builds a stats service. hooks may be nil (no filters applied).
func NewService(store component.Store[Stats], resolver StatResolver, kinder EntityKinder, h *hooks.Hooks) *Service {
	return &Service{store: store, resolver: resolver, kinder: kinder, hooks: h}
}

// Get returns an entity's stats (ok=false when it has no stats component).
func (s *Service) Get(ctx context.Context, entityID int64) (Stats, bool, error) {
	return s.store.Get(ctx, entityID)
}

// SetBase authors an entity's base stats and recomputes its derived stats.
func (s *Service) SetBase(ctx context.Context, entityID int64, base map[string]float64) (Stats, error) {
	kind := ""
	if s.kinder != nil {
		k, err := s.kinder.Kind(ctx, entityID)
		if err != nil {
			return Stats{}, err
		}
		kind = k
	}
	derived, err := s.resolver.Derive(ctx, kind, base)
	if err != nil {
		return Stats{}, err
	}
	if s.hooks != nil {
		derived = hooks.ApplyFilters(s.hooks, ctx, HookDerive, derived)
	}
	st := Stats{Base: base, Derived: derived}
	if err := s.store.Set(ctx, entityID, st); err != nil {
		return Stats{}, err
	}
	return st, nil
}

// Recompute re-derives an entity's stats from its stored base (e.g. after a
// buff-affecting change), applying the HookDerive filter again.
func (s *Service) Recompute(ctx context.Context, entityID int64) (Stats, error) {
	cur, ok, err := s.store.Get(ctx, entityID)
	if err != nil || !ok {
		return Stats{}, err
	}
	return s.SetBase(ctx, entityID, cur.Base)
}
