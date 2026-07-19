package events

import (
	"context"
	"sync"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
)

type EventType string

const (
	EventItemEquipped   EventType = "item_equipped"
	EventItemUnequipped EventType = "item_unequipped"
)

type Event struct {
	Type    EventType
	Payload any
}

type Handler func(ctx context.Context, event Event)

// legacyEvent adapts the (type, payload) pair used by this facade to the
// engine/bus.Event interface (Name() string), so that events published through
// the historical events.Bus API flow across the same engine bus the kernel owns.
type legacyEvent struct {
	t       EventType
	payload any
}

func (e legacyEvent) Name() string { return string(e.t) }

// Bus is a thin, behavior-compatible facade over an engine/bus.EventBus.
//
// Its public surface (Global/Subscribe/Publish + Event/Handler/EventType) is
// unchanged from the original in-package implementation, so existing callers
// and tests need no edits. All dispatch semantics — one detached goroutine per
// handler (context.WithoutCancel), panic isolation, no-op on zero subscribers —
// are provided by the underlying engine bus, which preserves them exactly.
type Bus struct {
	mu      sync.RWMutex
	backend bus.EventBus
}

func newBus() *Bus {
	return &Bus{backend: bus.NewInProc(nil)}
}

var globalBus = newBus()

func Global() *Bus {
	return globalBus
}

// SetBackend lets the engine kernel share its own EventBus instance with this
// facade, so events published here and typed events published directly on the
// engine bus travel through one bus. It must be called at startup before any
// Subscribe; existing subscriptions are not migrated to the new backend.
func (b *Bus) SetBackend(be bus.EventBus) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.backend = be
}

func (b *Bus) Subscribe(t EventType, h Handler) {
	b.mu.RLock()
	be := b.backend
	b.mu.RUnlock()

	be.Subscribe(string(t), func(ctx context.Context, ev bus.Event) {
		le, ok := ev.(legacyEvent)
		if !ok {
			return
		}
		h(ctx, Event{Type: t, Payload: le.payload})
	})
}

func (b *Bus) Publish(ctx context.Context, t EventType, payload any) {
	b.mu.RLock()
	be := b.backend
	b.mu.RUnlock()

	_ = be.Publish(ctx, legacyEvent{t: t, payload: payload})
}
