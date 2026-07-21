// Package world ties entities and their components together and answers
// component-indexed queries — "every entity that has components X and Y" — the
// ECS query a System iterates on each tick.
package world

import (
	"context"
	"fmt"

	"github.com/singoesdeep/zzrpg/gamekit/component"
	"github.com/singoesdeep/zzrpg/gamekit/entity"
)

// World holds the entity repo and the registered component indexes.
type World struct {
	Entities entity.Repo
	indexes  map[string]component.ComponentIndex
}

// New builds a world over an entity repo. Register each component's store.
func New(entities entity.Repo) *World {
	return &World{Entities: entities, indexes: map[string]component.ComponentIndex{}}
}

// Register adds a component index so it participates in queries. A component
// store (Store[T]) is a ComponentIndex, so pass it directly.
func (w *World) Register(idx component.ComponentIndex) {
	w.indexes[idx.Name()] = idx
}

// Query returns the ids of entities that have ALL the named components,
// intersecting their indexes. An unknown component name is an error.
func (w *World) Query(ctx context.Context, names ...string) ([]int64, error) {
	if len(names) == 0 {
		return nil, nil
	}
	base, ok := w.indexes[names[0]]
	if !ok {
		return nil, fmt.Errorf("world: unknown component %q", names[0])
	}
	ids, err := base.EntityIDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, name := range names[1:] {
		idx, ok := w.indexes[name]
		if !ok {
			return nil, fmt.Errorf("world: unknown component %q", name)
		}
		kept := ids[:0]
		for _, id := range ids {
			has, err := idx.Has(ctx, id)
			if err != nil {
				return nil, err
			}
			if has {
				kept = append(kept, id)
			}
		}
		ids = kept
	}
	return ids, nil
}

// Pair couples an entity id with its component value.
type Pair[T any] struct {
	EntityID int64
	Value    T
}

// With returns every entity that has the given component, paired with its value
// — the typed way for a System to iterate a component. It reads through the
// store, so it reflects the current persisted state.
func With[T any](ctx context.Context, s component.Store[T]) ([]Pair[T], error) {
	ids, err := s.EntityIDs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Pair[T], 0, len(ids))
	for _, id := range ids {
		v, ok, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, Pair[T]{EntityID: id, Value: v})
		}
	}
	return out, nil
}
