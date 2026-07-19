package socket

import (
	"testing"
	"time"
)

// TestHubBroadcastDoesNotDeadlockOnFullBuffer is a regression test for the
// broadcast self-deadlock: previously, when a client's Send buffer was full the
// Hub.Run broadcast case sent to h.Unregister — a channel only Run itself reads —
// while Run was busy inside that case, freezing the entire hub. The hub must now
// keep processing after dropping a slow consumer.
func TestHubBroadcastDoesNotDeadlockOnFullBuffer(t *testing.T) {
	h := NewHub()
	go h.Run()

	// A client whose (cap 1) send buffer we deliberately fill and never drain.
	slow := &Client{Send: make(chan []byte, 1)}
	h.Register <- slow
	slow.Send <- []byte("fill") // buffer is now full

	done := make(chan struct{})
	go func() {
		// Broadcasting to a full-buffer client must not block the hub.
		h.Broadcast <- []byte("hello")
		// If the hub is still alive it will accept another registration.
		h.Register <- &Client{Send: make(chan []byte, 1)}
		close(done)
	}()

	select {
	case <-done:
		// Hub processed the broadcast and a subsequent register: no deadlock.
	case <-time.After(2 * time.Second):
		t.Fatal("hub deadlocked while broadcasting to a full-buffer client")
	}
}

// TestHubAssociateCharacterOverrideNoDeadlock covers the connection-override
// path, which previously sent to h.Unregister while holding h.mu (the same lock
// Run's Unregister handler needs).
func TestHubAssociateCharacterOverrideNoDeadlock(t *testing.T) {
	h := NewHub()
	go h.Run()

	first := &Client{Send: make(chan []byte, 1)}
	second := &Client{Send: make(chan []byte, 1)}
	h.Register <- first
	h.Register <- second

	done := make(chan struct{})
	go func() {
		h.AssociateCharacter(first, 42)
		h.AssociateCharacter(second, 42) // overrides first
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("AssociateCharacter override deadlocked")
	}

	if c, ok := h.GetClientByCharacterID(42); !ok || c != second {
		t.Fatalf("expected character 42 to map to the second client after override")
	}
}
