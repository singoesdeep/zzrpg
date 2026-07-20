// Package idle is the offline ("idle") progression domain: when a character comes
// back online it may be granted gold, exp and loot for the time it was away. The
// rules are data-driven (content/idle/offline.json). This lives in a service so
// the logic is a testable domain concern, not inline transport code.
package idle

import (
	"context"
	"math/rand"
	"time"

	"github.com/singoesdeep/zzrpg/backend/content"
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

// Grant is the outcome of an offline-gains computation.
type Grant struct {
	ElapsedSeconds float64
	Gold           int64
	Exp            int64
	LeveledUp      bool
	NewLevel       int32
	Loot           []loot.DroppedItem
}

// Service computes and applies offline gains.
type Service struct {
	chars  CharacterRewarder
	loot   LootRoller
	inv    InventoryWriter
	config *content.IdleConfig
}

// NewService builds an idle service from the offline-gains content pack.
func NewService(chars CharacterRewarder, lootSvc LootRoller, inv InventoryWriter) *Service {
	return &Service{chars: chars, loot: lootSvc, inv: inv, config: content.MustLoadIdle()}
}

// GrantOffline computes the gains a character earned while away (since
// lastActiveAt) using its base stats, applies them (rewards + loot into the
// inventory), and returns the summary. granted is false when too little time has
// elapsed (below the configured minimum), in which case nothing is applied.
func (s *Service) GrantOffline(ctx context.Context, charID int64, baseStats map[string]float64, lastActiveAt time.Time) (Grant, bool, error) {
	elapsed := time.Since(lastActiveAt).Seconds()
	if elapsed < s.config.MinSeconds {
		return Grant{}, false, nil
	}
	if elapsed > s.config.CapSeconds {
		elapsed = s.config.CapSeconds
	}

	gold := int64((elapsed / 60.0) * s.config.GoldPerMin.PerMinute(baseStats))
	exp := int64((elapsed / 60.0) * s.config.ExpPerMin.PerMinute(baseStats))

	// One loot roll per elapsed minute, capped; gold drops fold into gold, items
	// go into the inventory.
	var items []loot.DroppedItem
	rollCount := int(elapsed / 60.0)
	if rollCount > s.config.MaxRolls {
		rollCount = s.config.MaxRolls
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < rollCount; i++ {
		if rng.Float64() >= s.config.RollChance {
			continue
		}
		drops, err := s.loot.RollLoot(ctx, s.config.LootTableID)
		if err != nil {
			continue
		}
		for _, drop := range drops {
			if drop.ItemDefinitionID == "gold" {
				gold += int64(drop.Quantity)
				continue
			}
			items = append(items, drop)
			_ = s.inv.AddItem(ctx, &inventory.InventoryItem{
				CharacterID:      int32(charID),
				ItemDefinitionID: drop.ItemDefinitionID,
				Quantity:         drop.Quantity,
				Durability:       100,
			})
		}
	}

	leveledUp, newLevel, err := s.chars.AddRewards(ctx, charID, gold, exp)
	if err != nil {
		return Grant{}, false, err
	}
	return Grant{
		ElapsedSeconds: elapsed,
		Gold:           gold,
		Exp:            exp,
		LeveledUp:      leveledUp,
		NewLevel:       newLevel,
		Loot:           items,
	}, true, nil
}
