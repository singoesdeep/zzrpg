package entity

import (
	"context"
	"sync"
	"time"
)

// memRepo is a concurrency-safe in-memory Repo for tests and DB-less/local use.
type memRepo struct {
	mu     sync.Mutex
	nextID int64
	byID   map[int64]Entity
}

// NewMemRepo returns an in-memory entity Repo.
func NewMemRepo() Repo {
	return &memRepo{nextID: 1, byID: make(map[int64]Entity)}
}

func (r *memRepo) Create(_ context.Context, kind string, ownerID int64) (Entity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := Entity{ID: r.nextID, Kind: kind, OwnerID: ownerID, CreatedAt: time.Now()}
	r.byID[e.ID] = e
	r.nextID++
	return e, nil
}

func (r *memRepo) Get(_ context.Context, id int64) (Entity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.byID[id]
	if !ok {
		return Entity{}, ErrNotFound{ID: id}
	}
	return e, nil
}

func (r *memRepo) ListByOwner(_ context.Context, ownerID int64) ([]Entity, error) {
	return r.filter(func(e Entity) bool { return e.OwnerID == ownerID }), nil
}

func (r *memRepo) ListByKind(_ context.Context, kind string) ([]Entity, error) {
	return r.filter(func(e Entity) bool { return e.Kind == kind }), nil
}

func (r *memRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, id)
	return nil
}

func (r *memRepo) filter(keep func(Entity) bool) []Entity {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Entity
	for _, e := range r.byID {
		if keep(e) {
			out = append(out, e)
		}
	}
	sortByID(out)
	return out
}

func sortByID(es []Entity) {
	for i := 1; i < len(es); i++ {
		for j := i; j > 0 && es[j-1].ID > es[j].ID; j-- {
			es[j-1], es[j] = es[j], es[j-1]
		}
	}
}
