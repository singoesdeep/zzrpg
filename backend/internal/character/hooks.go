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
