package items

import (
	"context"
	"errors"
	"time"
)

var (
	ErrItemAlreadyExists   = errors.New("item definition already exists")
	ErrItemNotFound        = errors.New("item definition not found")
	ErrInvalidSlotType     = errors.New("invalid slot type")
	ErrInvalidModifierType = errors.New("invalid modifier parameters")
)

// Stat modifier operations.
const (
	OpAdd      = "ADD"
	OpMultiply = "MULTIPLY"
)

type StatModifier struct {
	Stat      string  `json:"stat"`      // "HP", "MP", "STR", "INT", "DEX", "CON", "ATTACK", "DEFENSE", "CRIT_RATE"
	Operation string  `json:"operation"` // "ADD", "MULTIPLY"
	Value     float64 `json:"value"`
	Priority  int32   `json:"priority"`  // Base is 10, Equipment is 20, Buff is 30
}

type ItemDefinition struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	Description       string         `json:"description"`
	SlotType          string         `json:"slot_type"` // "WEAPON", "BODY_ARMOR", "HELMET", "SHIELD", "SHOES", "ACCESSORY", "NONE"
	MinLevel          int32          `json:"min_level"`
	ClassRestrictions []string       `json:"class_restrictions"`
	StatsModifiers    []StatModifier `json:"stats_modifiers"`
	Metadata          map[string]any `json:"metadata"`
	CreatedAt         time.Time      `json:"created_at"`
}

type ItemRepository interface {
	Create(ctx context.Context, item *ItemDefinition) error
	Update(ctx context.Context, item *ItemDefinition) error
	GetByID(ctx context.Context, id string) (*ItemDefinition, error)
	List(ctx context.Context, limit, offset int) ([]ItemDefinition, error)
	Delete(ctx context.Context, id string) error
}
