package events

import (
	"context"
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

	event := Event{Type: t, Payload: payload}
	for _, h := range handlers {
		go h(ctx, event) // Run async
	}
}
