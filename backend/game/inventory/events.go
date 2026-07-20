package inventory

import "github.com/singoesdeep/zzrpg/sdk/engine/outbox"

// Event names for inventory changes published on the engine bus.
const (
	EventItemEquipped         = "item_equipped"
	EventItemUnequipped       = "item_unequipped"
	EventItemAddedToInventory = "item_added_to_inventory"
)

// ItemAddedToInventory is published when an item is placed into a bag slot.
// Consumers can drive collect-item quests, achievements, or UI. Additive: the
// bus is async and a no-op with no subscribers.
type ItemAddedToInventory struct {
	CharacterID      int32
	ItemDefinitionID string
	Quantity         int32
	SlotIndex        int32
}

func (ItemAddedToInventory) Name() string { return EventItemAddedToInventory }

// RegisterEventDecoders registers decoders for every event this package emits so
// the cross-node event stream can rebuild them.
func RegisterEventDecoders(r *outbox.Registry) {
	r.Register(EventItemEquipped, outbox.JSONDecoder[ItemEquipped]())
	r.Register(EventItemUnequipped, outbox.JSONDecoder[ItemUnequipped]())
	r.Register(EventItemAddedToInventory, outbox.JSONDecoder[ItemAddedToInventory]())
}

// ItemEquipped is published when an item becomes equipped in an equipment slot.
// It implements engine/bus.Event via Name().
type ItemEquipped struct {
	CharacterID int32
	Item        *InventoryItem
}

func (ItemEquipped) Name() string { return EventItemEquipped }

// ItemUnequipped is published when an item leaves an equipment slot.
type ItemUnequipped struct {
	CharacterID int32
	Item        *InventoryItem
}

func (ItemUnequipped) Name() string { return EventItemUnequipped }
