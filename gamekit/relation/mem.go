package relation

import (
	"context"
	"sync"
)

// memRepo is a concurrency-safe in-memory Repo for tests and DB-less/local use.
type memRepo struct {
	mu    sync.Mutex
	edges map[Edge]struct{}
}

// NewMemRepo returns an in-memory relation Repo.
func NewMemRepo() Repo { return &memRepo{edges: map[Edge]struct{}{}} }

func (r *memRepo) Link(_ context.Context, from int64, t string, to int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.edges[Edge{From: from, Type: t, To: to}] = struct{}{}
	return nil
}

func (r *memRepo) Unlink(_ context.Context, from int64, t string, to int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.edges, Edge{From: from, Type: t, To: to})
	return nil
}

func (r *memRepo) To(_ context.Context, from int64, t string) ([]int64, error) {
	return r.collect(func(e Edge) (int64, bool) { return e.To, e.From == from && e.Type == t }), nil
}

func (r *memRepo) From(_ context.Context, to int64, t string) ([]int64, error) {
	return r.collect(func(e Edge) (int64, bool) { return e.From, e.To == to && e.Type == t }), nil
}

func (r *memRepo) Exists(_ context.Context, from int64, t string, to int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.edges[Edge{From: from, Type: t, To: to}]
	return ok, nil
}

func (r *memRepo) collect(pick func(Edge) (int64, bool)) []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []int64
	for e := range r.edges {
		if id, ok := pick(e); ok {
			out = append(out, id)
		}
	}
	sortIDs(out)
	return out
}

func sortIDs(ids []int64) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j-1] > ids[j]; j-- {
			ids[j-1], ids[j] = ids[j], ids[j-1]
		}
	}
}
