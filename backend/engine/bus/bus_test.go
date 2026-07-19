package bus

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// testEvent is a minimal Event implementation for tests.
type testEvent struct{ name string }

func (e testEvent) Name() string { return e.name }

func waitOn(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal(msg)
	}
}

func TestPublishDeliversToAllSubscribers(t *testing.T) {
	b := NewInProc(slog.Default())

	const n = 5
	var wg sync.WaitGroup
	wg.Add(n)

	got := make([]bool, n)
	var mu sync.Mutex

	for i := 0; i < n; i++ {
		i := i
		b.Subscribe("thing.happened", func(ctx context.Context, ev Event) {
			mu.Lock()
			got[i] = true
			mu.Unlock()
			wg.Done()
		})
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	if err := b.Publish(context.Background(), testEvent{name: "thing.happened"}); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}

	waitOn(t, done, "not all subscribers received the event in time")

	mu.Lock()
	defer mu.Unlock()
	for i, v := range got {
		if !v {
			t.Errorf("subscriber %d did not receive the event", i)
		}
	}
}

func TestPublishNoSubscribersIsNoOp(t *testing.T) {
	b := NewInProc(slog.Default())

	if err := b.Publish(context.Background(), testEvent{name: "nobody.listening"}); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
}

func TestPanickingHandlerDoesNotAffectOthers(t *testing.T) {
	b := NewInProc(slog.Default())

	otherCalled := make(chan struct{})
	panicked := make(chan struct{})

	b.Subscribe("risky", func(ctx context.Context, ev Event) {
		close(panicked)
		panic("boom")
	})
	b.Subscribe("risky", func(ctx context.Context, ev Event) {
		close(otherCalled)
	})

	if err := b.Publish(context.Background(), testEvent{name: "risky"}); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}

	waitOn(t, panicked, "panicking handler never ran")
	waitOn(t, otherCalled, "other handler was not called; panic likely propagated")
}

func TestDetachedContextSurvivesCancellation(t *testing.T) {
	b := NewInProc(slog.Default())

	ranAfterCancel := make(chan error, 1)

	b.Subscribe("ctx.detach", func(ctx context.Context, ev Event) {
		// Give the caller a moment to cancel before we check ctx.Err().
		time.Sleep(50 * time.Millisecond)
		ranAfterCancel <- ctx.Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	if err := b.Publish(ctx, testEvent{name: "ctx.detach"}); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	cancel() // cancel immediately after Publish returns

	select {
	case err := <-ranAfterCancel:
		if err != nil {
			t.Fatalf("handler observed a cancelled context: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler never ran after context cancellation")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	b := NewInProc(slog.Default())

	calls := make(chan struct{}, 10)
	sub := b.Subscribe("stoppable", func(ctx context.Context, ev Event) {
		calls <- struct{}{}
	})

	if err := b.Publish(context.Background(), testEvent{name: "stoppable"}); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	waitOn(t, calls, "handler did not fire before unsubscribe")

	sub.Unsubscribe()

	if err := b.Publish(context.Background(), testEvent{name: "stoppable"}); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}

	select {
	case <-calls:
		t.Fatal("handler fired after Unsubscribe")
	case <-time.After(200 * time.Millisecond):
		// expected: no further delivery
	}
}

func TestDoubleUnsubscribeIsNoOp(t *testing.T) {
	b := NewInProc(slog.Default())

	sub := b.Subscribe("thing", func(ctx context.Context, ev Event) {})

	sub.Unsubscribe()
	sub.Unsubscribe() // must not panic or error
}

func TestNewInProcNilLoggerFallsBackToDefault(t *testing.T) {
	b := NewInProc(nil)
	if b == nil {
		t.Fatal("NewInProc(nil) returned nil")
	}
	// Exercise a panic path to make sure the fallback logger is usable.
	done := make(chan struct{})
	b.Subscribe("nil.logger", func(ctx context.Context, ev Event) {
		defer close(done)
		panic("boom")
	})
	if err := b.Publish(context.Background(), testEvent{name: "nil.logger"}); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	waitOn(t, done, "handler with nil-logger bus never ran")
}
