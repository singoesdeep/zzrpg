package eventstream

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/outbox"
)

type streamTestEvent struct{ V int }

func (streamTestEvent) Name() string { return "stream_test_event" }

// TestCrossNodeFanout proves the horizontal-scale seam against live Redis: an
// event published by node "A" is delivered to node "B"'s local bus by B's
// consumer, while A's own consumer skips it (origin de-dup). Requires Redis.
func TestCrossNodeFanout(t *testing.T) {
	ctx := context.Background()
	client, err := Dial(ctx, "redis://localhost:6379")
	if err != nil {
		t.Skip("Redis not accessible on localhost:6379, skipping event-stream test.")
	}
	defer client.Close()

	stream := fmt.Sprintf("zzrpg:test:%d", time.Now().UnixNano())
	defer client.Del(context.Background(), stream)

	reg := outbox.NewRegistry()
	reg.Register(streamTestEvent{}.Name(), outbox.JSONDecoder[streamTestEvent]())

	busA := bus.NewInProc(nil)
	busB := bus.NewInProc(nil)
	gotA := make(chan bus.Event, 4)
	gotB := make(chan bus.Event, 4)
	busA.Subscribe("stream_test_event", func(_ context.Context, ev bus.Event) { gotA <- ev })
	busB.Subscribe("stream_test_event", func(_ context.Context, ev bus.Event) { gotB <- ev })

	consA := NewConsumer(client, busA.Publish, reg, stream, "A", nil)
	consB := NewConsumer(client, busB.Publish, reg, stream, "B", nil)

	// Create both groups before publishing so the message is "never-delivered"
	// (>) for each group regardless of when its Run loop starts reading.
	if err := consA.ensureGroup(ctx); err != nil {
		t.Fatalf("ensure group A: %v", err)
	}
	if err := consB.ensureGroup(ctx); err != nil {
		t.Fatalf("ensure group B: %v", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go consA.Run(runCtx)
	go consB.Run(runCtx)

	// Node A produces an event.
	pub := NewPublisher(client, stream, "A")
	if err := pub.Publish(ctx, streamTestEvent{V: 99}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Node B's bus must observe it.
	select {
	case ev := <-gotB:
		te, ok := ev.(streamTestEvent)
		if !ok || te.V != 99 {
			t.Errorf("node B got unexpected event: %#v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out: node B did not receive the cross-node event")
	}

	// Node A must NOT re-deliver its own event (it was already handled locally).
	select {
	case ev := <-gotA:
		t.Errorf("node A should have skipped its own event, but got %#v", ev)
	case <-time.After(300 * time.Millisecond):
		// expected: nothing arrives on A
	}
}
