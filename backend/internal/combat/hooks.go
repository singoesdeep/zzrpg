package combat

// Hook names for combat extension points. Plugins register filters against these
// to participate in the combat flow (see engine/hooks).
const (
	// HookDamage filters the final damage of an attack after it is computed and
	// before it is applied to the defender's HP. A plugin can boost, reduce, or
	// zero it — e.g. difficulty modifiers, shields, "double-damage" events.
	HookDamage = "combat.damage"
)

// DamageFilter is the value threaded through HookDamage filters. Only Damage is
// meant to be modified; the rest is read-only context.
type DamageFilter struct {
	AttackerID int64
	DefenderID int64
	IsCrit     bool
	Damage     int32
}
