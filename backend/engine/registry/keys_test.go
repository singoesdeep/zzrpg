package registry_test

import (
	"testing"

	"github.com/singoesdeep/zzrpg/backend/engine/registry"
)

type greeter interface{ Greet() string }

type enGreeter struct{}

func (enGreeter) Greet() string { return "hello" }

func TestTypedKeys_RoundTrip(t *testing.T) {
	r := registry.New()
	key := registry.NewKey[greeter]("greeter")

	var g greeter = enGreeter{}
	if err := registry.ProvideKey(r, key, g); err != nil {
		t.Fatalf("provide: %v", err)
	}
	got, err := registry.ResolveKey(r, key)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Greet() != "hello" {
		t.Fatalf("unexpected value: %q", got.Greet())
	}

	// Interop with the string API: same slot.
	if key.Name() != "greeter" {
		t.Fatalf("name = %q", key.Name())
	}
	if _, err := registry.Resolve[greeter](r, "greeter"); err != nil {
		t.Fatalf("string resolve of typed key failed: %v", err)
	}
}

func TestTypedKeys_MissingResolves(t *testing.T) {
	r := registry.New()
	key := registry.NewKey[greeter]("absent")
	if _, err := registry.ResolveKey(r, key); err == nil {
		t.Fatal("expected error resolving an unregistered key")
	}
}
