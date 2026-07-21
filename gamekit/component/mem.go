package component

import (
	"context"
	"sort"
	"sync"
)

// memStore is a concurrency-safe in-memory Store[T] for tests and DB-less use.
type memStore[T any] struct {
	name string
	mu   sync.Mutex
	m    map[int64]T
}

// NewMemStore returns an in-memory component store under the given name.
func NewMemStore[T any](name string) Store[T] {
	return &memStore[T]{name: name, m: make(map[int64]T)}
}

func (s *memStore[T]) Name() string { return s.name }

func (s *memStore[T]) Get(_ context.Context, entityID int64) (T, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[entityID]
	return v, ok, nil
}

func (s *memStore[T]) Set(_ context.Context, entityID int64, v T) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[entityID] = v
	return nil
}

func (s *memStore[T]) Delete(_ context.Context, entityID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, entityID)
	return nil
}

func (s *memStore[T]) Has(_ context.Context, entityID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.m[entityID]
	return ok, nil
}

func (s *memStore[T]) EntityIDs(_ context.Context) ([]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]int64, 0, len(s.m))
	for id := range s.m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}
