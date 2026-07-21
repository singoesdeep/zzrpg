// Package relation is the gamekit entity-graph toolkit: typed directed edges
// between entities that the single entity.OwnerID cannot express. A squad
// contains units, a city contains buildings, a party has members, a character
// equips items — all are (from, type, to) edges, queryable in both directions.
// It stays genre-neutral: the framework defines no edge types, games do.
package relation

import "context"

// Edge is a typed directed link from one entity to another.
type Edge struct {
	From int64  `json:"from"`
	Type string `json:"type"`
	To   int64  `json:"to"`
}

// Repo persists edges. Implementations: an in-memory Repo for tests/DB-less use
// and a Postgres Repo over the sdk store seam.
type Repo interface {
	// Link records an edge (idempotent — linking twice is one edge).
	Link(ctx context.Context, from int64, edgeType string, to int64) error
	// Unlink removes an edge (a no-op when absent).
	Unlink(ctx context.Context, from int64, edgeType string, to int64) error
	// To lists the targets of edges of a type leaving an entity (city → its
	// buildings).
	To(ctx context.Context, from int64, edgeType string) ([]int64, error)
	// From lists the sources of edges of a type arriving at an entity (a
	// building → the cities that contain it).
	From(ctx context.Context, to int64, edgeType string) ([]int64, error)
	// Exists reports whether a specific edge is present.
	Exists(ctx context.Context, from int64, edgeType string, to int64) (bool, error)
}
