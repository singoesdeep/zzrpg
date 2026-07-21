package loot

import (
	"context"
	"math/rand"

	"github.com/singoesdeep/zzrpg/backend/content"
	glootlib "github.com/singoesdeep/zzrpg/gamekit/loot"
	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

// fallbackTables is the embedded loot pack used when the DB has no row for a
// requested table (e.g. the training dummy's drops). Loaded once at startup.
var fallbackTables = content.MustLoadLootTables()

type LootService interface {
	CreateLootTable(ctx context.Context, lt *LootTable) error
	RollLoot(ctx context.Context, tableID string) ([]DroppedItem, error)
	ListLootTables(ctx context.Context, limit, offset int) ([]LootTable, error)
}

// lootService is this RPG's loot contract (LootTable/DroppedItem, DB-or-fallback
// resolution, admin CRUD). The actual roll mechanism — weighted probability,
// RNG concurrency-safety — is gamekit/loot's Roller; this type is the adapter
// between the two, translating types and preserving this package's existing
// external contract (including its own HookRoll/LootRoll, for backward
// compatibility with callers already wired to it).
type lootService struct {
	repo       LootRepository
	roller     *glootlib.Roller
	hooks      *hooks.Hooks
	rollerOpts []glootlib.Option
}

// Option configures a lootService at construction.
type Option func(*lootService)

// WithSeed makes loot rolls deterministic from the given seed instead of the
// default time-based seed. Useful for reproducible tests and replayable/sharded
// worlds where the same inputs must yield the same drops.
func WithSeed(seed int64) Option {
	return func(s *lootService) { s.rollerOpts = append(s.rollerOpts, glootlib.WithSeed(seed)) }
}

// WithRand injects a fully custom generator. It is still accessed under the
// service's mutex, so the supplied *rand.Rand need not be concurrency-safe.
func WithRand(r *rand.Rand) Option {
	return func(s *lootService) { s.rollerOpts = append(s.rollerOpts, glootlib.WithRand(r)) }
}

// WithHooks enables the loot.roll filter so plugins can adjust rolled drops.
func WithHooks(h *hooks.Hooks) Option {
	return func(s *lootService) { s.hooks = h }
}

// NewLootService builds a loot service. By default the RNG is seeded from the
// wall clock; pass WithSeed/WithRand to inject a deterministic generator.
func NewLootService(repo LootRepository, opts ...Option) LootService {
	s := &lootService{repo: repo}
	for _, opt := range opts {
		opt(s)
	}
	s.roller = glootlib.NewRoller(s.entriesFor, s.rollerOpts...)
	return s
}

func (s *lootService) CreateLootTable(ctx context.Context, lt *LootTable) error {
	return s.repo.CreateLootTable(ctx, lt)
}

func (s *lootService) ListLootTables(ctx context.Context, limit, offset int) ([]LootTable, error) {
	return s.repo.ListLootTables(ctx, limit, offset)
}

func (s *lootService) RollLoot(ctx context.Context, tableID string) ([]DroppedItem, error) {
	drops, err := s.roller.Roll(ctx, tableID)
	if err != nil {
		return nil, err
	}
	out := make([]DroppedItem, len(drops))
	for i, d := range drops {
		out[i] = DroppedItem{ItemDefinitionID: d.ItemID, Quantity: d.Quantity}
	}

	// This package's own HookRoll, kept for backward compatibility with
	// existing callers/tests wired to it directly (a second, independent filter
	// pass from gamekit/loot.Roller's own HookRoll, which nothing here uses).
	return hooks.ApplyFilters(s.hooks, ctx, HookRoll, LootRoll{TableID: tableID, Items: out}).Items, nil
}

// entriesFor is gamekit/loot.EntriesFor: the DB table if present, otherwise an
// embedded fallback table if one is defined for the ID.
func (s *lootService) entriesFor(ctx context.Context, tableID string) ([]glootlib.Entry, error) {
	lt, err := s.repo.GetLootTable(ctx, tableID)
	if err == nil {
		return toEntries(lt.Entries), nil
	}
	ct, ok := fallbackTables[tableID]
	if !ok {
		return nil, err
	}
	entries := make([]glootlib.Entry, len(ct.Entries))
	for i, e := range ct.Entries {
		entries[i] = glootlib.Entry{ItemID: e.ItemDefinitionID, Rate: e.Rate, Min: e.MinQuantity, Max: e.MaxQuantity}
	}
	return entries, nil
}

func toEntries(es []LootEntry) []glootlib.Entry {
	out := make([]glootlib.Entry, len(es))
	for i, e := range es {
		out[i] = glootlib.Entry{ItemID: e.ItemDefinitionID, Rate: e.Rate, Min: e.MinQuantity, Max: e.MaxQuantity}
	}
	return out
}
