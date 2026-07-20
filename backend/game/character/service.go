package character

import (
	"context"
	"strings"

	"github.com/singoesdeep/zzrpg/backend/content"
	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/hooks"
	"github.com/singoesdeep/zzrpg/backend/platform/statclient"
)

// classDefs is the class starting-stat pack, loaded once from embedded content.
var classDefs = content.MustLoadClasses()

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
	eventBus      bus.EventBus
	hooks         *hooks.Hooks
}

// NewCharacterService builds the character service. eventBus and hks may be nil
// (no domain events published / no reward filters applied, respectively).
func NewCharacterService(repo CharacterRepository, statClient statclient.Client, equipProvider EquipmentProvider, eventBus bus.EventBus, hks *hooks.Hooks) CharacterService {
	return &characterService{
		repo:          repo,
		statClient:    statClient,
		equipProvider: equipProvider,
		eventBus:      eventBus,
		hooks:         hks,
	}
}

// publish emits ev on the bus when one is configured. Publishing is async and
// fire-and-forget, so it never affects the service's synchronous outcome.
func (s *characterService) publish(ctx context.Context, ev bus.Event) {
	if s.eventBus != nil {
		_ = s.eventBus.Publish(ctx, ev)
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

	classBase, ok := classDefs[className]
	if !ok {
		return nil, ErrInvalidClass
	}
	// Copy so callers can't mutate the shared, loaded content definition.
	baseStats := make(map[string]float64, len(classBase))
	for k, v := range classBase {
		baseStats[k] = v
	}

	// Compute the initial derived stats through zzstat (the single source of
	// truth for stat math) — never in Go.
	if s.statClient == nil {
		return nil, ErrStatUnavailable
	}
	derivedStats, err := s.statClient.Calculate(ctx, statclient.CharacterState{
		CharacterID: 0,
		BaseStats:   baseStats,
	})
	if err != nil {
		return nil, err
	}

	char := &Character{
		UserID:    userID,
		Name:      name,
		ClassName: className,
	}

	if err := s.repo.Create(ctx, char, baseStats, derivedStats); err != nil {
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

	// 2. Fetch equipped items modifiers from equipment provider. Equipment
	// modifiers and statclient modifiers are the same shared contracts.Modifier
	// type now, so no per-field translation is needed.
	var eqModifiers []statclient.Modifier
	if s.equipProvider != nil {
		eqModifiers, err = s.equipProvider.GetEquippedModifiers(ctx, int32(charID))
		if err != nil {
			return err
		}
	}

	// 3. Assemble character state for embedded statclient call
	state := statclient.CharacterState{
		CharacterID: int32(charID),
		BaseStats:   charWithStats.Stats.BaseStats,
		Equipment:   eqModifiers,
	}

	// 4. Compute derived stats through zzstat — the single source of truth for
	// stat math. There is no Go fallback: if zzstat is unavailable, fail.
	if s.statClient == nil {
		return ErrStatUnavailable
	}
	finalStats, err := s.statClient.Calculate(ctx, state)
	if err != nil {
		return err
	}

	// 4b. Let plugins adjust the derived stats before they are cached — auras,
	// global buffs, prestige bonuses, ... The filter owns the map it returns.
	finalStats = hooks.ApplyFilters(s.hooks, ctx, HookStatsRecalc,
		StatsRecalcFilter{CharacterID: charID, DerivedStats: finalStats}).DerivedStats

	// 5. Save/Update derived stats cache in database
	if err := s.repo.UpdateStats(ctx, charID, finalStats); err != nil {
		return err
	}

	s.publish(ctx, StatsRecalculated{CharacterID: charID, DerivedStats: finalStats})
	return nil
}

func (s *characterService) AddRewards(ctx context.Context, charID int64, gold int64, exp int64) (bool, int32, error) {
	// Let plugins adjust the reward before it is applied and recorded (XP/gold
	// boosts, rested bonuses, event multipliers). The outbox RewardsGranted event
	// is written by the repo with these filtered amounts, so it stays consistent.
	r := hooks.ApplyFilters(s.hooks, ctx, HookRewards, RewardsFilter{CharacterID: charID, Gold: gold, Exp: exp})
	gold, exp = r.Gold, r.Exp

	leveledUp, newLevel, err := s.repo.AddRewards(ctx, charID, gold, exp)
	if err != nil {
		return false, 0, err
	}

	// RewardsGranted and CharacterLeveledUp are emitted transactionally via the
	// outbox inside repo.AddRewards (atomic with the reward write), then
	// dispatched by the relay — not published directly here.

	// Recalculate stats if character leveled up (since base stats increased)
	if leveledUp {
		_ = s.RecalculateStats(ctx, charID)
	}

	return leveledUp, newLevel, nil
}

func (s *characterService) UpdateLastActive(ctx context.Context, charID int64) error {
	return s.repo.UpdateLastActive(ctx, charID)
}
