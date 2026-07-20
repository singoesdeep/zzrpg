package quests

import "github.com/singoesdeep/zzrpg/sdk/engine/outbox"

// Domain event names published on the engine bus for quest lifecycle changes.
// Additive: emitting them does not change the service's synchronous behaviour
// (the bus is async, fire-and-forget, and a no-op with no subscribers).
const (
	EventQuestAccepted   = "quest_accepted"
	EventQuestProgressed = "quest_progressed"
	EventQuestCompleted  = "quest_completed"
)

// QuestAccepted is published when a character accepts a quest.
type QuestAccepted struct {
	CharacterID int32
	QuestID     string
}

func (QuestAccepted) Name() string { return EventQuestAccepted }

// QuestProgressed is published when a quest step advances (but the quest is not
// yet fully complete). Step is the index of the step that advanced.
type QuestProgressed struct {
	CharacterID int32
	QuestID     string
	Step        int32
}

func (QuestProgressed) Name() string { return EventQuestProgressed }

// QuestCompleted is published when the final step of a quest completes and its
// rewards are granted. Consumers can unlock follow-up quests or achievements.
type QuestCompleted struct {
	CharacterID int32
	QuestID     string
}

func (QuestCompleted) Name() string { return EventQuestCompleted }

// RegisterEventDecoders registers decoders for every event this package emits so
// the cross-node event stream can rebuild them.
func RegisterEventDecoders(r *outbox.Registry) {
	r.Register(EventQuestAccepted, outbox.JSONDecoder[QuestAccepted]())
	r.Register(EventQuestProgressed, outbox.JSONDecoder[QuestProgressed]())
	r.Register(EventQuestCompleted, outbox.JSONDecoder[QuestCompleted]())
}
