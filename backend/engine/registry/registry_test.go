package registry

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

type fakeService struct {
	name string
}

type otherService struct {
	value int
}

func TestProvideResolveHappyPath(t *testing.T) {
	r := New()

	svc := &fakeService{name: "combat"}
	if err := Provide[*fakeService](r, "combat", svc); err != nil {
		t.Fatalf("Provide returned unexpected error: %v", err)
	}

	got, err := Resolve[*fakeService](r, "combat")
	if err != nil {
		t.Fatalf("Resolve returned unexpected error: %v", err)
	}
	if got != svc {
		t.Fatalf("Resolve returned %+v, want %+v", got, svc)
	}
}

func TestProvideDuplicateName(t *testing.T) {
	r := New()

	if err := Provide[int](r, "dup", 1); err != nil {
		t.Fatalf("first Provide returned unexpected error: %v", err)
	}
	err := Provide[int](r, "dup", 2)
	if err == nil {
		t.Fatal("expected error registering duplicate name, got nil")
	}
	if !strings.Contains(err.Error(), "dup") {
		t.Fatalf("expected error to name the conflicting key %q, got: %v", "dup", err)
	}
}

func TestResolveMissingName(t *testing.T) {
	r := New()

	_, err := Resolve[int](r, "missing")
	if err == nil {
		t.Fatal("expected error resolving missing name, got nil")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected error to name the missing key %q, got: %v", "missing", err)
	}
}

func TestResolveTypeMismatch(t *testing.T) {
	r := New()

	if err := Provide[*fakeService](r, "svc", &fakeService{name: "x"}); err != nil {
		t.Fatalf("Provide returned unexpected error: %v", err)
	}

	_, err := Resolve[*otherService](r, "svc")
	if err == nil {
		t.Fatal("expected type-mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "svc") {
		t.Fatalf("expected error to name the key %q, got: %v", "svc", err)
	}
	if !strings.Contains(err.Error(), "otherService") || !strings.Contains(err.Error(), "fakeService") {
		t.Fatalf("expected error to name both requested and stored types, got: %v", err)
	}
}

func TestMustResolvePanicsOnMissing(t *testing.T) {
	r := New()

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected MustResolve to panic on missing name, but it did not")
		}
	}()

	MustResolve[int](r, "nope")
	t.Fatal("unreachable: MustResolve should have panicked")
}

func TestMustResolveSucceeds(t *testing.T) {
	r := New()

	if err := Provide[string](r, "greeting", "hello"); err != nil {
		t.Fatalf("Provide returned unexpected error: %v", err)
	}

	got := MustResolve[string](r, "greeting")
	if got != "hello" {
		t.Fatalf("MustResolve returned %q, want %q", got, "hello")
	}
}

func TestNamesSorted(t *testing.T) {
	r := New()

	names := []string{"zeta", "alpha", "mike"}
	for i, n := range names {
		if err := Provide[int](r, n, i); err != nil {
			t.Fatalf("Provide(%q) returned unexpected error: %v", n, err)
		}
	}

	got := Names(r)
	want := []string{"alpha", "mike", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
	}
}

func TestNamesEmpty(t *testing.T) {
	r := New()
	got := Names(r)
	if len(got) != 0 {
		t.Fatalf("Names() on empty registry = %v, want empty", got)
	}
}

func TestConcurrentProvideResolve(t *testing.T) {
	r := New()

	const workers = 50
	var wg sync.WaitGroup
	wg.Add(workers * 2)

	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("svc-%d", i)
			_ = Provide[int](r, name, i)
		}()
	}

	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("svc-%d", i)
			// May race with the Provide goroutine above; either
			// outcome (found or not-found) is valid, we're only
			// checking for races/panics under -race.
			_, _ = Resolve[int](r, name)
		}()
	}

	wg.Wait()

	// After all writes have completed, every name should be resolvable
	// and hold the expected value.
	for i := 0; i < workers; i++ {
		name := fmt.Sprintf("svc-%d", i)
		got, err := Resolve[int](r, name)
		if err != nil {
			t.Fatalf("Resolve(%q) returned unexpected error after wg.Wait: %v", name, err)
		}
		if got != i {
			t.Fatalf("Resolve(%q) = %d, want %d", name, got, i)
		}
	}
}

func TestDefineContentTypeDuplicate(t *testing.T) {
	r := New()

	ct := ContentType{
		Kind: "loot_table",
		Register: func(id string, raw []byte) error {
			return r.StoreContent("loot_table", id, string(raw))
		},
	}

	if err := r.DefineContentType(ct); err != nil {
		t.Fatalf("first DefineContentType returned unexpected error: %v", err)
	}

	err := r.DefineContentType(ct)
	if err == nil {
		t.Fatal("expected error defining duplicate content type, got nil")
	}
	if !strings.Contains(err.Error(), "loot_table") {
		t.Fatalf("expected error to name the conflicting kind %q, got: %v", "loot_table", err)
	}
}

func TestLookupMiss(t *testing.T) {
	r := New()

	_, ok := r.Lookup("mob", "goblin")
	if ok {
		t.Fatal("expected Lookup to report miss on empty registry, got hit")
	}
}

func TestStoreContentAndLookupRoundtrip(t *testing.T) {
	r := New()

	type lootTable struct {
		Entries []string
	}
	want := lootTable{Entries: []string{"gold", "sword"}}

	if err := r.StoreContent("loot_table", "goblin_drop", want); err != nil {
		t.Fatalf("StoreContent returned unexpected error: %v", err)
	}

	got, ok := r.Lookup("loot_table", "goblin_drop")
	if !ok {
		t.Fatal("expected Lookup hit after StoreContent, got miss")
	}
	typed, ok := got.(lootTable)
	if !ok {
		t.Fatalf("Lookup returned value of type %T, want lootTable", got)
	}
	if len(typed.Entries) != 2 || typed.Entries[0] != "gold" || typed.Entries[1] != "sword" {
		t.Fatalf("Lookup returned %+v, want %+v", typed, want)
	}
}

func TestContentTypeRegisterIntegration(t *testing.T) {
	r := New()

	ct := ContentType{
		Kind: "class",
		Register: func(id string, raw []byte) error {
			return r.StoreContent("class", id, string(raw))
		},
	}

	if err := r.DefineContentType(ct); err != nil {
		t.Fatalf("DefineContentType returned unexpected error: %v", err)
	}

	if err := ct.Register("warrior", []byte("raw-warrior-data")); err != nil {
		t.Fatalf("Register returned unexpected error: %v", err)
	}

	got, ok := r.Lookup("class", "warrior")
	if !ok {
		t.Fatal("expected Lookup hit after Register, got miss")
	}
	if got != "raw-warrior-data" {
		t.Fatalf("Lookup returned %v, want %q", got, "raw-warrior-data")
	}
}

func TestLookupDifferentKindsDoNotCollide(t *testing.T) {
	r := New()

	if err := r.StoreContent("class", "shared_id", "class-value"); err != nil {
		t.Fatalf("StoreContent returned unexpected error: %v", err)
	}
	if err := r.StoreContent("mob", "shared_id", "mob-value"); err != nil {
		t.Fatalf("StoreContent returned unexpected error: %v", err)
	}

	classVal, ok := r.Lookup("class", "shared_id")
	if !ok || classVal != "class-value" {
		t.Fatalf("Lookup(class, shared_id) = %v, %v; want class-value, true", classVal, ok)
	}

	mobVal, ok := r.Lookup("mob", "shared_id")
	if !ok || mobVal != "mob-value" {
		t.Fatalf("Lookup(mob, shared_id) = %v, %v; want mob-value, true", mobVal, ok)
	}
}

func TestConcurrentStoreContentAndLookup(t *testing.T) {
	r := New()

	const workers = 50
	var wg sync.WaitGroup
	wg.Add(workers * 2)

	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("item-%d", i)
			_ = r.StoreContent("item", id, i)
		}()
	}

	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("item-%d", i)
			_, _ = r.Lookup("item", id)
		}()
	}

	wg.Wait()

	for i := 0; i < workers; i++ {
		id := fmt.Sprintf("item-%d", i)
		got, ok := r.Lookup("item", id)
		if !ok {
			t.Fatalf("Lookup(item, %q) missed after wg.Wait", id)
		}
		if got != i {
			t.Fatalf("Lookup(item, %q) = %v, want %d", id, got, i)
		}
	}
}
