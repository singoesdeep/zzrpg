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
}

// NewMessageRouter returns an empty router.
func NewMessageRouter() *MessageRouter {
	return &MessageRouter{handlers: make(map[string]func(*Client, WSMessage))}
}

// Handle registers h as the handler for messages whose Type == msgType. A
// second registration for the same type overwrites the first.
func (r *MessageRouter) Handle(msgType string, h func(*Client, WSMessage)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[msgType] = h
}

// Dispatch routes msg to the handler registered for msg.Type, if any.
func (r *MessageRouter) Dispatch(client *Client, msg WSMessage) {
	r.mu.RLock()
	h, ok := r.handlers[msg.Type]
	r.mu.RUnlock()
	if ok {
		h(client, msg)
	}
}
