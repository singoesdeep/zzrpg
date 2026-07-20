// Package idle is the offline/idle progression domain. It builds on the
// game-agnostic engine/idle accrual framework: elapsed away-time is fed to the
// Producer for the character's active focus (a combat stage or a gathering
// lifeskill) plus every built RTS generator running in parallel, and the
// produced Output is mapped onto the game's reward systems (gold/exp, loot,
// inventory, lifeskill xp, and the resource wallet). Activities and tuning live
// in content (content/idle/*.json).
package idle

import (
	"context"
	"math/rand"
	"strings"
	"time"

	"github.com/singoesdeep/zzrpg/backend/content"
	eidle "github.com/singoesdeep/zzrpg/backend/engine/idle"
	"github.com/singoesdeep/zzrpg/backend/game/inventory"
	"github.com/singoesdeep/zzrpg/backend/game/loot"
)

// CharacterRewarder credits gold/exp to a character.
type CharacterRewarder interface {
	AddRewards(ctx context.Context, charID int64, gold int64, exp int64) (bool, int32, error)
}

// LootRoller rolls a loot table.
type LootRoller interface {
	RollLoot(ctx context.Context, tableID string) ([]loot.DroppedItem, error)
}

// InventoryWriter grants an item to a character's inventory.
type InventoryWriter interface {
	AddItem(ctx context.Context, item *inventory.InventoryItem) error
}

// Deps are the services and repositories the idle Service applies output to.
type Deps struct {
	Chars       CharacterRewarder
	Loot        LootRoller
	Inv         InventoryWriter
	Assignments AssignmentRepo
	Lifeskills  LifeskillRepo
	Buildings   BuildingRepo
	Wallet      WalletRepo
}

// AccrualRequest describes a character accruing idle progress since a point in
// time (their last-active timestamp for an offline grant, or their last tick for
// an online one). Power/Level scale combat-stage output; the assigned activity
// and lifeskill/building levels are loaded by the Service.
type AccrualRequest struct {
	CharacterID int64
	Since       time.Time
	Power       float64
	Level       int32
}

// Grant is the applied outcome of an accrual.
type Grant struct {
	ElapsedSeconds    float64
	Gold              int64
	Exp               int64
	LeveledUp         bool
	NewLevel          int32
	Loot              []loot.DroppedItem
	LifeskillLevelUps map[string]int32 // skill id -> new level, for skills that levelled
	Resources         map[string]int64 // resource id -> amount credited to the wallet
	Output            eidle.Output     // full raw ledger the producers emitted
}

// Service computes and applies idle progress via the accrual framework.
type Service struct {
	deps     Deps
	catalog  *Catalog
	registry *eidle.Registry
	curve    content.LifeskillCurve

	minSeconds     float64
	capSeconds     float64
	maxRolls       int
	defaultStageID string
}

// NewService builds an idle service, loading the activity catalog, the lifeskill
// curve, and the global accrual bounds from content.
func NewService(deps Deps) *Service {
	cat := NewCatalog()
	cfg := content.MustLoadIdle()
	return &Service{
		deps:           deps,
		catalog:        cat,
		registry:       cat.BuildRegistry(),
		curve:          content.MustLoadLifeskillCurve(),
		minSeconds:     cfg.MinSeconds,
		capSeconds:     cfg.CapSeconds,
		maxRolls:       cfg.MaxRolls,
		defaultStageID: "training_yard",
	}
}

// Power reduces derived stats to the combat-power scalar (data-driven weights).
func (s *Service) Power(derived map[string]float64) float64 { return s.catalog.Power(derived) }

// Catalog exposes the activity catalog (for API listing / validation).
func (s *Service) Catalog() *Catalog { return s.catalog }

// Assignment returns the character's active focus, falling back to the default
// starter stage when none is set.
func (s *Service) Assignment(ctx context.Context, charID int64) (Assignment, error) {
	a, ok, err := s.deps.Assignments.Get(ctx, charID)
	if err != nil {
		return Assignment{}, err
	}
	if !ok {
		return StageAssignment(s.defaultStageID), nil
	}
	return a, nil
}

// Accrue runs the character's active producer plus every built generator over
// the clamped elapsed window and applies the merged output. granted is false
// when too little time elapsed or nothing was produced.
func (s *Service) Accrue(ctx context.Context, req AccrualRequest) (Grant, bool, error) {
	elapsedSec := time.Since(req.Since).Seconds()
	elapsedMin, ok := eidle.Window(elapsedSec, s.minSeconds, s.capSeconds)
	if !ok {
		return Grant{}, false, nil
	}
	if s.capSeconds > 0 && elapsedSec > s.capSeconds {
		elapsedSec = s.capSeconds
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Active focus (stage or lifeskill).
	assignment, err := s.Assignment(ctx, req.CharacterID)
	if err != nil {
		return Grant{}, false, err
	}
	var merged eidle.Output
	activeStageID := ""
	if producer, ok := s.registry.Get(assignment.ID); ok {
		state, err := s.activeState(ctx, req, assignment)
		if err != nil {
			return Grant{}, false, err
		}
		if producer.Unlocked(state) {
			mergeInto(&merged, producer.Produce(elapsedMin, state, rng.Float64))
			if assignment.Type == ActivityStage {
				activeStageID = assignment.ID
			}
		}
	}

	// Passive generators (RTS): every built one produces in parallel.
	levels, err := s.deps.Buildings.Levels(ctx, req.CharacterID)
	if err != nil {
		return Grant{}, false, err
	}
	for genID, lvl := range levels {
		if lvl <= 0 {
			continue
		}
		if g, ok := s.catalog.Generator(genID); ok {
			st := eidle.State{Vars: map[string]float64{VarBuildingLevel: float64(lvl)}}
			mergeInto(&merged, GeneratorProducer{G: g}.Produce(elapsedMin, st, rng.Float64))
		}
	}

	if len(merged.Amounts) == 0 && len(merged.Drops) == 0 {
		return Grant{}, false, nil
	}
	return s.apply(ctx, req.CharacterID, merged, activeStageID, elapsedSec, rng)
}

// activeState assembles the State for the active producer, loading the lifeskill
// level when the focus is a lifeskill.
func (s *Service) activeState(ctx context.Context, req AccrualRequest, a Assignment) (eidle.State, error) {
	var skillLevel int32
	if a.Type == ActivityLifeskill {
		ls, err := s.deps.Lifeskills.Get(ctx, req.CharacterID, a.ID)
		if err != nil {
			return eidle.State{}, err
		}
		skillLevel = ls.Level
	}
	return BuildState(req.Power, req.Level, skillLevel, 0), nil
}

// apply maps a merged Output onto the game's reward systems.
func (s *Service) apply(ctx context.Context, charID int64, out eidle.Output, activeStageID string, elapsedSec float64, rng *rand.Rand) (Grant, bool, error) {
	gold := out.Amounts[AmountGold]
	exp := out.Amounts[AmountExp]
	var items []loot.DroppedItem

	// Stage loot rolls against the active stage's table.
	if rolls := int(out.Amounts[AmountLootRolls]); rolls > 0 && activeStageID != "" {
		if s.maxRolls > 0 && rolls > s.maxRolls {
			rolls = s.maxRolls
		}
		if stage, ok := s.catalog.Stage(activeStageID); ok && stage.LootTableID != "" {
			for i := 0; i < rolls; i++ {
				gold += s.rollInto(ctx, stage.LootTableID, charID, &items)
			}
		}
	}

	// Concrete item drops (lifeskill gathers) go to the inventory.
	for _, d := range out.Drops {
		items = append(items, loot.DroppedItem{ItemDefinitionID: d.ID, Quantity: int32(d.Quantity)})
		_ = s.deps.Inv.AddItem(ctx, &inventory.InventoryItem{
			CharacterID:      int32(charID),
			ItemDefinitionID: d.ID,
			Quantity:         int32(d.Quantity),
			Durability:       100,
		})
	}

	// Lifeskill xp -> levels, and resource amounts -> wallet.
	levelUps := map[string]int32{}
	resources := map[string]int64{}
	for key, amt := range out.Amounts {
		switch {
		case key == AmountGold || key == AmountExp || key == AmountLootRolls:
			// handled above
		case strings.HasSuffix(key, "_xp"):
			skillID := strings.TrimSuffix(key, "_xp")
			cur, err := s.deps.Lifeskills.Get(ctx, charID, skillID)
			if err != nil {
				continue
			}
			nl, nx, up := ApplyLifeskillXP(s.curve, cur.Level, cur.XP, amt)
			_ = s.deps.Lifeskills.Upsert(ctx, charID, LifeskillState{SkillID: skillID, Level: nl, XP: nx})
			if up {
				levelUps[skillID] = nl
			}
		default:
			_ = s.deps.Wallet.Credit(ctx, charID, key, amt)
			resources[key] = amt
		}
	}

	leveledUp, newLevel, err := s.deps.Chars.AddRewards(ctx, charID, gold, exp)
	if err != nil {
		return Grant{}, false, err
	}
	return Grant{
		ElapsedSeconds:    elapsedSec,
		Gold:              gold,
		Exp:               exp,
		LeveledUp:         leveledUp,
		NewLevel:          newLevel,
		Loot:              items,
		LifeskillLevelUps: levelUps,
		Resources:         resources,
		Output:            out,
	}, true, nil
}

// rollInto rolls a loot table once, folding gold into a running total and
// appending item drops to items (also granting them to the inventory).
func (s *Service) rollInto(ctx context.Context, tableID string, charID int64, items *[]loot.DroppedItem) int64 {
	drops, err := s.deps.Loot.RollLoot(ctx, tableID)
	if err != nil {
		return 0
	}
	var gold int64
	for _, drop := range drops {
		if drop.ItemDefinitionID == "gold" {
			gold += int64(drop.Quantity)
			continue
		}
		*items = append(*items, drop)
		_ = s.deps.Inv.AddItem(ctx, &inventory.InventoryItem{
			CharacterID:      int32(charID),
			ItemDefinitionID: drop.ItemDefinitionID,
			Quantity:         drop.Quantity,
			Durability:       100,
		})
	}
	return gold
}

// mergeInto accumulates src into dst.
func mergeInto(dst *eidle.Output, src eidle.Output) {
	for k, v := range src.Amounts {
		dst.Add(k, v)
	}
	dst.Drops = append(dst.Drops, src.Drops...)
}
