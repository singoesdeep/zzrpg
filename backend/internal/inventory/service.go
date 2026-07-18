package inventory

import (
	"context"
	"strings"

	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/events"
)

type InventoryService interface {
	MoveItem(ctx context.Context, charID int32, fromSlot, toSlot int32) error
	GetInventory(ctx context.Context, charID int32) ([]InventoryItem, error)
	AddItem(ctx context.Context, item *InventoryItem) error
}

type inventoryService struct {
	repo        InventoryRepository
	charService character.CharacterService
	eventBus    *events.Bus
}

func NewInventoryService(repo InventoryRepository, charService character.CharacterService, eventBus *events.Bus) InventoryService {
	return &inventoryService{
		repo:        repo,
		charService: charService,
		eventBus:    eventBus,
	}
}

func (s *inventoryService) GetInventory(ctx context.Context, charID int32) ([]InventoryItem, error) {
	return s.repo.ListByCharacter(ctx, charID)
}

func (s *inventoryService) AddItem(ctx context.Context, item *InventoryItem) error {
	// Find first empty bag slot (0..99)
	existingItems, err := s.repo.ListByCharacter(ctx, item.CharacterID)
	if err != nil {
		return err
	}

	occupied := make(map[int32]bool)
	for _, it := range existingItems {
		occupied[it.SlotIndex] = true
	}

	emptySlot := int32(-1)
	for i := int32(MinBagSlot); i <= int32(MaxBagSlot); i++ {
		if !occupied[i] {
			emptySlot = i
			break
		}
	}

	if emptySlot == -1 {
		return ErrSlotOutOfBounds // bag is full
	}

	item.SlotIndex = emptySlot
	return s.repo.AddItem(ctx, item)
}

func (s *inventoryService) MoveItem(ctx context.Context, charID int32, fromSlot, toSlot int32) error {
	// 1. Boundary check
	if !isValidSlot(fromSlot) || !isValidSlot(toSlot) {
		return ErrSlotOutOfBounds
	}

	// 2. Fetch active item
	item, err := s.repo.GetBySlot(ctx, charID, fromSlot)
	if err != nil {
		return err
	}

	// 3. Check equipment rules if moving to equipment slots (1000+)
	if isEquipmentSlot(toSlot) {
		if err := s.validateEquipmentRequirements(ctx, charID, item, toSlot); err != nil {
			return err
		}
	}

	// 4. Check destination slot status
	destItem, err := s.repo.GetBySlot(ctx, charID, toSlot)
	if err != nil && err != ErrItemNotFound {
		return err
	}

	isFromEquip := isEquipmentSlot(fromSlot)
	isToEquip := isEquipmentSlot(toSlot)

	if destItem == nil {
		// Empty slot: simple move
		if err := s.repo.Move(ctx, charID, fromSlot, toSlot); err != nil {
			return err
		}
	} else {
		// Occupied slot: swap
		// If moving to equipment, check if the item currently equipped can go back to fromSlot (validation not usually needed for bag, but check class/level constraints if swapping between two equipment slots)
		if isFromEquip && !isToEquip {
			// Swapping equipped item to bag (always allowed)
		} else if !isFromEquip && isToEquip {
			// Swapping bag item to equipment slot which currently holds destItem.
			// destItem will go to fromSlot (bag), which is always allowed.
		} else if isFromEquip && isToEquip {
			// Swapping equipment slots (e.g. ring 1 to ring 2). Validate requirements for destItem on fromSlot.
			if err := s.validateEquipmentRequirements(ctx, charID, destItem, fromSlot); err != nil {
				return err
			}
		}

		if err := s.repo.Swap(ctx, charID, fromSlot, toSlot); err != nil {
			return err
		}
	}

	// 5. Fire Event Bus triggers for stat calculations
	if isFromEquip {
		s.eventBus.Publish(ctx, events.EventItemUnequipped, EquippedItemEventPayload{
			CharacterID: charID,
			Item:        item,
		})
	}
	if isToEquip {
		s.eventBus.Publish(ctx, events.EventItemEquipped, EquippedItemEventPayload{
			CharacterID: charID,
			Item:        item,
		})
	}
	if destItem != nil {
		if isToEquip { // destItem was unequipped to fromSlot
			s.eventBus.Publish(ctx, events.EventItemUnequipped, EquippedItemEventPayload{
				CharacterID: charID,
				Item:        destItem,
			})
		}
		if isFromEquip { // destItem was equipped from toSlot
			s.eventBus.Publish(ctx, events.EventItemEquipped, EquippedItemEventPayload{
				CharacterID: charID,
				Item:        destItem,
			})
		}
	}

	return nil
}

func (s *inventoryService) validateEquipmentRequirements(ctx context.Context, charID int32, item *InventoryItem, targetSlot int32) error {
	// Verify slot matching
	details := item.ItemDetails
	if details == nil {
		return ErrInvalidEquipmentSlot
	}

	switch targetSlot {
	case WeaponSlot:
		if details.SlotType != "WEAPON" {
			return ErrInvalidEquipmentSlot
		}
	case BodyArmorSlot:
		if details.SlotType != "BODY_ARMOR" {
			return ErrInvalidEquipmentSlot
		}
	case HelmetSlot:
		if details.SlotType != "HELMET" {
			return ErrInvalidEquipmentSlot
		}
	case ShieldSlot:
		if details.SlotType != "SHIELD" {
			return ErrInvalidEquipmentSlot
		}
	case ShoesSlot:
		if details.SlotType != "SHOES" {
			return ErrInvalidEquipmentSlot
		}
	case AccessorySlot:
		if details.SlotType != "ACCESSORY" {
			return ErrInvalidEquipmentSlot
		}
	default:
		return ErrInvalidEquipmentSlot
	}

	// Fetch character details
	char, err := s.charService.GetByID(ctx, int64(charID))
	if err != nil {
		return err
	}

	// Check Level restriction
	if char.Level < details.MinLevel {
		return ErrLevelRestricted
	}

	// Check Class restriction
	if len(details.ClassRestrictions) > 0 {
		allowed := false
		for _, class := range details.ClassRestrictions {
			if strings.EqualFold(class, char.ClassName) {
				allowed = true
				break
			}
		}
		if !allowed {
			return ErrClassRestricted
		}
	}

	return nil
}

func isValidSlot(slot int32) bool {
	return (slot >= MinBagSlot && slot <= MaxBagSlot) || isEquipmentSlot(slot)
}

func isEquipmentSlot(slot int32) bool {
	return slot >= WeaponSlot && slot <= AccessorySlot
}
