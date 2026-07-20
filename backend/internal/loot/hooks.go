package loot

// HookRoll filters the items rolled from a loot table before they are returned.
// A plugin can add bonus drops, remove items, or scale quantities — e.g. a
// luck buff, a double-drop event, or an event-only item. Enabled via WithHooks.
const HookRoll = "loot.roll"

// LootRoll is the value threaded through HookRoll filters. TableID is read-only
// context; Items is the rolled result a filter may modify.
type LootRoll struct {
	TableID string
	Items   []DroppedItem
}
