// Package hooks provides synchronous, ordered extension points — the in-line
// counterpart to the async event bus. Where the bus lets plugins REACT to events
// after the fact, hooks let them PARTICIPATE in a flow while it runs:
//
//   - Filters transform a value as it threads through a named chain, so a plugin
//     can adjust computed damage, rewrite a loot roll, tweak a price, etc. — the
//     "apply_filters" of a WordPress-style extension model.
//   - Actions run ordered side effects at a named point and may abort the chain
//     by returning an error (a gate/veto) — the "do_action".
//
// Hooks are registered by name (so a plugin extends "combat.damage" without
// importing the producer), run in ascending priority (ties broken by registration
// order), are panic-isolated (a misbehaving hook can't crash the request), and
// type-checked at the call boundary via generics. All entry points are nil-safe,
// so a flow can call them unconditionally whether or not hooks are wired.
package hooks

import (
	"context"
	"log/slog"
	"sort"
	"sync"
)

type entry struct {
	priority int
	seq      int // registration order; stable tie-break within a priority
	fn       any // func(context.Context, T) T (filter) | func(context.Context, T) error (action)
}

// Hooks is the registry of named filter and action chains.
type Hooks struct {
	log     *slog.Logger
	mu      sync.RWMutex
	filters map[string][]entry
	actions map[string][]entry
	seq     int
}

// New returns an empty registry. If log is nil, slog.Default() is used.
func New(log *slog.Logger) *Hooks {
	if log == nil {
		log = slog.Default()
	}
	return &Hooks{
		log:     log,
		filters: make(map[string][]entry),
		actions: make(map[string][]entry),
	}
}

func (h *Hooks) register(m map[string][]entry, name string, priority int, fn any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seq++
	lst := append(m[name], entry{priority: priority, seq: h.seq, fn: fn})
	sort.SliceStable(lst, func(i, j int) bool {
		if lst[i].priority != lst[j].priority {
			return lst[i].priority < lst[j].priority
		}
		return lst[i].seq < lst[j].seq
	})
	m[name] = lst
}

func (h *Hooks) snapshot(m map[string][]entry, name string) []entry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	src := m[name]
	if len(src) == 0 {
		return nil
	}
	out := make([]entry, len(src))
	copy(out, src)
	return out
}

// AddFilter registers fn as a filter for the named chain. Filters run in ascending
// priority (a common default is 10); the value threads through them.
func AddFilter[T any](h *Hooks, name string, priority int, fn func(context.Context, T) T) {
	if h == nil || fn == nil {
		return
	}
	h.register(h.filters, name, priority, fn)
}

// ApplyFilters threads value through every filter registered for name and returns
// the final value. A filter of the wrong type for this call is skipped (logged);
// a panicking filter is recovered and the value passes through it unchanged.
func ApplyFilters[T any](h *Hooks, ctx context.Context, name string, value T) T {
	if h == nil {
		return value
	}
	for _, e := range h.snapshot(h.filters, name) {
		fn, ok := e.fn.(func(context.Context, T) T)
		if !ok {
			h.log.Error("hooks: filter type mismatch, skipping", "hook", name)
			continue
		}
		value = applyOne(h, ctx, name, fn, value)
	}
	return value
}

// applyOne runs one filter with panic isolation; on panic the value is unchanged.
func applyOne[T any](h *Hooks, ctx context.Context, name string, fn func(context.Context, T) T, value T) (out T) {
	out = value
	defer func() {
		if r := recover(); r != nil {
			h.log.Error("hooks: filter panicked", "hook", name, "panic", r)
			out = value
		}
	}()
	return fn(ctx, value)
}

// AddAction registers fn as an action for the named chain. Actions run in
// ascending priority; the first non-nil error aborts the remaining actions.
func AddAction[T any](h *Hooks, name string, priority int, fn func(context.Context, T) error) {
	if h == nil || fn == nil {
		return
	}
	h.register(h.actions, name, priority, fn)
}

// DoAction runs every action registered for name in order, passing value. It
// returns the first error an action reports (aborting the rest) — the basis of a
// gate/veto — or nil. A panicking action is recovered and logged, and does not
// abort the chain.
func DoAction[T any](h *Hooks, ctx context.Context, name string, value T) error {
	if h == nil {
		return nil
	}
	for _, e := range h.snapshot(h.actions, name) {
		fn, ok := e.fn.(func(context.Context, T) error)
		if !ok {
			h.log.Error("hooks: action type mismatch, skipping", "hook", name)
			continue
		}
		if err := doOne(h, ctx, name, fn, value); err != nil {
			return err
		}
	}
	return nil
}

// doOne runs one action with panic isolation; a panic is logged and treated as a
// non-aborting no-op.
func doOne[T any](h *Hooks, ctx context.Context, name string, fn func(context.Context, T) error, value T) (err error) {
	defer func() {
		if r := recover(); r != nil {
			h.log.Error("hooks: action panicked", "hook", name, "panic", r)
			err = nil
		}
	}()
	return fn(ctx, value)
}
