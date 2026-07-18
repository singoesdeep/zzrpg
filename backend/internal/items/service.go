package items

import (
	"context"
	"strings"
)

type ItemService interface {
	Create(ctx context.Context, item *ItemDefinition) error
	Update(ctx context.Context, item *ItemDefinition) error
	GetByID(ctx context.Context, id string) (*ItemDefinition, error)
	List(ctx context.Context) ([]ItemDefinition, error)
	Delete(ctx context.Context, id string) error
}

type itemService struct {
	repo ItemRepository
}

func NewItemService(repo ItemRepository) ItemService {
	return &itemService{repo: repo}
}

func (s *itemService) Create(ctx context.Context, item *ItemDefinition) error {
	if err := s.validate(item); err != nil {
		return err
	}
	return s.repo.Create(ctx, item)
}

func (s *itemService) Update(ctx context.Context, item *ItemDefinition) error {
	if err := s.validate(item); err != nil {
		return err
	}
	return s.repo.Update(ctx, item)
}

func (s *itemService) GetByID(ctx context.Context, id string) (*ItemDefinition, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrItemNotFound
	}
	return s.repo.GetByID(ctx, id)
}

func (s *itemService) List(ctx context.Context) ([]ItemDefinition, error) {
	return s.repo.List(ctx)
}

func (s *itemService) Delete(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrItemNotFound
	}
	return s.repo.Delete(ctx, id)
}

func (s *itemService) validate(item *ItemDefinition) error {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = strings.TrimSpace(item.Name)
	item.SlotType = strings.ToUpper(strings.TrimSpace(item.SlotType))

	if item.ID == "" || item.Name == "" {
		return ErrInvalidModifierType // standard bad payload error
	}

	validSlots := map[string]bool{
		"WEAPON":     true,
		"BODY_ARMOR": true,
		"HELMET":     true,
		"SHIELD":     true,
		"SHOES":      true,
		"ACCESSORY":  true,
		"NONE":       true,
	}

	if !validSlots[item.SlotType] {
		return ErrInvalidSlotType
	}

	validStats := map[string]bool{
		"HP":        true,
		"MP":        true,
		"STR":       true,
		"INT":       true,
		"DEX":       true,
		"CON":       true,
		"ATTACK":    true,
		"DEFENSE":   true,
		"CRIT_RATE": true,
	}

	for _, m := range item.StatsModifiers {
		m.Stat = strings.ToUpper(strings.TrimSpace(m.Stat))
		m.Operation = strings.ToUpper(strings.TrimSpace(m.Operation))

		if !validStats[m.Stat] {
			return ErrInvalidModifierType
		}

		if m.Operation != "ADD" && m.Operation != "MULTIPLY" {
			return ErrInvalidModifierType
		}

		if m.Value <= 0.0 {
			return ErrInvalidModifierType
		}
	}

	if item.Metadata == nil {
		item.Metadata = make(map[string]any)
	}

	return nil
}
