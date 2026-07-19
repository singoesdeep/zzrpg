package character

import (
	"context"
	"strings"

	"github.com/singoesdeep/zzrpg/backend/internal/statclient"
)

type CharacterService interface {
	Create(ctx context.Context, userID int64, name, className string) (*CharacterWithStats, error)
	GetByID(ctx context.Context, id int64) (*CharacterWithStats, error)
	ListByUserID(ctx context.Context, userID int64) ([]Character, error)
	RecalculateStats(ctx context.Context, id int64) error
	SetEquipmentProvider(p EquipmentProvider)
	AddRewards(ctx context.Context, charID int64, gold int64, exp int64) (bool, int32, error)
	UpdateLastActive(ctx context.Context, charID int64) error
}

type characterService struct {
	repo          CharacterRepository
	statClient    statclient.Client
	equipProvider EquipmentProvider
}

func NewCharacterService(repo CharacterRepository, statClient statclient.Client, equipProvider EquipmentProvider) CharacterService {
	return &characterService{
		repo:          repo,
		statClient:    statClient,
		equipProvider: equipProvider,
	}
}

func (s *characterService) SetEquipmentProvider(p EquipmentProvider) {
	s.equipProvider = p
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

func (s *characterService) RecalculateStats(ctx context.Context, charID int64) error {
	// 1. Fetch character details and current base stats
	charWithStats, err := s.repo.GetByID(ctx, charID)
	if err != nil {
		return err
	}

	// 2. Fetch equipped items modifiers from equipment provider
	var eqModifiers []statclient.Modifier
	if s.equipProvider != nil {
		eqMods, err := s.equipProvider.GetEquippedModifiers(ctx, int32(charID))
		if err != nil {
			return err
		}
		for _, m := range eqMods {
			eqModifiers = append(eqModifiers, statclient.Modifier{
				Stat:      m.Stat,
				Operation: m.Operation,
				Value:     m.Value,
				Priority:  m.Priority,
				SourceID:  m.SourceID,
			})
		}
	}

	// 3. Assemble character state for embedded statclient call
	state := statclient.CharacterState{
		CharacterID: int32(charID),
		BaseStats:   charWithStats.Stats.BaseStats,
		Equipment:   eqModifiers,
	}

	// 4. Call embedded client (or fallback if statClient is nil)
	var finalStats map[string]float64
	if s.statClient != nil {
		finalStats, err = s.statClient.Calculate(ctx, state)
		if err != nil {
			return err
		}
	} else {
		// Fallback when the resolver is unavailable (tests / local). Same formula
		// as character creation — see FallbackDerivedStats in stats.go.
		finalStats = FallbackDerivedStats(charWithStats.Stats.BaseStats)
	}

	// 5. Save/Update derived stats cache in database
	return s.repo.UpdateStats(ctx, charID, finalStats)
}

func (s *characterService) AddRewards(ctx context.Context, charID int64, gold int64, exp int64) (bool, int32, error) {
	leveledUp, newLevel, err := s.repo.AddRewards(ctx, charID, gold, exp)
	if err != nil {
		return false, 0, err
	}

	// Recalculate stats if character leveled up (since base stats increased)
	if leveledUp {
		_ = s.RecalculateStats(ctx, charID)
	}

	return leveledUp, newLevel, nil
}

func (s *characterService) UpdateLastActive(ctx context.Context, charID int64) error {
	return s.repo.UpdateLastActive(ctx, charID)
}
