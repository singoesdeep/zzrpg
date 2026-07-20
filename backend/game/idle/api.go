package idle

import (
	"context"
	"errors"
)

// Errors returned by the API-facing service methods, mapped to HTTP statuses by
// the handler.
var (
	ErrActivityNotFound      = errors.New("idle: activity not found")
	ErrActivityLocked        = errors.New("idle: activity locked for this character")
	ErrNotAGenerator         = errors.New("idle: not a generator")
	ErrMaxLevel              = errors.New("idle: generator already at max level")
	ErrInsufficientResources = errors.New("idle: insufficient resources")
)

// StateView is a character's full idle state for the API.
type StateView struct {
	Assignment Assignment       `json:"assignment"`
	Lifeskills []LifeskillState `json:"lifeskills"`
	Buildings  map[string]int32 `json:"buildings"`
	Wallet     map[string]int64 `json:"wallet"`
}

// State returns the character's assignment, lifeskill levels, building levels,
// and resource balances.
func (s *Service) State(ctx context.Context, charID int64) (StateView, error) {
	a, err := s.Assignment(ctx, charID)
	if err != nil {
		return StateView{}, err
	}
	var ls []LifeskillState
	for _, id := range s.catalog.LifeskillIDs() {
		st, err := s.deps.Lifeskills.Get(ctx, charID, id)
		if err != nil {
			return StateView{}, err
		}
		ls = append(ls, st)
	}
	buildings, err := s.deps.Buildings.Levels(ctx, charID)
	if err != nil {
		return StateView{}, err
	}
	wallet, err := s.deps.Wallet.Balances(ctx, charID)
	if err != nil {
		return StateView{}, err
	}
	return StateView{Assignment: a, Lifeskills: ls, Buildings: buildings, Wallet: wallet}, nil
}

// ActivityView is one selectable activity and whether the character may use it.
type ActivityView struct {
	Type     ActivityType `json:"type"`
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Unlocked bool         `json:"unlocked"`
}

// Activities lists every stage, lifeskill, and generator with an unlock flag for
// a character of the given power/level. Lifeskills and generators are always
// selectable; stages gate on level/power.
func (s *Service) Activities(power float64, level int32) []ActivityView {
	var out []ActivityView
	for _, id := range s.catalog.StageIDs() {
		st, _ := s.catalog.Stage(id)
		out = append(out, ActivityView{ActivityStage, id, st.Name, StageUnlocked(st, power, level)})
	}
	for _, id := range s.catalog.LifeskillIDs() {
		l, _ := s.catalog.Lifeskill(id)
		out = append(out, ActivityView{ActivityLifeskill, id, l.Name, true})
	}
	return out
}

// Assign validates and sets the character's active focus. A stage must be
// unlocked for the character; an unknown activity is rejected.
func (s *Service) Assign(ctx context.Context, charID int64, power float64, level int32, a Assignment) error {
	switch a.Type {
	case ActivityStage:
		st, ok := s.catalog.Stage(a.ID)
		if !ok {
			return ErrActivityNotFound
		}
		if !StageUnlocked(st, power, level) {
			return ErrActivityLocked
		}
	case ActivityLifeskill:
		if _, ok := s.catalog.Lifeskill(a.ID); !ok {
			return ErrActivityNotFound
		}
	default:
		return ErrActivityNotFound
	}
	return s.deps.Assignments.Set(ctx, charID, a)
}

// UpgradeBuilding spends the scaled wallet cost to raise a generator one level.
// Cost to reach level N is the generator's base UpgradeCost times N.
func (s *Service) UpgradeBuilding(ctx context.Context, charID int64, generatorID string) (int32, error) {
	g, ok := s.catalog.Generator(generatorID)
	if !ok {
		return 0, ErrNotAGenerator
	}
	cur, err := s.deps.Buildings.Get(ctx, charID, generatorID)
	if err != nil {
		return 0, err
	}
	next := cur + 1
	if g.MaxLevel > 0 && next > g.MaxLevel {
		return 0, ErrMaxLevel
	}

	balances, err := s.deps.Wallet.Balances(ctx, charID)
	if err != nil {
		return 0, err
	}
	cost := make(map[string]int64, len(g.UpgradeCost))
	for res, base := range g.UpgradeCost {
		c := base * int64(next)
		cost[res] = c
		if balances[res] < c {
			return 0, ErrInsufficientResources
		}
	}
	for res, c := range cost {
		if err := s.deps.Wallet.Credit(ctx, charID, res, -c); err != nil {
			return 0, err
		}
	}
	if err := s.deps.Buildings.Set(ctx, charID, generatorID, next); err != nil {
		return 0, err
	}
	return next, nil
}
