package inventory

import (
	"context"
	"errors"
	"time"

	"github.com/singoesdeep/zzrpg/backend/internal/items"
)

var (
	ErrSlotOccupied       = errors.New("destination slot is already occupied")
	ErrSlotOutOfBounds    = errors.New("slot index out of bounds")
	ErrItemNotFound       = errors.New("item not found in inventory")
	ErrLevelRestricted    = errors.New("character level is too low to equip this item")
	ErrClassRestricted    = errors.New("character class is restricted from equipping this item")
	ErrInvalidEquipmentSlot = errors.New("item slot type does not match equipment slot")
	ErrNotOwner             = errors.New("character does not belong to the requesting user")
)

const (
	MinBagSlot    = 0
	MaxBagSlot    = 99
	WeaponSlot    = 1000
	BodyArmorSlot = 1001
	HelmetSlot    = 1002
	ShieldSlot    = 1003
	ShoesSlot     = 1004
	AccessorySlot = 1005
)

type InventoryItem struct {
	ID               int64                `json:"id"`
	CharacterID      int32                `json:"character_id"`
	SlotIndex        int32                `json:"slot_index"`
	ItemDefinitionID string               `json:"item_definition_id"`
	Quantity         int32                `json:"quantity"`
	Durability       int32                `json:"durability"`
	CustomModifiers  []items.StatModifier `json:"custom_modifiers"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`

	// Joined item details (optional)
	ItemDetails *items.ItemDefinition `json:"item_details,omitempty"`
}

type MoveRequest struct {
	CharacterID int32 `json:"character_id"`
	FromSlot    int32 `json:"from_slot"`
	ToSlot      int32 `json:"to_slot"`
}

type EquippedItemEventPayload struct {
	CharacterID int32
	Item        *InventoryItem
}

type InventoryRepository interface {
	GetBySlot(ctx context.Context, charID int32, slot int32) (*InventoryItem, error)
	ListByCharacter(ctx context.Context, charID int32) ([]InventoryItem, error)
	Move(ctx context.Context, charID int32, fromSlot, toSlot int32) error
	Swap(ctx context.Context, charID int32, slotA, slotB int32) error
	AddItem(ctx context.Context, item *InventoryItem) error
	RemoveItem(ctx context.Context, id int64) error
}
