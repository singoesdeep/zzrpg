package socket

import "sync"

// MessageRouter dispatches inbound WebSocket messages to per-type handlers.
// It replaces the single hand-written switch statement that previously lived in
// main(): each plugin registers the message types it owns via Handle, and the
// hub's read loop calls Dispatch. Handlers registered for an unknown type are
// simply not invoked (unknown messages are ignored, matching prior behaviour).
type MessageRouter struct {
	mu       sync.RWMutex
	handlers map[string]func(*Client, WSMessage)
	owners   map[string]string       // msgType -> owning plugin name ("" = ungated)
	gate     func(owner string) bool // nil = everything active
}

// NewMessageRouter returns an empty router.
func NewMessageRouter() *MessageRouter {
	return &MessageRouter{
		handlers: make(map[string]func(*Client, WSMessage)),
		owners:   make(map[string]string),
	}
}

// SetGate installs a predicate consulted before dispatching an owned message: a
// handler whose owner reports inactive is skipped. A nil gate (the default)
// treats every owner as active.
func (r *MessageRouter) SetGate(fn func(owner string) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gate = fn
}

// Handle registers h as the handler for messages whose Type == msgType. A
// second registration for the same type overwrites the first. Handlers
// registered this way are never gated (use HandleOwned for plugin-owned types).
func (r *MessageRouter) Handle(msgType string, h func(*Client, WSMessage)) {
	r.HandleOwned(msgType, "", h)
}

// HandleOwned registers h for msgType and records the owning plugin so Dispatch
// can suppress it while that plugin is deactivated.
func (r *MessageRouter) HandleOwned(msgType, owner string, h func(*Client, WSMessage)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[msgType] = h
	r.owners[msgType] = owner
}

// Dispatch routes msg to the handler registered for msg.Type, if any, unless
// the handler's owning plugin is currently deactivated.
func (r *MessageRouter) Dispatch(client *Client, msg WSMessage) {
	r.mu.RLock()
	h, ok := r.handlers[msg.Type]
	owner := r.owners[msg.Type]
	gate := r.gate
	r.mu.RUnlock()
	if !ok {
		return
	}
	if owner != "" && gate != nil && !gate(owner) {
		return
	}
	h(client, msg)
}
