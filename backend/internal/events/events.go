package events

import (
	"context"
	"log/slog"
	"sync"
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

type Bus struct {
	mu       sync.RWMutex
	handlers map[EventType][]Handler
}

var globalBus = &Bus{
	handlers: make(map[EventType][]Handler),
}

func Global() *Bus {
	return globalBus
}

func (b *Bus) Subscribe(t EventType, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[t] = append(b.handlers[t], h)
}

func (b *Bus) Publish(ctx context.Context, t EventType, payload any) {
	b.mu.RLock()
	handlers, ok := b.handlers[t]
	b.mu.RUnlock()

	if !ok {
		return
	}

	// Detach from the caller's cancellation (typically an HTTP request context):
	// handlers run asynchronously and must survive the request returning, while
	// still inheriting request-scoped values. Without this, a handler such as the
	// equip → stat-recalculation trigger would routinely fail with "context
	// canceled" the moment its originating request completed.
	detached := context.WithoutCancel(ctx)
	event := Event{Type: t, Payload: payload}
	for _, h := range handlers {
		go func(h Handler) {
			// A panicking subscriber must not crash the whole server.
			defer func() {
				if rec := recover(); rec != nil {
					slog.Error("event handler panicked", "event", t, "panic", rec)
				}
			}()
			h(detached, event)
		}(h)
	}
}
