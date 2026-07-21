// Package component is the gamekit component layer: typed data attached to an
// entity, backed by its own store. Components are optional and composable — an
// entity has exactly the components it was given. Games attach built-in
// components (stats, inventory, …) or define their own.
//
// A component's persistence is a Store[T]. gamekit provides an in-memory store
// (tests/DB-less) and a generic JSONB-backed store; games may also write a
// bespoke relational Store[T] for a component with its own columns.
package component

import "context"

// ComponentIndex is the non-generic view of a component store used by the world
// for entity queries: which entities have this component.
type ComponentIndex interface {
	// Name is the component's unique name, e.g. "stats", "inventory", "resources".
	Name() string
	// EntityIDs returns every entity id that has this component (sorted).
	EntityIDs(ctx context.Context) ([]int64, error)
	// Has reports whether the entity has this component.
	Has(ctx context.Context, entityID int64) (bool, error)
}

// Store is the typed persistence for one component kind, keyed by entity id. It
// is also a ComponentIndex so it can be registered with the world for queries.
type Store[T any] interface {
	ComponentIndex
	// Get returns the entity's component; ok is false when absent.
	Get(ctx context.Context, entityID int64) (value T, ok bool, err error)
	// Set stores (or replaces) the entity's component.
	Set(ctx context.Context, entityID int64, v T) error
	// Delete removes the entity's component.
	Delete(ctx context.Context, entityID int64) error
}
