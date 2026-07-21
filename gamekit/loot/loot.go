// Package loot is gamekit's weighted-drop toolkit: the probability/RNG
// mechanism for rolling a named table into a list of (item id, quantity)
// drops. It owns none of the content or persistence — a table's drop rules are
// resolved by a game-supplied EntriesFor func (a DB lookup, a JSON content
// pack, a hardcoded map, or a fallback chain of these), exactly as the idle
// toolkit takes its inputs as funcs rather than concrete stores. "Gold" is just
// an ItemID string to the framework; a game decides what its item ids mean.
package loot

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/singoesdeep/zzrpg/sdk/engine/hooks"
)

// RateScale is the denominator Entry.Rate is measured against — Rate=1000
// means a 10% drop chance.
const RateScale = 10000

// Entry is one weighted drop rule: a quantity in [Min,Max] drops with
// probability Rate/RateScale.
type Entry struct {
	ItemID string
	Rate   int32
	Min    int32
	Max    int32
}

// Drop is one item id + quantity a roll produced.
type Drop struct {
	ItemID   string
	Quantity int32
}

// HookRoll is a Filter over a completed roll before it is returned — the seam
// a plugin uses for luck bonuses, event multipliers, or bonus/blocked items.
const HookRoll = "loot.roll"

// Roll is the HookRoll payload.
type Roll struct {
	TableID string
	Items   []Drop
}

// EntriesFor resolves a table id to its drop rules. The framework never
// persists tables itself — a game's own lookup does.
type EntriesFor func(ctx context.Context, tableID string) ([]Entry, error)

// Roller rolls loot tables against a game-supplied EntriesFor. Concurrency-safe
// (RollLoot may be called from many goroutines at once, e.g. many concurrent
// kills).
type Roller struct {
	entriesFor EntriesFor
	hooks      *hooks.Hooks

	randMu sync.Mutex
	rand   *rand.Rand
}

// Option configures a Roller at construction.
type Option func(*Roller)

// WithSeed makes rolls deterministic from the given seed instead of the
// default time-based seed — for reproducible tests and replayable worlds.
func WithSeed(seed int64) Option { return func(r *Roller) { r.rand = rand.New(rand.NewSource(seed)) } }

// WithRand injects a fully custom generator. It is still accessed under the
// Roller's mutex, so the supplied *rand.Rand need not be concurrency-safe.
func WithRand(gen *rand.Rand) Option { return func(r *Roller) { r.rand = gen } }

// WithHooks enables the HookRoll filter so plugins can adjust rolled drops.
func WithHooks(h *hooks.Hooks) Option { return func(r *Roller) { r.hooks = h } }

// NewRoller builds a Roller. By default the RNG is seeded from the wall clock;
// pass WithSeed/WithRand to inject a deterministic generator.
func NewRoller(entriesFor EntriesFor, opts ...Option) *Roller {
	r := &Roller{entriesFor: entriesFor, rand: rand.New(rand.NewSource(time.Now().UnixNano()))}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Roll resolves tableID's entries and rolls each independently, then threads
// the result through HookRoll.
func (r *Roller) Roll(ctx context.Context, tableID string) ([]Drop, error) {
	entries, err := r.entriesFor(ctx, tableID)
	if err != nil {
		return nil, err
	}
	var drops []Drop
	for _, e := range entries {
		if d, ok := r.rollEntry(e); ok {
			drops = append(drops, d)
		}
	}
	return hooks.ApplyFilters(r.hooks, ctx, HookRoll, Roll{TableID: tableID, Items: drops}).Items, nil
}

// rollEntry decides one drop rule: drops with probability Rate/RateScale, in a
// quantity uniformly drawn from [Min,Max].
func (r *Roller) rollEntry(e Entry) (Drop, bool) {
	if r.roll31n(RateScale) >= e.Rate {
		return Drop{}, false
	}
	qty := e.Min
	if e.Max > e.Min {
		qty = e.Min + r.roll31n(e.Max-e.Min+1)
	}
	return Drop{ItemID: e.ItemID, Quantity: qty}, true
}

// roll31n serializes access to the non-thread-safe *rand.Rand.
func (r *Roller) roll31n(n int32) int32 {
	r.randMu.Lock()
	defer r.randMu.Unlock()
	return r.rand.Int31n(n)
}
