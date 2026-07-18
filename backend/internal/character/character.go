package character

import (
	"context"
	"errors"
	"time"
)

var (
	ErrCharacterNotFound      = errors.New("character not found")
	ErrCharacterLimitReached  = errors.New("character limit reached for user")
	ErrCharacterNameTaken     = errors.New("character name already taken")
	ErrInvalidClass           = errors.New("invalid character class")
	ErrNameTooShort           = errors.New("character name too short (minimum 3 characters)")
	ErrNameTooLong            = errors.New("character name too long (maximum 16 characters)")
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
	CharacterID  int64             `json:"character_id"`
	BaseStats    map[string]float64 `json:"base_stats"`
	DerivedStats map[string]float64 `json:"derived_stats"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

type CharacterWithStats struct {
	Character
	Stats CharacterStats `json:"stats"`
}

type CharacterRepository interface {
	Create(ctx context.Context, char *Character, baseStats map[string]float64) error
	GetByID(ctx context.Context, id int64) (*CharacterWithStats, error)
	GetByName(ctx context.Context, name string) (*Character, error)
	ListByUserID(ctx context.Context, userID int64) ([]Character, error)
	UpdateStats(ctx context.Context, charID int64, derivedStats map[string]float64) error
	UpdateLastActive(ctx context.Context, charID int64) error
}

type EquipmentModifier struct {
	Stat      string
	Operation string
	Value     float64
	Priority  int32
	SourceID  string
}

type EquipmentProvider interface {
	GetEquippedModifiers(ctx context.Context, charID int32) ([]EquipmentModifier, error)
}
