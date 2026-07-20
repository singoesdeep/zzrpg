package idle_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/singoesdeep/zzrpg/backend/game/idle"
	"github.com/singoesdeep/zzrpg/backend/game/inventory"
	"github.com/singoesdeep/zzrpg/backend/game/loot"
)

type mockCharRewarder struct {
	lastCharID int64
	lastGold   int64
	lastExp    int64
	leveledUp  bool
	newLevel   int32
	err        error
}

func (m *mockCharRewarder) AddRewards(ctx context.Context, charID int64, gold int64, exp int64) (bool, int32, error) {
	m.lastCharID = charID
	m.lastGold = gold
	m.lastExp = exp
	return m.leveledUp, m.newLevel, m.err
}

type mockLootRoller struct {
	drops []loot.DroppedItem
	err   error
}

func (m *mockLootRoller) RollLoot(ctx context.Context, tableID string) ([]loot.DroppedItem, error) {
	return m.drops, m.err
}

type mockInventoryWriter struct {
	added []*inventory.InventoryItem
	err   error
}

func (m *mockInventoryWriter) AddItem(ctx context.Context, item *inventory.InventoryItem) error {
	if m.err != nil {
		return m.err
	}
	m.added = append(m.added, item)
	return nil
}

// stageReq builds an offline request assigned to the training_yard stage, with
// a power/level that unlocks it, for the given away-duration.
func stageReq(charID int64, away time.Duration) idle.OfflineRequest {
	return idle.OfflineRequest{
		CharacterID:  charID,
		LastActiveAt: time.Now().Add(-away),
		Assignment:   idle.StageAssignment("training_yard"),
		State:        idle.BuildState(100, 5, 0, 0),
	}
}

func TestGrantOffline_TooShort(t *testing.T) {
	chars := &mockCharRewarder{}
	svc := idle.NewService(chars, &mockLootRoller{}, &mockInventoryWriter{})

	grant, granted, err := svc.GrantOffline(context.Background(), stageReq(1, 5*time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if granted {
		t.Fatalf("expected granted to be false for short elapsed time")
	}
	if grant.Gold != 0 || grant.Exp != 0 {
		t.Fatalf("expected 0 gains, got gold=%d, exp=%d", grant.Gold, grant.Exp)
	}
}

func TestGrantOffline_Success(t *testing.T) {
	chars := &mockCharRewarder{leveledUp: true, newLevel: 2}
	lootRoller := &mockLootRoller{
		drops: []loot.DroppedItem{
			{ItemDefinitionID: "gold", Quantity: 5},
			{ItemDefinitionID: "iron_sword", Quantity: 1},
		},
	}
	inv := &mockInventoryWriter{}
	svc := idle.NewService(chars, lootRoller, inv)

	grant, granted, err := svc.GrantOffline(context.Background(), stageReq(101, 600*time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !granted {
		t.Fatalf("expected granted to be true")
	}
	if grant.Gold == 0 && grant.Exp == 0 {
		t.Fatalf("expected non-zero gold/exp")
	}
	if !grant.LeveledUp || grant.NewLevel != 2 {
		t.Fatalf("expected level up to 2, got leveledUp=%v, newLevel=%d", grant.LeveledUp, grant.NewLevel)
	}
	// loot rolls granted the iron_sword into the inventory
	if len(inv.added) == 0 {
		t.Fatalf("expected loot items granted to inventory")
	}
}

func TestGrantOffline_LockedStageGrantsNothing(t *testing.T) {
	chars := &mockCharRewarder{}
	svc := idle.NewService(chars, &mockLootRoller{}, &mockInventoryWriter{})

	// dragon_peak requires level 20 / power 600; a level-5, power-100 character
	// is locked out entirely.
	req := idle.OfflineRequest{
		CharacterID:  7,
		LastActiveAt: time.Now().Add(-3600 * time.Second),
		Assignment:   idle.StageAssignment("dragon_peak"),
		State:        idle.BuildState(100, 5, 0, 0),
	}
	_, granted, err := svc.GrantOffline(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if granted {
		t.Fatalf("expected no grant for a locked stage")
	}
	if chars.lastGold != 0 {
		t.Fatalf("locked stage must not credit rewards")
	}
}

func TestGrantOffline_Lifeskill(t *testing.T) {
	chars := &mockCharRewarder{}
	inv := &mockInventoryWriter{}
	svc := idle.NewService(chars, &mockLootRoller{}, inv)

	req := idle.OfflineRequest{
		CharacterID:  9,
		LastActiveAt: time.Now().Add(-600 * time.Second),
		Assignment:   idle.LifeskillAssignment("mining"),
		State:        idle.BuildState(0, 5, 8, 0), // skill level 8
	}
	grant, granted, err := svc.GrantOffline(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !granted {
		t.Fatalf("expected mining to grant")
	}
	if len(grant.Loot) == 0 || len(inv.added) == 0 {
		t.Fatalf("expected gathered ore in loot/inventory, got loot=%d added=%d", len(grant.Loot), len(inv.added))
	}
	if grant.Output.Amounts["mining_xp"] == 0 {
		t.Fatalf("expected mining xp in the output ledger")
	}
}

func TestGrantOffline_RewardError(t *testing.T) {
	chars := &mockCharRewarder{err: fmt.Errorf("db error")}
	svc := idle.NewService(chars, &mockLootRoller{}, &mockInventoryWriter{})

	_, granted, err := svc.GrantOffline(context.Background(), stageReq(1, 600*time.Second))
	if err == nil {
		t.Fatalf("expected error from AddRewards, got nil")
	}
	if granted {
		t.Fatalf("expected granted to be false on error")
	}
}
