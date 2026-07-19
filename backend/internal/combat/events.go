package combat

import "github.com/singoesdeep/zzrpg/backend/engine/outbox"

// Domain event names published on the engine bus when combat resolves. These
// events are additive: they let consumers (analytics, achievements, aggro/AI,
// death penalties, client fan-out) react to combat without combat depending on
// them. Emitting them does not change combat's own synchronous behaviour — the
// bus is async, fire-and-forget, and a no-op when nothing is subscribed.
const (
	EventCombatAttackResolved = "combat_attack_resolved"
	EventMobKilled            = "mob_killed"
	EventPlayerKilled         = "player_killed"
	EventCharacterDamaged     = "character_damaged"
)

// CharacterDamaged is published when a defender's HP is reduced by an attack,
// carrying the post-hit health. Consumers can drive aggro/threat, on-damage
// passives, or health UI. (Combat is currently the only damage source; other
// sources would emit this too.)
type CharacterDamaged struct {
	CharacterID int64
	Amount      int32
	NewHP       float64
	IsDead      bool
}

func (CharacterDamaged) Name() string { return EventCharacterDamaged }

// RegisterEventDecoders registers decoders for every event this package emits so
// the cross-node event stream can rebuild them.
func RegisterEventDecoders(r *outbox.Registry) {
	r.Register(EventCombatAttackResolved, outbox.JSONDecoder[CombatAttackResolved]())
	r.Register(EventMobKilled, outbox.JSONDecoder[MobKilled]())
	r.Register(EventPlayerKilled, outbox.JSONDecoder[PlayerKilled]())
	r.Register(EventCharacterDamaged, outbox.JSONDecoder[CharacterDamaged]())
}

// CombatAttackResolved is published for every resolved attack (hit or miss),
// carrying the same outcome the caller receives in AttackResult.
type CombatAttackResolved struct {
	AttackerID     int64
	DefenderID     int64
	IsHit          bool
	Damage         int32
	IsCrit         bool
	DefenderHP     float64
	DefenderMaxHP  float64
	DefenderIsDead bool
}

func (CombatAttackResolved) Name() string { return EventCombatAttackResolved }

// MobKilled is published when an attack lands the killing blow on a defined mob
// (as opposed to a player). LootTableID is the mob's drop table, so subscribers
// can reason about the kill without re-deriving it. Emitted alongside — not
// instead of — the synchronous KillRewarder, so loot/quest grants are unchanged.
type MobKilled struct {
	KillerID    int64
	VictimID    int64
	LootTableID string
}

func (MobKilled) Name() string { return EventMobKilled }

// PlayerKilled is published when an attack lands the killing blow on another
// player (a PvP kill).
type PlayerKilled struct {
	KillerID int64
	VictimID int64
}

func (PlayerKilled) Name() string { return EventPlayerKilled }
