package idle

import (
	"context"
	"errors"

	"github.com/singoesdeep/zzrpg/backend/content"
)

// Errors returned by the API-facing service methods, mapped to HTTP statuses by
// the handler.
var (
	ErrActivityNotFound      = errors.New("idle: activity not found")
	ErrActivityLocked        = errors.New("idle: activity locked for this character")
	ErrNotAGenerator         = errors.New("idle: not a generator")
	ErrMaxLevel              = errors.New("idle: generator already at max level")
	ErrInsufficientResources = errors.New("idle: insufficient resources")
	ErrInsufficientGold      = errors.New("idle: insufficient gold")
)

// CostGoldKey is the reserved upgrade_cost key that is paid from the character's
// gold (earned from combat) rather than the resource wallet. This lets a fresh
// character bootstrap their first generator without needing the very resource it
// would produce.
const CostGoldKey = "gold"

// GeneratorView is a generator's current level plus the cost to raise it once
// more, so a client can render the build/upgrade action.
type GeneratorView struct {
	ID       string           `json:"id"`
	Resource string           `json:"resource"`
	Level    int32            `json:"level"`
	Maxed    bool             `json:"maxed"`
	NextCost map[string]int64 `json:"next_cost,omitempty"`
}

// StateView is a character's full idle state for the API.
type StateView struct {
	Assignment Assignment       `json:"assignment"`
	Lifeskills []LifeskillState `json:"lifeskills"`
	Buildings  map[string]int32 `json:"buildings"`
	Generators []GeneratorView  `json:"generators"`
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

	var gens []GeneratorView
	for _, id := range s.catalog.GeneratorIDs() {
		g, _ := s.catalog.Generator(id)
		lvl := buildings[id]
		maxed := g.MaxLevel > 0 && lvl >= g.MaxLevel
		view := GeneratorView{ID: id, Resource: g.Resource, Level: lvl, Maxed: maxed}
		if !maxed {
			view.NextCost = UpgradeCostFor(g, lvl)
		}
		gens = append(gens, view)
	}

	return StateView{Assignment: a, Lifeskills: ls, Buildings: buildings, Generators: gens, Wallet: wallet}, nil
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

// UpgradeCostFor returns the cost to raise a generator from currentLevel to the
// next level: the generator's base UpgradeCost scaled by the target level. The
// "gold" key (CostGoldKey) is paid from the character's gold; all other keys are
// paid from the resource wallet.
func UpgradeCostFor(g content.Generator, currentLevel int32) map[string]int64 {
	next := int64(currentLevel + 1)
	cost := make(map[string]int64, len(g.UpgradeCost))
	for res, base := range g.UpgradeCost {
		cost[res] = base * next
	}
	return cost
}

// UpgradeBuilding raises a generator one level, paying its scaled cost: the
// "gold" portion from the character (atomic) and any resource portion from the
// wallet. Costs are checked before anything is debited.
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

	// Split the cost into gold vs wallet resources.
	var goldCost int64
	resCost := map[string]int64{}
	for res, c := range UpgradeCostFor(g, cur) {
		if res == CostGoldKey {
			goldCost = c
		} else {
			resCost[res] = c
		}
	}

	// Check resource affordability up front (wallet).
	balances, err := s.deps.Wallet.Balances(ctx, charID)
	if err != nil {
		return 0, err
	}
	for res, c := range resCost {
		if balances[res] < c {
			return 0, ErrInsufficientResources
		}
	}

	// Pay gold first — SpendGold checks and debits atomically, so a shortfall
	// aborts before any resource is touched.
	if goldCost > 0 {
		okGold, err := s.deps.Chars.SpendGold(ctx, charID, goldCost)
		if err != nil {
			return 0, err
		}
		if !okGold {
			return 0, ErrInsufficientGold
		}
	}
	for res, c := range resCost {
		if err := s.deps.Wallet.Credit(ctx, charID, res, -c); err != nil {
			return 0, err
		}
	}
	if err := s.deps.Buildings.Set(ctx, charID, generatorID, next); err != nil {
		return 0, err
	}
	return next, nil
}
