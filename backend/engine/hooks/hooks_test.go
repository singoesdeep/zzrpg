package hooks

import (
	"context"
	"errors"
	"testing"
)

func TestApplyFiltersThreadsValueInPriorityOrder(t *testing.T) {
	h := New(nil)
	ctx := context.Background()

	// Registered out of priority order; higher priority runs later, so it sees
	// the earlier filter's output. add 1 (prio 10), then double (prio 20).
	AddFilter(h, "n", 20, func(_ context.Context, v int) int { return v * 2 })
	AddFilter(h, "n", 10, func(_ context.Context, v int) int { return v + 1 })

	// (5 + 1) * 2 = 12
	if got := ApplyFilters(h, ctx, "n", 5); got != 12 {
		t.Errorf("expected 12, got %d", got)
	}
}

func TestApplyFiltersStableWithinSamePriority(t *testing.T) {
	h := New(nil)
	order := []string{}
	AddFilter(h, "s", 10, func(_ context.Context, v string) string { order = append(order, "a"); return v })
	AddFilter(h, "s", 10, func(_ context.Context, v string) string { order = append(order, "b"); return v })
	ApplyFilters(h, context.Background(), "s", "x")
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Errorf("expected registration order a,b within same priority, got %v", order)
	}
}

func TestApplyFiltersNoFiltersReturnsValue(t *testing.T) {
	h := New(nil)
	if got := ApplyFilters(h, context.Background(), "missing", 42); got != 42 {
		t.Errorf("expected unchanged 42, got %d", got)
	}
}

func TestApplyFiltersNilHooksIsPassthrough(t *testing.T) {
	var h *Hooks // nil
	if got := ApplyFilters(h, context.Background(), "n", 7); got != 7 {
		t.Errorf("nil hooks should pass the value through, got %d", got)
	}
}

func TestApplyFiltersPanicIsolation(t *testing.T) {
	h := New(nil)
	AddFilter(h, "p", 10, func(_ context.Context, v int) int { panic("boom") })
	AddFilter(h, "p", 20, func(_ context.Context, v int) int { return v + 100 })
	// The panicking filter passes the value through unchanged; the next still runs.
	if got := ApplyFilters(h, context.Background(), "p", 1); got != 101 {
		t.Errorf("expected panic-isolated chain to yield 101, got %d", got)
	}
}

func TestApplyFiltersTypeMismatchSkipped(t *testing.T) {
	h := New(nil)
	// Registered as a string filter, but applied as int — mismatch, skipped.
	AddFilter(h, "m", 10, func(_ context.Context, v string) string { return v + "!" })
	if got := ApplyFilters(h, context.Background(), "m", 5); got != 5 {
		t.Errorf("mismatched filter should be skipped, got %d", got)
	}
}

func TestDoActionRunsInOrderAndGatesOnError(t *testing.T) {
	h := New(nil)
	var ran []int
	AddAction(h, "g", 10, func(_ context.Context, v int) error { ran = append(ran, 1); return nil })
	AddAction(h, "g", 20, func(_ context.Context, v int) error { ran = append(ran, 2); return errors.New("veto") })
	AddAction(h, "g", 30, func(_ context.Context, v int) error { ran = append(ran, 3); return nil })

	err := DoAction(h, context.Background(), "g", 0)
	if err == nil || err.Error() != "veto" {
		t.Errorf("expected the veto error, got %v", err)
	}
	// The third action must not run (chain aborted).
	if len(ran) != 2 || ran[0] != 1 || ran[1] != 2 {
		t.Errorf("expected actions 1,2 then abort, got %v", ran)
	}
}

func TestDoActionPanicDoesNotAbort(t *testing.T) {
	h := New(nil)
	reached := false
	AddAction(h, "pa", 10, func(_ context.Context, v int) error { panic("boom") })
	AddAction(h, "pa", 20, func(_ context.Context, v int) error { reached = true; return nil })
	if err := DoAction(h, context.Background(), "pa", 0); err != nil {
		t.Errorf("panic should not surface as an error, got %v", err)
	}
	if !reached {
		t.Error("a panicking action must not abort the chain")
	}
}

func TestDoActionNilHooks(t *testing.T) {
	var h *Hooks
	if err := DoAction(h, context.Background(), "x", 0); err != nil {
		t.Errorf("nil hooks DoAction should be a no-op, got %v", err)
	}
}
