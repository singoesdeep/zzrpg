package loot

// EventLootDropped is the bus routing key for LootDropped.
const EventLootDropped = "loot_dropped"

// LootDropped is published when a loot table is rolled and awarded to a
// character (e.g. on a kill). Consumers can drive UI, analytics, or collection
// tracking. Additive: the bus is async and a no-op with no subscribers.
type LootDropped struct {
	CharacterID int64
	TableID     string
	Items       []DroppedItem
}

func (LootDropped) Name() string { return EventLootDropped }
