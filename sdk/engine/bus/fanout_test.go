package bus

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fanoutTestEvent struct{ N int }

func (fanoutTestEvent) Name() string { return "fanout_test_event" }

// TestFanoutForwardsOnPublishNotLocal proves the decorator's contract: Publish
// delivers locally AND forwards; PublishLocal delivers locally only (so
// re-injected remote events don't loop). With no forwarder it is a pass-through.
func TestFanoutForwardsOnPublishNotLocal(t *testing.T) {
	fb := NewFanout(NewInProc(nil))

	delivered := make(chan Event, 8)
	fb.Subscribe("fanout_test_event", func(_ context.Context, ev Event) { delivered <- ev })

	var mu sync.Mutex
	var forwarded int
	fb.SetForwarder(func(_ context.Context, ev Event) {
		mu.Lock()
		forwarded++
		mu.Unlock()
	})

	_ = fb.Publish(context.Background(), fanoutTestEvent{N: 1})      // local + forward
	_ = fb.PublishLocal(context.Background(), fanoutTestEvent{N: 2}) // local only

	// Both events are delivered to the local subscriber.
	for i := 0; i < 2; i++ {
		select {
		case <-delivered:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for local delivery %d", i+1)
		}
	}

	// Only the Publish call forwarded (the forwarder runs synchronously in Publish).
	mu.Lock()
	got := forwarded
	mu.Unlock()
	if got != 1 {
		t.Errorf("expected exactly 1 forwarded event, got %d", got)
	}
}
