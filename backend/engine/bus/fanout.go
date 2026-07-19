package bus

import (
	"context"
	"sync"
)

// Fanout decorates an EventBus so that every locally-published event is also
// handed to an optional forwarder — used to broadcast events to other nodes over
// a transport such as Redis Streams. Subscribe is delegated unchanged, so local
// subscribers behave exactly as with the inner bus.
//
// Re-injecting an event that arrived from another node must use PublishLocal, so
// it is delivered to local subscribers WITHOUT being forwarded again (which would
// loop it back around the cluster).
//
// With no forwarder installed, Fanout is a transparent pass-through: the app runs
// single-node exactly as it would on the bare inner bus.
type Fanout struct {
	inner EventBus

	mu      sync.RWMutex
	forward func(context.Context, Event)
}

// NewFanout wraps inner. Until SetForwarder is called it is a pass-through.
func NewFanout(inner EventBus) *Fanout {
	return &Fanout{inner: inner}
}

// SetForwarder installs (or, with nil, clears) the hook invoked with every event
// published through Publish, after local delivery. Safe to call concurrently.
func (f *Fanout) SetForwarder(fn func(context.Context, Event)) {
	f.mu.Lock()
	f.forward = fn
	f.mu.Unlock()
}

// Publish delivers ev to local subscribers and then forwards it to other nodes
// (if a forwarder is installed). It never blocks on either.
func (f *Fanout) Publish(ctx context.Context, ev Event) error {
	_ = f.inner.Publish(ctx, ev)
	f.mu.RLock()
	fwd := f.forward
	f.mu.RUnlock()
	if fwd != nil {
		fwd(ctx, ev)
	}
	return nil
}

// PublishLocal delivers ev to local subscribers only, without forwarding. The
// cross-node consumer uses it to re-inject remote events without looping.
func (f *Fanout) PublishLocal(ctx context.Context, ev Event) error {
	return f.inner.Publish(ctx, ev)
}

// Subscribe registers h on the inner bus.
func (f *Fanout) Subscribe(name string, h Handler) Subscription {
	return f.inner.Subscribe(name, h)
}
