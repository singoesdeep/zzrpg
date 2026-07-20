// Package creature is the unified combat-entity model. Anything that fights —
// player characters, mobs, and (later) pets or NPCs — resolves to a Creature, so
// combat treats them uniformly instead of branching per type. New kinds are added
// by teaching a Resolver to produce Creatures for their IDs; combat does not
// change.
package creature

import "context"

// Kind distinguishes what a creature is. It drives policy that legitimately
// differs by type (e.g. mob sessions are auto-created on first hit; characters
// must be "logged in").
type Kind string

const (
	KindCharacter Kind = "character"
	KindMob       Kind = "mob"
	KindPet       Kind = "pet"
)

// Creature is a combat entity's server-authoritative view: the stats combat needs
// plus reward metadata for kills. Fields a given kind doesn't use are zero (mobs
// have no Class; characters carry the PvP loot/quest defaults).
type Creature struct {
	ID    int64
	Kind  Kind
	Class string // for skill gating (characters); "" otherwise
	Level int32

	Attack   float64
	Defense  float64
	Dex      float64
	CritRate float64
	CritDmg  float64
	MaxHP    float64
	MaxMP    float64

	// Reward metadata used when this creature is killed.
	LootTableID string
	QuestTag    string
}

// Resolver produces the Creature for an ID. ok is false when no creature exists
// for the ID (across every source the resolver knows). It is how combat resolves
// both the attacker and the defender without knowing whether either is a
// character, a mob, or a pet.
type Resolver interface {
	Resolve(ctx context.Context, id int64) (Creature, bool, error)
}

// ResolverFunc adapts a plain function to a Resolver.
type ResolverFunc func(ctx context.Context, id int64) (Creature, bool, error)

// Resolve implements Resolver.
func (f ResolverFunc) Resolve(ctx context.Context, id int64) (Creature, bool, error) {
	return f(ctx, id)
}
