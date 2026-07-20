package quests

const (
	// HookAccept runs (as an action) when a character tries to accept a quest,
	// after the level check. A plugin may block it by returning an error —
	// prerequisites, faction locks, timed event windows, ...
	HookAccept = "quest.accept"

	// HookProgress filters the progress amount before it is applied — e.g. a
	// "double quest progress" event.
	HookProgress = "quest.progress"
)

// QuestAccept is the read-only context passed to HookAccept actions.
type QuestAccept struct {
	CharacterID int32
	QuestID     string
}

// QuestProgressFilter is threaded through HookProgress filters. All fields but
// Amount are read-only context; Amount is what a filter may modify.
type QuestProgressFilter struct {
	CharacterID int32
	ActionType  string
	Target      string
	Amount      int32
}
