package socket

import "testing"

func TestMessageRouter_GateSuppressesOwnedHandler(t *testing.T) {
	r := NewMessageRouter()

	var ungated, owned int
	r.Handle("CHAT", func(*Client, WSMessage) { ungated++ })
	r.HandleOwned("COMBAT_ATTACK", "combat", func(*Client, WSMessage) { owned++ })

	active := true
	r.SetGate(func(owner string) bool { return active })

	// While active, both handlers fire.
	r.Dispatch(nil, WSMessage{Type: "CHAT"})
	r.Dispatch(nil, WSMessage{Type: "COMBAT_ATTACK"})
	if ungated != 1 || owned != 1 {
		t.Fatalf("active dispatch: ungated=%d owned=%d, want 1/1", ungated, owned)
	}

	// Deactivate combat: the owned handler is suppressed, the ungated one is not.
	active = false
	r.Dispatch(nil, WSMessage{Type: "CHAT"})
	r.Dispatch(nil, WSMessage{Type: "COMBAT_ATTACK"})
	if ungated != 2 {
		t.Fatalf("ungated handler must ignore gate, got %d", ungated)
	}
	if owned != 1 {
		t.Fatalf("owned handler must be suppressed while deactivated, got %d", owned)
	}

	// Reactivate: the owned handler fires again.
	active = true
	r.Dispatch(nil, WSMessage{Type: "COMBAT_ATTACK"})
	if owned != 2 {
		t.Fatalf("owned handler must resume after reactivation, got %d", owned)
	}
}
