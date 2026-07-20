package character

import (
	"context"
	"errors"
	"time"

	"github.com/singoesdeep/zzrpg/backend/contracts"
)

var (
	ErrCharacterNotFound     = errors.New("character not found")
	ErrCharacterLimitReached = errors.New("character limit reached for user")
	ErrCharacterNameTaken    = errors.New("character name already taken")
	ErrInvalidClass          = errors.New("invalid character class")
	ErrNameTooShort          = errors.New("character name too short (minimum 3 characters)")
	ErrNameTooLong           = errors.New("character name too long (maximum 16 characters)")
	ErrStatUnavailable       = errors.New("stat calculation unavailable: zzstat is not loaded")
)

type Character struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	Name         string    `json:"name"`
	ClassName    string    `json:"class_name"`
	Level        int32     `json:"level"`
	Experience   int64     `json:"experience"`
	Gold         int64     `json:"gold"`
	LastActiveAt time.Time `json:"last_active_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CharacterStats struct {
	CharacterID  int64              `json:"character_id"`
	BaseStats    map[string]float64 `json:"base_stats"`
	DerivedStats map[string]float64 `json:"derived_stats"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

type CharacterWithStats struct {
	Character
	Stats CharacterStats `json:"stats"`
}

type CharacterRepository interface {
	Create(ctx context.Context, char *Character, baseStats, derivedStats map[string]float64) error
	GetByID(ctx context.Context, id int64) (*CharacterWithStats, error)
	GetByName(ctx context.Context, name string) (*Character, error)
	ListByUserID(ctx context.Context, userID int64) ([]Character, error)
	UpdateStats(ctx context.Context, charID int64, derivedStats map[string]float64) error
	UpdateLastActive(ctx context.Context, charID int64) error
	AddRewards(ctx context.Context, charID int64, gold int64, exp int64) (bool, int32, error)
	// SpendGold atomically debits amount from the character's gold if the balance
	// is sufficient, returning ok=false (no error) when it is not.
	SpendGold(ctx context.Context, charID int64, amount int64) (ok bool, err error)
}

// EquipmentModifier is a domain alias for the shared contracts.Modifier.
type EquipmentModifier = contracts.Modifier

type EquipmentProvider interface {
	GetEquippedModifiers(ctx context.Context, charID int32) ([]EquipmentModifier, error)
}
