// Package entity is the gamekit entity foundation: a minimal identity to which
// optional components (stats, inventory, resources, …) are attached. A
// character, a city, a mob, and an RTS unit are all just entities of different
// Kind — the framework imposes no game concepts on them.
package entity

import (
	"context"
	"time"
)

// Entity is a bare identity row; all game data lives in components keyed by ID.
type Entity struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`     // template name: "character", "city", "goblin", …
	OwnerID   int64     `json:"owner_id"` // owning account, or 0 for a world-owned entity
	CreatedAt time.Time `json:"created_at"`
}

// Repo persists entities. Implementations: an in-memory Repo for tests/DB-less
// use and a Postgres Repo over the sdk store seam.
type Repo interface {
	Create(ctx context.Context, kind string, ownerID int64) (Entity, error)
	Get(ctx context.Context, id int64) (Entity, error)
	ListByOwner(ctx context.Context, ownerID int64) ([]Entity, error)
	ListByKind(ctx context.Context, kind string) ([]Entity, error)
	Delete(ctx context.Context, id int64) error
}

// ErrNotFound is returned when an entity id does not exist.
type ErrNotFound struct{ ID int64 }

func (e ErrNotFound) Error() string { return "entity: not found" }
