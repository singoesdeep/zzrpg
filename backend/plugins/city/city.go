// Package city is a self-contained example game — a minimalist idle city
// builder — that exists purely to prove the plugin surface: it uses NO
// characters, combat, or zzstat, and touches no engine/platform code. It ships
// its own schema (Migrator), defines its own content type (registry content
// registry), exposes a typed service key, and drives resource production through
// the game-agnostic engine/idle accrual framework.
package city

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"sort"
	"time"

	eidle "github.com/singoesdeep/zzrpg/sdk/engine/idle"
	"github.com/singoesdeep/zzrpg/sdk/engine/registry"
	"github.com/singoesdeep/zzrpg/sdk/engine/store"
)

// ContentKind is this game's own content type, registered generically.
const ContentKind = "city_building"

// StarterGold is granted when a city is founded so the first building is
// affordable (bootstrapping).
const StarterGold = 100

// Errors surfaced to the handler.
var (
	ErrNoCity           = errors.New("city: not founded")
	ErrCityExists       = errors.New("city: already founded")
	ErrUnknownBuilding  = errors.New("city: unknown building")
	ErrCannotAfford     = errors.New("city: insufficient resources")
	ErrNothingToCollect = errors.New("city: nothing to collect yet")
	minCollectSeconds   = 1.0
	capCollectSeconds   = 86400.0 // a day
	ServiceKey          = registry.NewKey[*Service]("city")
)

// BuildingDef is the content each city building is described by. The engine
// knows nothing about this type — the plugin defines it.
type BuildingDef struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Resource   string           `json:"resource"`
	BasePerMin float64          `json:"base_per_min"`
	PerLevel   float64          `json:"per_level"`
	Cost       map[string]int64 `json:"cost"`
}

// buildingProducer adapts a BuildingDef to the engine/idle Producer contract:
// output scales with the building's level.
type buildingProducer struct{ def BuildingDef }

func (buildingProducer) Unlocked(eidle.State) bool { return true }

func (p buildingProducer) Produce(elapsedMin float64, s eidle.State, _ func() float64) eidle.Output {
	rate := p.def.BasePerMin + p.def.PerLevel*s.Get("level")
	var o eidle.Output
	o.Add(p.def.Resource, int64(rate*elapsedMin))
	return o
}

// Service is the city game's domain service. It persists via store.Store and
// defines/loads its own content into the registry.
type Service struct {
	db    store.Store
	reg   *registry.Registry
	order []string // building ids, load order
}

// NewService defines the city_building content type, loads the embedded building
// pack through the generic content registry, and returns the service.
func NewService(db store.Store, reg *registry.Registry, buildingsJSON []byte) (*Service, error) {
	if err := registry.DefineContent[BuildingDef](reg, ContentKind); err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buildingsJSON, &raw); err != nil {
		return nil, err
	}
	s := &Service{db: db, reg: reg}
	for id, r := range raw {
		if err := reg.LoadContent(ContentKind, id, r); err != nil {
			return nil, err
		}
		s.order = append(s.order, id)
	}
	sort.Strings(s.order)
	return s, nil
}

func (s *Service) building(id string) (BuildingDef, bool) {
	return registry.Content[BuildingDef](s.reg, ContentKind, id)
}

// Buildings returns the defined buildings in load order (for the API).
func (s *Service) Buildings() []BuildingDef {
	out := make([]BuildingDef, 0, len(s.order))
	for _, id := range s.order {
		if b, ok := s.building(id); ok {
			out = append(out, b)
		}
	}
	return out
}

// --- persistence helpers -----------------------------------------------------

func (s *Service) exists(ctx context.Context, owner string) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM cities WHERE owner = $1)", owner).Scan(&ok)
	return ok, err
}

func (s *Service) credit(ctx context.Context, q store.Querier, owner, res string, amt int64) error {
	_, err := q.Exec(ctx, `
		INSERT INTO city_resources (owner, resource_id, amount) VALUES ($1,$2,$3)
		ON CONFLICT (owner, resource_id) DO UPDATE SET amount = city_resources.amount + EXCLUDED.amount`,
		owner, res, amt)
	return err
}

func (s *Service) resources(ctx context.Context, owner string) (map[string]int64, error) {
	rows, err := s.db.Query(ctx, "SELECT resource_id, amount FROM city_resources WHERE owner = $1", owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var amt int64
		if err := rows.Scan(&id, &amt); err != nil {
			return nil, err
		}
		out[id] = amt
	}
	return out, rows.Err()
}

func (s *Service) levels(ctx context.Context, owner string) (map[string]int32, error) {
	rows, err := s.db.Query(ctx, "SELECT building_id, level FROM city_buildings WHERE owner = $1", owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int32{}
	for rows.Next() {
		var id string
		var lvl int32
		if err := rows.Scan(&id, &lvl); err != nil {
			return nil, err
		}
		out[id] = lvl
	}
	return out, rows.Err()
}

// --- game actions ------------------------------------------------------------

// Found creates a city and grants starter gold.
func (s *Service) Found(ctx context.Context, owner string) error {
	if ok, err := s.exists(ctx, owner); err != nil {
		return err
	} else if ok {
		return ErrCityExists
	}
	return s.db.WithinTx(ctx, func(q store.Querier) error {
		if _, err := q.Exec(ctx, "INSERT INTO cities (owner) VALUES ($1)", owner); err != nil {
			return err
		}
		return s.credit(ctx, q, owner, "gold", StarterGold)
	})
}

// Build raises a building one level, paying its scaled cost (base cost * next
// level) from the city's resource wallet.
func (s *Service) Build(ctx context.Context, owner, buildingID string) (int32, error) {
	if ok, err := s.exists(ctx, owner); err != nil {
		return 0, err
	} else if !ok {
		return 0, ErrNoCity
	}
	def, ok := s.building(buildingID)
	if !ok {
		return 0, ErrUnknownBuilding
	}
	levels, err := s.levels(ctx, owner)
	if err != nil {
		return 0, err
	}
	next := levels[buildingID] + 1

	bal, err := s.resources(ctx, owner)
	if err != nil {
		return 0, err
	}
	cost := map[string]int64{}
	for res, base := range def.Cost {
		c := base * int64(next)
		cost[res] = c
		if bal[res] < c {
			return 0, ErrCannotAfford
		}
	}

	err = s.db.WithinTx(ctx, func(q store.Querier) error {
		for res, c := range cost {
			if err := s.credit(ctx, q, owner, res, -c); err != nil {
				return err
			}
		}
		_, err := q.Exec(ctx, `
			INSERT INTO city_buildings (owner, building_id, level) VALUES ($1,$2,$3)
			ON CONFLICT (owner, building_id) DO UPDATE SET level = EXCLUDED.level`,
			owner, buildingID, next)
		return err
	})
	if err != nil {
		return 0, err
	}
	return next, nil
}

// Collect accrues production from every built building since the city's last
// tick — via the engine/idle framework — credits it, and advances the tick.
func (s *Service) Collect(ctx context.Context, owner string) (map[string]int64, error) {
	var lastTick time.Time
	err := s.db.QueryRow(ctx, "SELECT last_tick FROM cities WHERE owner = $1", owner).Scan(&lastTick)
	if err != nil {
		return nil, ErrNoCity
	}
	elapsedMin, ok := eidle.Window(time.Since(lastTick).Seconds(), minCollectSeconds, capCollectSeconds)
	if !ok {
		return nil, ErrNothingToCollect
	}

	levels, err := s.levels(ctx, owner)
	if err != nil {
		return nil, err
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	var merged eidle.Output
	for id, lvl := range levels {
		if lvl <= 0 {
			continue
		}
		if def, ok := s.building(id); ok {
			st := eidle.State{Vars: map[string]float64{"level": float64(lvl)}}
			out := buildingProducer{def: def}.Produce(elapsedMin, st, rng.Float64)
			for k, v := range out.Amounts {
				merged.Add(k, v)
			}
		}
	}
	if len(merged.Amounts) == 0 {
		// Still advance the tick so time isn't double-counted next call.
		_, _ = s.db.Exec(ctx, "UPDATE cities SET last_tick = now() WHERE owner = $1", owner)
		return map[string]int64{}, nil
	}

	err = s.db.WithinTx(ctx, func(q store.Querier) error {
		for res, amt := range merged.Amounts {
			if err := s.credit(ctx, q, owner, res, amt); err != nil {
				return err
			}
		}
		_, err := q.Exec(ctx, "UPDATE cities SET last_tick = now() WHERE owner = $1", owner)
		return err
	})
	if err != nil {
		return nil, err
	}
	return merged.Amounts, nil
}

// StateView is the city's full state for the API.
type StateView struct {
	Owner     string           `json:"owner"`
	Resources map[string]int64 `json:"resources"`
	Buildings []BuildingView   `json:"buildings"`
}

// BuildingView is a building's level plus the cost to raise it once more.
type BuildingView struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Resource string           `json:"resource"`
	Level    int32            `json:"level"`
	NextCost map[string]int64 `json:"next_cost"`
}

// State returns the city's resources and buildings-with-next-cost.
func (s *Service) State(ctx context.Context, owner string) (StateView, error) {
	if ok, err := s.exists(ctx, owner); err != nil {
		return StateView{}, err
	} else if !ok {
		return StateView{}, ErrNoCity
	}
	res, err := s.resources(ctx, owner)
	if err != nil {
		return StateView{}, err
	}
	levels, err := s.levels(ctx, owner)
	if err != nil {
		return StateView{}, err
	}
	var bvs []BuildingView
	for _, def := range s.Buildings() {
		lvl := levels[def.ID]
		next := map[string]int64{}
		for r, base := range def.Cost {
			next[r] = base * int64(lvl+1)
		}
		bvs = append(bvs, BuildingView{ID: def.ID, Name: def.Name, Resource: def.Resource, Level: lvl, NextCost: next})
	}
	return StateView{Owner: owner, Resources: res, Buildings: bvs}, nil
}
