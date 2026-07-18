package items

import (
	"context"
	"testing"
)

type mockItemRepository struct {
	items map[string]*ItemDefinition
}

func newMockItemRepository() *mockItemRepository {
	return &mockItemRepository{items: make(map[string]*ItemDefinition)}
}

func (m *mockItemRepository) Create(ctx context.Context, item *ItemDefinition) error {
	if _, ok := m.items[item.ID]; ok {
		return ErrItemAlreadyExists
	}
	m.items[item.ID] = item
	return nil
}

func (m *mockItemRepository) Update(ctx context.Context, item *ItemDefinition) error {
	if _, ok := m.items[item.ID]; !ok {
		return ErrItemNotFound
	}
	m.items[item.ID] = item
	return nil
}

func (m *mockItemRepository) GetByID(ctx context.Context, id string) (*ItemDefinition, error) {
	item, ok := m.items[id]
	if !ok {
		return nil, ErrItemNotFound
	}
	return item, nil
}

func (m *mockItemRepository) List(ctx context.Context) ([]ItemDefinition, error) {
	var list []ItemDefinition
	for _, it := range m.items {
		list = append(list, *it)
	}
	return list, nil
}

func (m *mockItemRepository) Delete(ctx context.Context, id string) error {
	if _, ok := m.items[id]; !ok {
		return ErrItemNotFound
	}
	delete(m.items, id)
	return nil
}

func TestCreateItem(t *testing.T) {
	repo := newMockItemRepository()
	service := NewItemService(repo)

	// 1. Success case: Dragon Sword
	item := &ItemDefinition{
		ID:       "dragon_sword_0",
		Name:     "Dragon Sword +0",
		SlotType: "WEAPON",
		MinLevel: 10,
		StatsModifiers: []StatModifier{
			{Stat: "ATTACK", Operation: "ADD", Value: 120.0, Priority: 20},
			{Stat: "CRIT_RATE", Operation: "ADD", Value: 5.0, Priority: 20},
		},
		Metadata: map[string]any{"weight": 5},
	}

	err := service.Create(context.Background(), item)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// 2. Duplicate ID
	err = service.Create(context.Background(), item)
	if err != ErrItemAlreadyExists {
		t.Errorf("expected ErrItemAlreadyExists, got %v", err)
	}

	// 3. Invalid Slot
	badSlotItem := &ItemDefinition{
		ID:       "bad_item",
		Name:     "Bad Item",
		SlotType: "INVALID_SLOT",
	}
	err = service.Create(context.Background(), badSlotItem)
	if err != ErrInvalidSlotType {
		t.Errorf("expected ErrInvalidSlotType, got %v", err)
	}

	// 4. Invalid Modifier Stat
	badModifierItem := &ItemDefinition{
		ID:       "bad_mod_item",
		Name:     "Bad Mod Item",
		SlotType: "WEAPON",
		StatsModifiers: []StatModifier{
			{Stat: "INVALID_STAT", Operation: "ADD", Value: 10},
		},
	}
	err = service.Create(context.Background(), badModifierItem)
	if err != ErrInvalidModifierType {
		t.Errorf("expected ErrInvalidModifierType for bad stat, got %v", err)
	}

	// 5. Invalid Modifier Operation
	badOpItem := &ItemDefinition{
		ID:       "bad_op_item",
		Name:     "Bad Op Item",
		SlotType: "WEAPON",
		StatsModifiers: []StatModifier{
			{Stat: "ATTACK", Operation: "SUBTRACT", Value: 10},
		},
	}
	err = service.Create(context.Background(), badOpItem)
	if err != ErrInvalidModifierType {
		t.Errorf("expected ErrInvalidModifierType for bad operation, got %v", err)
	}
}

func TestUpdateAndDelete(t *testing.T) {
	repo := newMockItemRepository()
	service := NewItemService(repo)

	item := &ItemDefinition{
		ID:       "shield_0",
		Name:     "Battle Shield +0",
		SlotType: "SHIELD",
		MinLevel: 1,
	}

	// Create
	if err := service.Create(context.Background(), item); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Update
	item.Name = "Battle Shield +1"
	item.StatsModifiers = []StatModifier{
		{Stat: "DEFENSE", Operation: "ADD", Value: 20.0, Priority: 20},
	}
	if err := service.Update(context.Background(), item); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	updatedItem, err := service.GetByID(context.Background(), "shield_0")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}

	if updatedItem.Name != "Battle Shield +1" || len(updatedItem.StatsModifiers) != 1 {
		t.Errorf("expected updated values, got %+v", updatedItem)
	}

	// Delete
	if err := service.Delete(context.Background(), "shield_0"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err = service.GetByID(context.Background(), "shield_0")
	if err != ErrItemNotFound {
		t.Errorf("expected ErrItemNotFound after delete, got %v", err)
	}
}
