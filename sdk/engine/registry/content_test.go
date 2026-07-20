package registry_test

import (
	"testing"

	"github.com/singoesdeep/zzrpg/sdk/engine/registry"
)

// cardDef is a made-up content type a hypothetical card-game plugin might define
// entirely on its own — the engine knows nothing about it.
type cardDef struct {
	Name string `json:"name"`
	Cost int    `json:"cost"`
}

func TestTypedContent_DefineLoadLookup(t *testing.T) {
	r := registry.New()

	if err := registry.DefineContent[cardDef](r, "card"); err != nil {
		t.Fatalf("define: %v", err)
	}
	// redefining the same kind is rejected
	if err := registry.DefineContent[cardDef](r, "card"); err == nil {
		t.Fatal("expected error redefining a content kind")
	}

	if err := r.LoadContent("card", "fireball", []byte(`{"name":"Fireball","cost":3}`)); err != nil {
		t.Fatalf("load: %v", err)
	}

	got, ok := registry.Content[cardDef](r, "card", "fireball")
	if !ok {
		t.Fatal("expected fireball to be found")
	}
	if got.Name != "Fireball" || got.Cost != 3 {
		t.Fatalf("unexpected content: %+v", got)
	}

	// missing id
	if _, ok := registry.Content[cardDef](r, "card", "absent"); ok {
		t.Fatal("did not expect an absent card")
	}
	// loading an undefined kind errors
	if err := r.LoadContent("spell", "x", []byte(`{}`)); err == nil {
		t.Fatal("expected error loading an undefined content kind")
	}
	// bad JSON surfaces a parse error
	if err := r.LoadContent("card", "broken", []byte(`{not json`)); err == nil {
		t.Fatal("expected parse error on bad JSON")
	}
}
