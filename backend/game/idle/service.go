// Package idle is the offline ("idle") progression domain. It builds on the
// game-agnostic engine/idle accrual framework: on return, a character's elapsed
// away-time is fed to the Producer for whatever activity they are assigned to
// (a combat stage, a gathering lifeskill, or an RTS resource generator), and the
// produced Output is mapped onto the game's reward systems. The activities and
// their tuning live in content (content/idle/*.json), so designers can add or
// rebalance them without code changes.
package idle

import (
	"context"
	"math/rand"
	"time"

	"github.com/singoesdeep/zzrpg/backend/content"
	eidle "github.com/singoesdeep/zzrpg/backend/engine/idle"
	"github.com/singoesdeep/zzrpg/backend/game/inventory"
	"github.com/singoesdeep/zzrpg/backend/game/loot"
)

// CharacterRewarder credits gold/exp to a character (consumer-owned minimal
// surface).
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

// OfflineRequest describes a returning character: how long they were away, what
// activity they were assigned to, and the state that scales that activity's
// output (power, level, skill level, building level).
type OfflineRequest struct {
	CharacterID  int64
	LastActiveAt time.Time
	Assignment   Assignment
	State        eidle.State
}

// Grant is the outcome of an offline-gains computation, after application.
type Grant struct {
	ElapsedSeconds float64
	Gold           int64
	Exp            int64
	LeveledUp      bool
	NewLevel       int32
	Loot           []loot.DroppedItem
	// Output is the full raw ledger the producer emitted, including amounts the
	// game does not yet persist to a wallet (e.g. lifeskill xp, RTS resources).
	// Exposed so callers can surface them and Phase-2 systems can apply them.
	Output eidle.Output
}

// Service computes and applies offline gains via the accrual framework.
type Service struct {
	chars    CharacterRewarder
	loot     LootRoller
	inv      InventoryWriter
	catalog  *Catalog
	registry *eidle.Registry

	minSeconds float64
	capSeconds float64
	maxRolls   int
}

// NewService builds an idle service, loading the activity catalog and the global
// accrual bounds (min/cap elapsed, max loot rolls) from content.
func NewService(chars CharacterRewarder, lootSvc LootRoller, inv InventoryWriter) *Service {
	cat := NewCatalog()
	cfg := content.MustLoadIdle()
	return &Service{
		chars:      chars,
		loot:       lootSvc,
		inv:        inv,
		catalog:    cat,
		registry:   cat.BuildRegistry(),
		minSeconds: cfg.MinSeconds,
		capSeconds: cfg.CapSeconds,
		maxRolls:   cfg.MaxRolls,
	}
}

// Power reduces derived stats to the combat-power scalar used by stage
// activities (delegates to the catalog's data-driven weights).
func (s *Service) Power(derived map[string]float64) float64 { return s.catalog.Power(derived) }

// GrantOffline runs the producer for the character's assigned activity over the
// clamped elapsed window, applies the output, and returns the summary. granted
// is false when too little time elapsed, the activity is unknown, or it is
// locked for this character — in which case nothing is applied.
func (s *Service) GrantOffline(ctx context.Context, req OfflineRequest) (Grant, bool, error) {
	elapsedSec := time.Since(req.LastActiveAt).Seconds()
	elapsedMin, ok := eidle.Window(elapsedSec, s.minSeconds, s.capSeconds)
	if !ok {
		return Grant{}, false, nil
	}
	if s.capSeconds > 0 && elapsedSec > s.capSeconds {
		elapsedSec = s.capSeconds
	}

	producer, ok := s.registry.Get(req.Assignment.ID)
	if !ok || !producer.Unlocked(req.State) {
		return Grant{}, false, nil
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	out := producer.Produce(elapsedMin, req.State, rng.Float64)

	gold := out.Amounts[AmountGold]
	exp := out.Amounts[AmountExp]
	var items []loot.DroppedItem

	// Stage loot: resolve the owed rolls against the assigned stage's table.
	if rolls := int(out.Amounts[AmountLootRolls]); rolls > 0 {
		if s.maxRolls > 0 && rolls > s.maxRolls {
			rolls = s.maxRolls
		}
		if stage, isStage := s.catalog.Stage(req.Assignment.ID); isStage && stage.LootTableID != "" {
			for i := 0; i < rolls; i++ {
				gold += s.rollInto(ctx, stage.LootTableID, req.CharacterID, &items, rng)
			}
		}
	}

	// Concrete drops (lifeskill/generator items) go straight to the inventory.
	for _, d := range out.Drops {
		items = append(items, loot.DroppedItem{ItemDefinitionID: d.ID, Quantity: int32(d.Quantity)})
		_ = s.inv.AddItem(ctx, &inventory.InventoryItem{
			CharacterID:      int32(req.CharacterID),
			ItemDefinitionID: d.ID,
			Quantity:         int32(d.Quantity),
			Durability:       100,
		})
	}

	leveledUp, newLevel, err := s.chars.AddRewards(ctx, req.CharacterID, gold, exp)
	if err != nil {
		return Grant{}, false, err
	}
	return Grant{
		ElapsedSeconds: elapsedSec,
		Gold:           gold,
		Exp:            exp,
		LeveledUp:      leveledUp,
		NewLevel:       newLevel,
		Loot:           items,
		Output:         out,
	}, true, nil
}

// rollInto rolls a loot table once, folding gold drops into a running total and
// appending item drops to items (also granting them to the inventory). Returns
// the gold gained from this roll.
func (s *Service) rollInto(ctx context.Context, tableID string, charID int64, items *[]loot.DroppedItem, _ *rand.Rand) int64 {
	drops, err := s.loot.RollLoot(ctx, tableID)
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
		_ = s.inv.AddItem(ctx, &inventory.InventoryItem{
			CharacterID:      int32(charID),
			ItemDefinitionID: drop.ItemDefinitionID,
			Quantity:         drop.Quantity,
			Durability:       100,
		})
	}
	return gold
}
