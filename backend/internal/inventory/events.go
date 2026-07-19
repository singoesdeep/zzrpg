package inventory

// Event names for equipment changes published on the engine bus.
const (
	EventItemEquipped   = "item_equipped"
	EventItemUnequipped = "item_unequipped"
)

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
