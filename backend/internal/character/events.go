package character

import (
	"time"

	"github.com/singoesdeep/zzrpg/backend/engine/outbox"
	"github.com/singoesdeep/zzrpg/backend/internal/loot"
)

// Domain event names published on the engine bus for character progression and
// session lifecycle. Additive: emitting them does not change any service's
// synchronous behaviour (the bus is async, fire-and-forget, and a no-op with no
// subscribers).
const (
	EventRewardsGranted      = "rewards_granted"
	EventCharacterLeveledUp  = "character_leveled_up"
	EventStatsRecalculated   = "stats_recalculated"
	EventCharacterLoggedIn   = "character_logged_in"
	EventCharacterLoggedOut  = "character_logged_out"
	EventOfflineGainsGranted = "offline_gains_granted"
)

// CharacterLoggedIn is published when a character is selected and its session
// starts. Consumers can drive presence, matchmaking, or idle catch-up.
type CharacterLoggedIn struct {
	CharacterID  int64
	LastActiveAt time.Time
}

func (CharacterLoggedIn) Name() string { return EventCharacterLoggedIn }

// CharacterLoggedOut is published when a character's connection ends (the hub
// deregisters the client). Consumers can update presence or persist state.
type CharacterLoggedOut struct {
	CharacterID int64
}

func (CharacterLoggedOut) Name() string { return EventCharacterLoggedOut }

// OfflineGainsGranted is published after idle rewards are credited on login,
// carrying the same summary the client receives.
type OfflineGainsGranted struct {
	CharacterID    int64
	ElapsedSeconds float64
	Gold           int64
	Exp            int64
	LeveledUp      bool
	NewLevel       int32
	Loot           []loot.DroppedItem
}

func (OfflineGainsGranted) Name() string { return EventOfflineGainsGranted }

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

// RegisterOutboxDecoders teaches the relay how to rebuild this package's
// transactionally-emitted events (RewardsGranted, CharacterLeveledUp) from their
// stored outbox payloads so it can republish them on the bus.
func RegisterOutboxDecoders(r *outbox.Relay) {
	r.Register(EventRewardsGranted, outbox.JSONDecoder[RewardsGranted]())
	r.Register(EventCharacterLeveledUp, outbox.JSONDecoder[CharacterLeveledUp]())
}
