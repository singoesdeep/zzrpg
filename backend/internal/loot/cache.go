package loot

import (
	"context"
	"encoding/json"
	"time"

	"github.com/singoesdeep/zzrpg/backend/pkg/cache"
)

// cachedRepository is a read-through cache decorator over a LootRepository.
// Loot tables are static configuration read on every mob/dummy kill, so caching
// GetLootTable removes a per-kill database round-trip. Writes invalidate the key.
type cachedRepository struct {
	inner LootRepository
	cache cache.Cache
	ttl   time.Duration
}

// NewCachedRepository wraps inner so GetLootTable results are cached in c for ttl.
// Pass cache.Noop{} to disable caching (behaves exactly like inner).
func NewCachedRepository(inner LootRepository, c cache.Cache, ttl time.Duration) LootRepository {
	return &cachedRepository{inner: inner, cache: c, ttl: ttl}
}

func lootTableKey(id string) string { return "loot:table:" + id }

func (r *cachedRepository) GetLootTable(ctx context.Context, id string) (*LootTable, error) {
	key := lootTableKey(id)
	if b, ok, _ := r.cache.Get(ctx, key); ok {
		var lt LootTable
		if err := json.Unmarshal(b, &lt); err == nil {
			return &lt, nil
		}
		// Corrupt entry: ignore and reload from the source of truth.
	}

	lt, err := r.inner.GetLootTable(ctx, id)
	if err != nil {
		// Do not cache misses/errors (keeps the dummy-drops fallback path working).
		return nil, err
	}
	if b, err := json.Marshal(lt); err == nil {
		_ = r.cache.Set(ctx, key, b, r.ttl)
	}
	return lt, nil
}

func (r *cachedRepository) CreateLootTable(ctx context.Context, lt *LootTable) error {
	if err := r.inner.CreateLootTable(ctx, lt); err != nil {
		return err
	}
	// Invalidate so a subsequent read reflects the new definition immediately.
	_ = r.cache.Delete(ctx, lootTableKey(lt.ID))
	return nil
}

// ListLootTables is not cached (admin/rare read); passes through to the source.
func (r *cachedRepository) ListLootTables(ctx context.Context, limit, offset int) ([]LootTable, error) {
	return r.inner.ListLootTables(ctx, limit, offset)
}
