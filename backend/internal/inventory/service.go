package inventory

import (
	"context"
	"strings"
	"sync"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
)

// keyedMutex serializes operations per key (here, per character). Inventory
// mutations (AddItem, MoveItem) do read-then-write across multiple statements
// and would otherwise race, e.g. two AddItem calls picking the same empty slot.
// This is a single-node guard; a horizontally scaled deployment must replace it
// with a database-level lock (SELECT ... FOR UPDATE or pg_advisory_xact_lock).
//
// Entries are reference-counted and evicted once no goroutine holds or is
// waiting for a key, so the map does not grow unbounded with the number of
// characters ever seen.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[int32]*refMutex
}

// refMutex is a per-key mutex plus a count of goroutines currently holding or
// waiting for it. refs is only touched under keyedMutex.mu.
type refMutex struct {
	mu   sync.Mutex
	refs int
}

func newKeyedMutex() *keyedMutex {
	return &keyedMutex{locks: make(map[int32]*refMutex)}
}

// lock acquires the mutex for key and returns its unlock function. Registering
// interest (refs++) happens under k.mu before the per-key lock is taken, so a
// waiter can never have its entry evicted by a departing holder; the entry is
// removed only when the last reference is released.
func (k *keyedMutex) lock(key int32) func() {
	k.mu.Lock()
	m, ok := k.locks[key]
	if !ok {
		m = &refMutex{}
		k.locks[key] = m
	}
	m.refs++
	k.mu.Unlock()

	m.mu.Lock()
	return func() {
		m.mu.Unlock()
		k.mu.Lock()
		m.refs--
		if m.refs == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}

type InventoryService interface {
	MoveItem(ctx context.Context, charID int32, fromSlot, toSlot int32) error
	GetInventory(ctx context.Context, charID int32) ([]InventoryItem, error)
	AddItem(ctx context.Context, item *InventoryItem) error
	GetEquippedModifiers(ctx context.Context, charID int32) ([]character.EquipmentModifier, error)
	// VerifyOwnership reports whether charID belongs to userID. Used at the HTTP
	// boundary to prevent IDOR; internal callers (combat, quests, offline gains)
	// operate with system authority and skip this check.
	VerifyOwnership(ctx context.Context, userID int64, charID int32) error
}

type inventoryService struct {
	repo        InventoryRepository
	charService character.CharacterService
	eventBus    bus.EventBus
	charLocks   *keyedMutex
}

func NewInventoryService(repo InventoryRepository, charService character.CharacterService, eventBus bus.EventBus) InventoryService {
	return &inventoryService{
		repo:        repo,
		charService: charService,
		eventBus:    eventBus,
		charLocks:   newKeyedMutex(),
	}
}

func (s *inventoryService) VerifyOwnership(ctx context.Context, userID int64, charID int32) error {
	char, err := s.charService.GetByID(ctx, int64(charID))
	if err != nil {
		// Includes character.ErrCharacterNotFound; callers map this to 404.
		return err
	}
	if char.UserID != userID {
		return ErrNotOwner
	}
	return nil
}

func (s *inventoryService) GetInventory(ctx context.Context, charID int32) ([]InventoryItem, error) {
	return s.repo.ListByCharacter(ctx, charID)
}

func (s *inventoryService) AddItem(ctx context.Context, item *InventoryItem) error {
	// Serialize inventory mutations for this character so the find-empty-slot /
	// insert sequence is atomic against concurrent adds and moves.
	defer s.charLocks.lock(item.CharacterID)()

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
	if err := s.repo.AddItem(ctx, item); err != nil {
		return err
	}

	if s.eventBus != nil {
		_ = s.eventBus.Publish(ctx, ItemAddedToInventory{
			CharacterID:      item.CharacterID,
			ItemDefinitionID: item.ItemDefinitionID,
			Quantity:         item.Quantity,
			SlotIndex:        item.SlotIndex,
		})
	}
	return nil
}

func (s *inventoryService) MoveItem(ctx context.Context, charID int32, fromSlot, toSlot int32) error {
	// Serialize inventory mutations for this character so the read (GetBySlot) →
	// validate → write (Move/Swap) sequence is atomic against concurrent
	// mutations, eliminating the TOCTOU that could hit the slot unique constraint.
	defer s.charLocks.lock(charID)()

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
		_ = s.eventBus.Publish(ctx, ItemUnequipped{CharacterID: charID, Item: item})
	}
	if isToEquip {
		_ = s.eventBus.Publish(ctx, ItemEquipped{CharacterID: charID, Item: item})
	}
	if destItem != nil {
		if isToEquip { // destItem was unequipped to fromSlot
			_ = s.eventBus.Publish(ctx, ItemUnequipped{CharacterID: charID, Item: destItem})
		}
		if isFromEquip { // destItem was equipped from toSlot
			_ = s.eventBus.Publish(ctx, ItemEquipped{CharacterID: charID, Item: destItem})
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

func (s *inventoryService) GetEquippedModifiers(ctx context.Context, charID int32) ([]character.EquipmentModifier, error) {
	itemsList, err := s.repo.ListByCharacter(ctx, charID)
	if err != nil {
		return nil, err
	}

	var mods []character.EquipmentModifier
	for _, it := range itemsList {
		if isEquipmentSlot(it.SlotIndex) && it.ItemDetails != nil {
			// Add base modifiers of the item
			for _, m := range it.ItemDetails.StatsModifiers {
				mods = append(mods, character.EquipmentModifier{
					Stat:      m.Stat,
					Operation: m.Operation,
					Value:     m.Value,
					Priority:  m.Priority,
					SourceID:  it.ItemDefinitionID,
				})
			}
			// Add custom modifiers (random bonuses)
			for _, m := range it.CustomModifiers {
				mods = append(mods, character.EquipmentModifier{
					Stat:      m.Stat,
					Operation: m.Operation,
					Value:     m.Value,
					Priority:  m.Priority,
					SourceID:  it.ItemDefinitionID + "_custom",
				})
			}
		}
	}
	return mods, nil
}
