package combat

// Hook names for combat extension points. Plugins register filters against these
// to participate in the combat flow (see engine/hooks).
const (
	// HookPreAttack runs (as an action) before an attack is resolved. A plugin
	// may abort the attack by returning an error — e.g. a peaceful-zone,
	// disarm, or stun plugin. The error surfaces to the caller.
	HookPreAttack = "combat.pre_attack"

	// HookDamage filters the final damage of an attack after it is computed and
	// before it is applied to the defender's HP. A plugin can boost, reduce, or
	// zero it — e.g. difficulty modifiers, shields, "double-damage" events.
	HookDamage = "combat.damage"
)

// PreAttack is the read-only context passed to HookPreAttack actions.
type PreAttack struct {
	AttackerID int64
	DefenderID int64
	SkillID    string
}

// DamageFilter is the value threaded through HookDamage filters. Only Damage is
// meant to be modified; the rest is read-only context.
type DamageFilter struct {
	AttackerID int64
	DefenderID int64
	IsCrit     bool
	Damage     int32
}
