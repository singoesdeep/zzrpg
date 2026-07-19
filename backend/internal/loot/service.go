package loot

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/singoesdeep/zzrpg/backend/content"
)

// fallbackTables is the embedded loot pack used when the DB has no row for a
// requested table (e.g. the training dummy's drops). Loaded once at startup.
var fallbackTables = content.MustLoadLootTables()

type LootService interface {
	CreateLootTable(ctx context.Context, lt *LootTable) error
	RollLoot(ctx context.Context, tableID string) ([]DroppedItem, error)
	ListLootTables(ctx context.Context, limit, offset int) ([]LootTable, error)
}

type lootService struct {
	repo LootRepository
	// rand is a *math/rand.Rand, which is NOT safe for concurrent use. RollLoot
	// is called concurrently (one goroutine per combat/offline roll), so every
	// access is serialized by randMu.
	randMu sync.Mutex
	rand   *rand.Rand
}

// Option configures a lootService at construction.
type Option func(*lootService)

// WithSeed makes loot rolls deterministic from the given seed instead of the
// default time-based seed. Useful for reproducible tests and replayable/sharded
// worlds where the same inputs must yield the same drops.
func WithSeed(seed int64) Option {
	return func(s *lootService) { s.rand = rand.New(rand.NewSource(seed)) }
}

// WithRand injects a fully custom generator. It is still accessed under the
// service's mutex, so the supplied *rand.Rand need not be concurrency-safe.
func WithRand(r *rand.Rand) Option {
	return func(s *lootService) { s.rand = r }
}

// NewLootService builds a loot service. By default the RNG is seeded from the
// wall clock; pass WithSeed/WithRand to inject a deterministic generator.
func NewLootService(repo LootRepository, opts ...Option) LootService {
	s := &lootService{
		repo: repo,
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *lootService) CreateLootTable(ctx context.Context, lt *LootTable) error {
	return s.repo.CreateLootTable(ctx, lt)
}

func (s *lootService) ListLootTables(ctx context.Context, limit, offset int) ([]LootTable, error) {
	return s.repo.ListLootTables(ctx, limit, offset)
}

func (s *lootService) RollLoot(ctx context.Context, tableID string) ([]DroppedItem, error) {
	lt, err := s.repo.GetLootTable(ctx, tableID)
	if err != nil {
		// Fall back to an embedded content table if one is defined for this ID
		// (e.g. the training dummy's drops when the DB has no row yet); other
		// missing tables still surface the repo error.
		ct, ok := fallbackTables[tableID]
		if !ok {
			return nil, err
		}
		var drops []DroppedItem
		for _, e := range ct.Entries {
			if d, ok := s.rollEntry(e.ItemDefinitionID, e.Rate, e.MinQuantity, e.MaxQuantity); ok {
				drops = append(drops, d)
			}
		}
		return drops, nil
	}

	var drops []DroppedItem
	for _, e := range lt.Entries {
		if d, ok := s.rollEntry(e.ItemDefinitionID, e.Rate, e.MinQuantity, e.MaxQuantity); ok {
			drops = append(drops, d)
		}
	}

	return drops, nil
}

// rollEntry decides one drop rule: it drops with probability rate/10000, in a
// quantity uniformly drawn from [minQty, maxQty]. Shared by the DB and fallback
// paths so both roll identically.
func (s *lootService) rollEntry(itemID string, rate, minQty, maxQty int32) (DroppedItem, bool) {
	if s.roll31n(10000) >= rate {
		return DroppedItem{}, false
	}
	qty := minQty
	if maxQty > minQty {
		qty = minQty + s.roll31n(maxQty-minQty+1)
	}
	return DroppedItem{ItemDefinitionID: itemID, Quantity: qty}, true
}

// roll31n serializes access to the non-thread-safe *rand.Rand.
func (s *lootService) roll31n(n int32) int32 {
	s.randMu.Lock()
	defer s.randMu.Unlock()
	return s.rand.Int31n(n)
}
