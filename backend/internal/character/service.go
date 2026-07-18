package character

import (
	"context"
	"strings"
)

type CharacterService interface {
	Create(ctx context.Context, userID int64, name, className string) (*CharacterWithStats, error)
	GetByID(ctx context.Context, id int64) (*CharacterWithStats, error)
	ListByUserID(ctx context.Context, userID int64) ([]Character, error)
}

type characterService struct {
	repo CharacterRepository
}

func NewCharacterService(repo CharacterRepository) CharacterService {
	return &characterService{repo: repo}
}

func (s *characterService) Create(ctx context.Context, userID int64, name, className string) (*CharacterWithStats, error) {
	name = strings.TrimSpace(name)
	className = strings.ToUpper(strings.TrimSpace(className))

	// Validation
	if len(name) < 3 {
		return nil, ErrNameTooShort
	}
	if len(name) > 16 {
		return nil, ErrNameTooLong
	}

	var baseStats map[string]float64
	switch className {
	case "WARRIOR":
		baseStats = map[string]float64{"STR": 15, "INT": 5, "DEX": 10, "CON": 15}
	case "MAGE":
		baseStats = map[string]float64{"STR": 5, "INT": 20, "DEX": 10, "CON": 10}
	case "ASSASSIN":
		baseStats = map[string]float64{"STR": 10, "INT": 5, "DEX": 20, "CON": 10}
	case "SURA":
		baseStats = map[string]float64{"STR": 12, "INT": 12, "DEX": 10, "CON": 11}
	default:
		return nil, ErrInvalidClass
	}

	char := &Character{
		UserID:    userID,
		Name:      name,
		ClassName: className,
	}

	err := s.repo.Create(ctx, char, baseStats)
	if err != nil {
		return nil, err
	}

	return s.repo.GetByID(ctx, char.ID)
}

func (s *characterService) GetByID(ctx context.Context, id int64) (*CharacterWithStats, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *characterService) ListByUserID(ctx context.Context, userID int64) ([]Character, error) {
	return s.repo.ListByUserID(ctx, userID)
}
