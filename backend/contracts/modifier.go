// Package contracts holds shared value objects used across domains — the small
// engine "kernel" types that would otherwise be re-declared (and manually
// translated) in every package that touches them.
package contracts

// Modifier is a single stat modifier: it adds to or multiplies a named stat.
// It is the one shared shape for equipment bonuses, item stat rolls, skills,
// and buffs, replacing the previously duplicated character.EquipmentModifier,
// statclient.Modifier, and items.StatModifier.
//
// The JSON tags match the historical items.StatModifier persistence format
// (item_definitions.stats_modifiers / inventories.custom_modifiers JSONB), so
// stored data round-trips unchanged. SourceID is omitempty: it is only set for
// in-memory equipment modifiers (never persisted), so item JSON is byte-for-byte
// identical to before.
type Modifier struct {
	Stat      string  `json:"stat"`      // "HP", "MP", "STR", "INT", "DEX", "CON", "ATTACK", "DEFENSE", "CRIT_RATE"
	Operation string  `json:"operation"` // "ADD", "MULTIPLY"
	Value     float64 `json:"value"`
	Priority  int32   `json:"priority"` // Base is 10, Equipment is 20, Buff is 30
	SourceID  string  `json:"source_id,omitempty"`
}
