package inventory

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/game/character"
	"github.com/singoesdeep/zzrpg/backend/game/items"
)

type mockInventoryRepository struct {
	items map[int64]*InventoryItem
}

func newMockInventoryRepository() *mockInventoryRepository {
	return &mockInventoryRepository{items: make(map[int64]*InventoryItem)}
}

func (m *mockInventoryRepository) GetBySlot(ctx context.Context, charID int32, slot int32) (*InventoryItem, error) {
	for _, it := range m.items {
		if it.CharacterID == charID && it.SlotIndex == slot {
			return it, nil
		}
	}
	return nil, ErrItemNotFound
}

func (m *mockInventoryRepository) ListByCharacter(ctx context.Context, charID int32) ([]InventoryItem, error) {
	var list []InventoryItem
	for _, it := range m.items {
		if it.CharacterID == charID {
			list = append(list, *it)
		}
	}
	return list, nil
}

func (m *mockInventoryRepository) Move(ctx context.Context, charID int32, fromSlot, toSlot int32) error {
	it, err := m.GetBySlot(ctx, charID, fromSlot)
	if err != nil {
		return err
	}
	it.SlotIndex = toSlot
	return nil
}

func (m *mockInventoryRepository) Swap(ctx context.Context, charID int32, slotA, slotB int32) error {
	itA, err := m.GetBySlot(ctx, charID, slotA)
	if err != nil {
		return err
	}
	itB, err := m.GetBySlot(ctx, charID, slotB)
	if err != nil {
		return err
	}
	itA.SlotIndex = slotB
	itB.SlotIndex = slotA
	return nil
}

func (m *mockInventoryRepository) AddItem(ctx context.Context, item *InventoryItem) error {
	item.ID = int64(len(m.items) + 1)
	m.items[item.ID] = item
	return nil
}

func (m *mockInventoryRepository) RemoveItem(ctx context.Context, id int64) error {
	if _, ok := m.items[id]; !ok {
		return ErrItemNotFound
	}
	delete(m.items, id)
	return nil
}

type mockCharacterService struct {
	char *character.CharacterWithStats
}

func (m *mockCharacterService) Create(ctx context.Context, userID int64, name, className string) (*character.CharacterWithStats, error) {
	return nil, nil
}
func (m *mockCharacterService) GetByID(ctx context.Context, id int64) (*character.CharacterWithStats, error) {
	if m.char == nil {
		return nil, character.ErrCharacterNotFound
	}
	return m.char, nil
}
func (m *mockCharacterService) ListByUserID(ctx context.Context, userID int64) ([]character.Character, error) {
	return nil, nil
}
func (m *mockCharacterService) RecalculateStats(ctx context.Context, id int64) error {
	return nil
}
func (m *mockCharacterService) SetEquipmentProvider(p character.EquipmentProvider) {}
func (m *mockCharacterService) AddRewards(ctx context.Context, charID int64, gold int64, exp int64) (bool, int32, error) {
	return false, 0, nil
}
func (m *mockCharacterService) UpdateLastActive(ctx context.Context, charID int64) error {
	return nil
}

func TestMoveAndEquipItem(t *testing.T) {
	repo := newMockInventoryRepository()
	charService := &mockCharacterService{
		char: &character.CharacterWithStats{
			Character: character.Character{
				ID:        1,
				Name:      "SavasciHero",
				ClassName: "WARRIOR",
				Level:     15,
			},
		},
	}
	eventBus := bus.NewInProc(nil)

	service := NewInventoryService(repo, charService, eventBus)

	// Create test sword definition
	swordDef := &items.ItemDefinition{
		ID:                "sword_lvl_10",
		Name:              "Iron Sword +9",
		SlotType:          "WEAPON",
		MinLevel:          10,
		ClassRestrictions: []string{"WARRIOR", "SURA"},
	}

	// Add test sword into character inventory bag slot 5
	swordItem := &InventoryItem{
		CharacterID:      1,
		SlotIndex:        5,
		ItemDefinitionID: "sword_lvl_10",
		Quantity:         1,
		ItemDetails:      swordDef,
	}
	_ = repo.AddItem(context.Background(), swordItem)

	// Subscribe to event bus to verify notifications
	var wg sync.WaitGroup
	var receivedEvent string
	wg.Add(1)
	eventBus.Subscribe(EventItemEquipped, func(ctx context.Context, event bus.Event) {
		receivedEvent = event.Name()
		wg.Done()
	})

	// 1. Success Equipping Sword
	err := service.MoveItem(context.Background(), 1, 5, WeaponSlot)
	if err != nil {
		t.Fatalf("expected successful move/equip, got %v", err)
	}

	// Wait for event to be received in goroutine
	wg.Wait()
	if receivedEvent != EventItemEquipped {
		t.Errorf("expected EventItemEquipped event notification, got %s", receivedEvent)
	}

	// Verify sword is now in WeaponSlot (1000)
	it, _ := repo.GetBySlot(context.Background(), 1, WeaponSlot)
	if it == nil || it.ItemDefinitionID != "sword_lvl_10" {
		t.Errorf("sword not found in WeaponSlot")
	}

	// 2. Failure: Equipping in wrong slot (e.g. Shield slot)
	// Add another sword in slot 10
	swordItem2 := &InventoryItem{
		CharacterID:      1,
		SlotIndex:        10,
		ItemDefinitionID: "sword_lvl_10",
		Quantity:         1,
		ItemDetails:      swordDef,
	}
	_ = repo.AddItem(context.Background(), swordItem2)

	err = service.MoveItem(context.Background(), 1, 10, ShieldSlot)
	if err != ErrInvalidEquipmentSlot {
		t.Errorf("expected ErrInvalidEquipmentSlot, got %v", err)
	}

	// 3. Failure: Level Restriction (e.g. item requires level 20, character is level 15)
	highLvlSwordDef := &items.ItemDefinition{
		ID:       "sword_lvl_20",
		Name:     "Dragon Sword +0",
		SlotType: "WEAPON",
		MinLevel: 20,
	}
	swordItem3 := &InventoryItem{
		CharacterID:      1,
		SlotIndex:        15,
		ItemDefinitionID: "sword_lvl_20",
		Quantity:         1,
		ItemDetails:      highLvlSwordDef,
	}
	_ = repo.AddItem(context.Background(), swordItem3)

	err = service.MoveItem(context.Background(), 1, 15, WeaponSlot)
	// Target slot WeaponSlot is already occupied, which means it will trigger Swap validation.
	// But first, it validates equipment constraints for slot 15 -> WeaponSlot.
	// WeaponSlot requires swordItem3. It requires lvl 20. Char is lvl 15. Expect level restricted error.
	if err != ErrLevelRestricted {
		t.Errorf("expected ErrLevelRestricted, got %v", err)
	}

	// 4. Failure: Class Restriction (e.g. item is restricted to Mage only)
	mageStaffDef := &items.ItemDefinition{
		ID:                "staff_lvl_1",
		Name:              "Apprentice Staff",
		SlotType:          "WEAPON",
		MinLevel:          1,
		ClassRestrictions: []string{"MAGE"},
	}
	staffItem := &InventoryItem{
		CharacterID:      1,
		SlotIndex:        20,
		ItemDefinitionID: "staff_lvl_1",
		Quantity:         1,
		ItemDetails:      mageStaffDef,
	}
	_ = repo.AddItem(context.Background(), staffItem)

	// Temporarily empty weapon slot to avoid Swap triggering
	_ = repo.RemoveItem(context.Background(), 1) // removes first sword item at WeaponSlot (ID 1)

	err = service.MoveItem(context.Background(), 1, 20, WeaponSlot)
	if err != ErrClassRestricted {
		t.Errorf("expected ErrClassRestricted, got %v", err)
	}
}

// Helper to make time mocking easy
func init() {
	_ = time.Second
}

// TestAddItemEmitsEvent proves the inventory seam: a bus subscriber receives
// ItemAddedToInventory (with the assigned slot) when an item is added — enabling
// collect-item quests/achievements without inventory depending on them.
func TestAddItemEmitsEvent(t *testing.T) {
	repo := newMockInventoryRepository()
	eventBus := bus.NewInProc(nil)
	added := make(chan ItemAddedToInventory, 1)
	eventBus.Subscribe(EventItemAddedToInventory, func(_ context.Context, ev bus.Event) {
		added <- ev.(ItemAddedToInventory)
	})
	service := NewInventoryService(repo, &mockCharacterService{}, eventBus)

	err := service.AddItem(context.Background(), &InventoryItem{
		CharacterID:      7,
		ItemDefinitionID: "red_potion_1",
		Quantity:         3,
		Durability:       100,
	})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	select {
	case ev := <-added:
		if ev.CharacterID != 7 || ev.ItemDefinitionID != "red_potion_1" || ev.Quantity != 3 || ev.SlotIndex != 0 {
			t.Errorf("unexpected ItemAddedToInventory: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ItemAddedToInventory")
	}
}
