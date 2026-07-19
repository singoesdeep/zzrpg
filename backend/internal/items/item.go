package items

import (
	"context"
	"errors"
	"time"

	"github.com/singoesdeep/zzrpg/backend/contracts"
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

// StatModifier is a domain alias for the shared contracts.Modifier.
type StatModifier = contracts.Modifier

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
