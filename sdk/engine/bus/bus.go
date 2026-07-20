// Package bus provides a small, typed, in-process event bus.
//
// It replaces the old string-keyed events.Bus with an interface-based
// design (Event / Handler / EventBus) while preserving the exact runtime
// semantics of the previous implementation: asynchronous, fire-and-forget
// dispatch, one goroutine per handler, a context detached from the
// publisher's cancellation, and panic isolation so a misbehaving handler
// cannot crash the server.
package bus

import (
	"context"
	"log/slog"
	"sync"
)

// Event is a typed domain event. Name is the routing key used to match
// events to subscribers (it replaces the old string EventType).
// Implementations are typically plain structs.
type Event interface {
	// Name returns the routing key for this event.
	Name() string
}

// Handler reacts to an event. It runs asynchronously, on a context detached
// from the publisher's cancellation (see EventBus.Publish for details).
type Handler func(ctx context.Context, ev Event)

// Subscription lets a subscriber detach from further event delivery.
type Subscription interface {
	// Unsubscribe removes the associated handler so it no longer receives
	// events. It is safe to call concurrently with Publish, and safe to
	// call more than once (subsequent calls are a no-op).
	Unsubscribe()
}

// EventBus dispatches typed events to registered handlers.
type EventBus interface {
	// Publish dispatches ev to every handler currently subscribed to
	// ev.Name(). Dispatch is asynchronous and fire-and-forget: each handler
	// runs in its own goroutine on a context detached from ctx's
	// cancellation (via context.WithoutCancel), so handlers survive the
	// publisher's context being cancelled (e.g. the originating HTTP
	// request returning) while still inheriting any request-scoped
	// values. A panicking handler is recovered and logged; it does not
	// affect other handlers or the caller. Publishing an event with no
	// subscribers is a silent no-op. Publish itself never blocks on
	// handler execution and always returns nil.
	Publish(ctx context.Context, ev Event) error

	// Subscribe registers h to be invoked for every event whose Name()
	// equals name. It returns a Subscription that can be used to stop
	// receiving events.
	Subscribe(name string, h Handler) Subscription
}

// entry pairs a handler with a stable id so it can be located and removed
// again on Unsubscribe, even though Handler values are not comparable.
type entry struct {
	id int64
	fn Handler
}

// inProc is the default in-process EventBus implementation.
type inProc struct {
	log *slog.Logger

	mu       sync.RWMutex
	handlers map[string][]entry
	nextID   int64
}

// NewInProc returns the default in-process EventBus. If log is nil,
// slog.Default() is used for panic logging.
func NewInProc(log *slog.Logger) EventBus {
	if log == nil {
		log = slog.Default()
	}
	return &inProc{
		log:      log,
		handlers: make(map[string][]entry),
	}
}

// Publish implements EventBus.
func (b *inProc) Publish(ctx context.Context, ev Event) error {
	name := ev.Name()

	b.mu.RLock()
	entries := b.handlers[name]
	// Copy the slice header's backing data is not enough on its own to
	// guarantee safety against concurrent Unsubscribe mutating the slice
	// in place, so snapshot into a fresh slice while holding the lock.
	snapshot := make([]entry, len(entries))
	copy(snapshot, entries)
	b.mu.RUnlock()

	if len(snapshot) == 0 {
		return nil
	}

	// Detach from the caller's cancellation (typically an HTTP request
	// context): handlers run asynchronously and must survive the request
	// returning, while still inheriting request-scoped values.
	detached := context.WithoutCancel(ctx)

	for _, e := range snapshot {
		go func(h Handler) {
			// A panicking subscriber must not crash the whole server.
			defer func() {
				if rec := recover(); rec != nil {
					b.log.Error("event handler panicked", "event", name, "panic", rec)
				}
			}()
			h(detached, ev)
		}(e.fn)
	}

	return nil
}

// Subscribe implements EventBus.
func (b *inProc) Subscribe(name string, h Handler) Subscription {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.handlers[name] = append(b.handlers[name], entry{id: id, fn: h})
	b.mu.Unlock()

	return &subscription{bus: b, name: name, id: id}
}

// unsubscribe removes the handler identified by id from the name bucket.
func (b *inProc) unsubscribe(name string, id int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entries := b.handlers[name]
	for i, e := range entries {
		if e.id == id {
			// Remove without preserving order; order is not part of the
			// contract.
			entries[i] = entries[len(entries)-1]
			b.handlers[name] = entries[:len(entries)-1]
			break
		}
	}
}

// subscription is the Subscription returned by inProc.Subscribe.
type subscription struct {
	bus  *inProc
	name string
	id   int64

	once sync.Once
}

// Unsubscribe implements Subscription.
func (s *subscription) Unsubscribe() {
	s.once.Do(func() {
		s.bus.unsubscribe(s.name, s.id)
	})
}
