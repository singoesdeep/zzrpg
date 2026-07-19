package character

// Domain event names published on the engine bus for character progression.
// Additive: emitting them does not change the service's synchronous behaviour
// (the bus is async, fire-and-forget, and a no-op with no subscribers).
const (
	EventRewardsGranted     = "rewards_granted"
	EventCharacterLeveledUp = "character_leveled_up"
	EventStatsRecalculated  = "stats_recalculated"
)

// RewardsGranted is published whenever gold/exp is credited to a character
// (kill loot, quest rewards, offline gains all funnel through AddRewards).
type RewardsGranted struct {
	CharacterID int64
	Gold        int64
	Exp         int64
}

func (RewardsGranted) Name() string { return EventRewardsGranted }

// CharacterLeveledUp is published when accrued exp pushes a character to a new
// level. Consumers can unlock talents/skills, broadcast, or update UI.
type CharacterLeveledUp struct {
	CharacterID int64
	NewLevel    int32
}

func (CharacterLeveledUp) Name() string { return EventCharacterLeveledUp }

// StatsRecalculated is published after a character's derived stats are recomputed
// (level-up or equipment change), carrying the fresh derived-stat map.
type StatsRecalculated struct {
	CharacterID  int64
	DerivedStats map[string]float64
}

func (StatsRecalculated) Name() string { return EventStatsRecalculated }
