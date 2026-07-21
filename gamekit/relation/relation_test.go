package relation

import (
	"context"
	"reflect"
	"testing"
)

func TestGraphLinkQueryBothDirections(t *testing.T) {
	ctx := context.Background()
	r := NewMemRepo()

	// A city (1) contains three buildings; building 20 is also contained by a
	// second city (2) to prove From() aggregates.
	for _, b := range []int64{10, 20, 30} {
		if err := r.Link(ctx, 1, "contains", b); err != nil {
			t.Fatal(err)
		}
	}
	_ = r.Link(ctx, 1, "contains", 20) // idempotent
	_ = r.Link(ctx, 2, "contains", 20)
	_ = r.Link(ctx, 1, "allied_with", 2) // different type, must not leak

	to, _ := r.To(ctx, 1, "contains")
	if !reflect.DeepEqual(to, []int64{10, 20, 30}) {
		t.Fatalf("city 1 contains = %v, want [10 20 30]", to)
	}
	from, _ := r.From(ctx, 20, "contains")
	if !reflect.DeepEqual(from, []int64{1, 2}) {
		t.Fatalf("building 20 contained by = %v, want [1 2]", from)
	}

	if ok, _ := r.Exists(ctx, 1, "contains", 10); !ok {
		t.Fatal("edge 1-contains-10 should exist")
	}
	if err := r.Unlink(ctx, 1, "contains", 10); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.Exists(ctx, 1, "contains", 10); ok {
		t.Fatal("edge 1-contains-10 should be gone")
	}
	to, _ = r.To(ctx, 1, "contains")
	if !reflect.DeepEqual(to, []int64{20, 30}) {
		t.Fatalf("after unlink city 1 contains = %v, want [20 30]", to)
	}
}
