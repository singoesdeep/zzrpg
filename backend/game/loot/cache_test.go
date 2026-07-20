package loot

import (
	"context"
	"testing"
	"time"
)

// fakeCache is an in-memory Cache for testing the decorator without Redis.
type fakeCache struct {
	data map[string][]byte
}

func newFakeCache() *fakeCache { return &fakeCache{data: make(map[string][]byte)} }

func (f *fakeCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	v, ok := f.data[key]
	return v, ok, nil
}
func (f *fakeCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	f.data[key] = value
	return nil
}
func (f *fakeCache) Delete(_ context.Context, key string) error {
	delete(f.data, key)
	return nil
}

func (f *fakeCache) Ping(_ context.Context) error { return nil }

// countingRepo wraps a LootRepository and counts GetLootTable calls so tests can
// assert whether a read was served from cache or hit the source.
type countingRepo struct {
	inner    LootRepository
	getCalls int
}

func (c *countingRepo) GetLootTable(ctx context.Context, id string) (*LootTable, error) {
	c.getCalls++
	return c.inner.GetLootTable(ctx, id)
}
func (c *countingRepo) CreateLootTable(ctx context.Context, lt *LootTable) error {
	return c.inner.CreateLootTable(ctx, lt)
}
func (c *countingRepo) ListLootTables(ctx context.Context, limit, offset int) ([]LootTable, error) {
	return c.inner.ListLootTables(ctx, limit, offset)
}

func TestCachedRepositoryReadThroughAndInvalidation(t *testing.T) {
	ctx := context.Background()
	inner := newMockLootRepository()
	_ = inner.CreateLootTable(ctx, &LootTable{ID: "goblin_drops", Description: "v1"})

	counting := &countingRepo{inner: inner}
	repo := NewCachedRepository(counting, newFakeCache(), time.Minute)

	// First read: cache miss -> hits source and caches the result.
	lt, err := repo.GetLootTable(ctx, "goblin_drops")
	if err != nil || lt.Description != "v1" {
		t.Fatalf("first read failed: lt=%+v err=%v", lt, err)
	}
	if counting.getCalls != 1 {
		t.Fatalf("expected 1 source read, got %d", counting.getCalls)
	}

	// Second read: served from cache, source not touched.
	if _, err := repo.GetLootTable(ctx, "goblin_drops"); err != nil {
		t.Fatalf("second read failed: %v", err)
	}
	if counting.getCalls != 1 {
		t.Fatalf("expected read served from cache (still 1 source read), got %d", counting.getCalls)
	}

	// Writing a new version invalidates the cache entry.
	if err := repo.CreateLootTable(ctx, &LootTable{ID: "goblin_drops", Description: "v2"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	lt, err = repo.GetLootTable(ctx, "goblin_drops")
	if err != nil {
		t.Fatalf("post-invalidation read failed: %v", err)
	}
	if counting.getCalls != 2 {
		t.Fatalf("expected source re-read after invalidation, got %d source reads", counting.getCalls)
	}
	if lt.Description != "v2" {
		t.Fatalf("expected fresh v2 after invalidation, got %q", lt.Description)
	}
}

// TestCachedRepositoryDoesNotCacheMisses ensures not-found results are not
// cached, so the service's dummy-drops fallback keeps working on every call.
func TestCachedRepositoryDoesNotCacheMisses(t *testing.T) {
	ctx := context.Background()
	counting := &countingRepo{inner: newMockLootRepository()}
	repo := NewCachedRepository(counting, newFakeCache(), time.Minute)

	for i := 0; i < 3; i++ {
		if _, err := repo.GetLootTable(ctx, "does_not_exist"); err == nil {
			t.Fatal("expected error for missing table")
		}
	}
	if counting.getCalls != 3 {
		t.Fatalf("expected every miss to reach the source (3), got %d", counting.getCalls)
	}
}
