package contracts

import (
	"encoding/json"
	"testing"
)

// TestModifierJSONParity guarantees that the unified Modifier serializes exactly
// like the previous items.StatModifier when no SourceID is set (the persisted
// case), so item_definitions.stats_modifiers / inventories.custom_modifiers
// JSONB round-trips unchanged.
func TestModifierJSONParity(t *testing.T) {
	m := Modifier{Stat: "ATTACK", Operation: "ADD", Value: 12, Priority: 20}

	got, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"stat":"ATTACK","operation":"ADD","value":12,"priority":20}`
	if string(got) != want {
		t.Errorf("persisted JSON changed:\n got %s\nwant %s", got, want)
	}

	// Legacy rows with no source_id must unmarshal cleanly to an empty SourceID.
	var back Modifier
	if err := json.Unmarshal([]byte(want), &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back != m {
		t.Errorf("round-trip mismatch: got %+v, want %+v", back, m)
	}

	// When SourceID is set (in-memory equipment mods, never persisted) it appears.
	withSource, _ := json.Marshal(Modifier{Stat: "HP", Operation: "ADD", Value: 5, Priority: 20, SourceID: "sword_1"})
	wantWith := `{"stat":"HP","operation":"ADD","value":5,"priority":20,"source_id":"sword_1"}`
	if string(withSource) != wantWith {
		t.Errorf("source_id JSON:\n got %s\nwant %s", withSource, wantWith)
	}
}
