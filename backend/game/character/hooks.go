package character

// HookRewards filters gold/exp before they are applied and recorded. A plugin can
// scale or add to them — e.g. an XP-boost weekend, a rested bonus, or a premium
// multiplier.
const HookRewards = "character.rewards"

// RewardsFilter is the value threaded through HookRewards filters. CharacterID is
// read-only context; Gold and Exp are the amounts a filter may modify.
type RewardsFilter struct {
	CharacterID int64
	Gold        int64
	Exp         int64
}

// HookStatsRecalc filters a character's derived stats after they are recomputed
// (on level-up or equipment change) and before they are cached. A plugin can add
// auras, global buffs, or prestige bonuses.
const HookStatsRecalc = "character.stats_recalc"

// StatsRecalcFilter is the value threaded through HookStatsRecalc filters.
// CharacterID is read-only context; DerivedStats is the map a filter may modify.
type StatsRecalcFilter struct {
	CharacterID  int64
	DerivedStats map[string]float64
}
