package idle_test

import (
	"testing"

	eidle "github.com/singoesdeep/zzrpg/backend/engine/idle"
)

func TestWindow(t *testing.T) {
	if _, ok := eidle.Window(5, 60, 0); ok {
		t.Fatal("below min should not accrue")
	}
	min, ok := eidle.Window(600, 60, 0)
	if !ok || min != 10 {
		t.Fatalf("600s over min 60 = 10 min, got %v ok=%v", min, ok)
	}
	// cap clamps
	min, ok = eidle.Window(100000, 60, 3600)
	if !ok || min != 60 {
		t.Fatalf("capped at 3600s = 60 min, got %v", min)
	}
}

func TestOutputAddAndDrop(t *testing.T) {
	var o eidle.Output
	o.Add("gold", 5)
	o.Add("gold", 3)
	o.Add("exp", 0) // ignored
	o.AddDrop("wood", 4)
	o.AddDrop("stone", 0) // ignored
	if o.Amounts["gold"] != 8 {
		t.Fatalf("gold = %d, want 8", o.Amounts["gold"])
	}
	if _, ok := o.Amounts["exp"]; ok {
		t.Fatal("zero amount should not be recorded")
	}
	if len(o.Drops) != 1 || o.Drops[0].ID != "wood" {
		t.Fatalf("expected a single wood drop, got %+v", o.Drops)
	}
}

type fixedProducer struct{ unlocked bool }

func (f fixedProducer) Unlocked(eidle.State) bool { return f.unlocked }
func (f fixedProducer) Produce(min float64, _ eidle.State, _ func() float64) eidle.Output {
	var o eidle.Output
	o.Add("x", int64(min))
	return o
}

func TestRegistry(t *testing.T) {
	r := eidle.NewRegistry()
	r.Register("a", fixedProducer{unlocked: true})
	r.Register("b", fixedProducer{unlocked: false})
	if _, ok := r.Get("a"); !ok {
		t.Fatal("expected producer a")
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("did not expect a missing producer")
	}
	if ids := r.IDs(); len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("expected registration order [a b], got %v", ids)
	}
}
