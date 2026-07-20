package idle_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/singoesdeep/zzrpg/backend/internal/idle"
	"github.com/singoesdeep/zzrpg/backend/internal/inventory"
	"github.com/singoesdeep/zzrpg/backend/internal/loot"
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

func TestGrantOffline_TooShort(t *testing.T) {
	chars := &mockCharRewarder{}
	lootRoller := &mockLootRoller{}
	inv := &mockInventoryWriter{}
	svc := idle.NewService(chars, lootRoller, inv)

	baseStats := map[string]float64{"STR": 10}
	lastActive := time.Now().Add(-5 * time.Second) // Below min seconds (60s)

	grant, granted, err := svc.GrantOffline(context.Background(), 1, baseStats, lastActive)
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

	baseStats := map[string]float64{"STR": 10, "INT": 10}
	lastActive := time.Now().Add(-600 * time.Second) // 10 minutes

	grant, granted, err := svc.GrantOffline(context.Background(), 101, baseStats, lastActive)
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
}

func TestGrantOffline_RewardError(t *testing.T) {
	chars := &mockCharRewarder{err: fmt.Errorf("db error")}
	lootRoller := &mockLootRoller{}
	inv := &mockInventoryWriter{}
	svc := idle.NewService(chars, lootRoller, inv)

	baseStats := map[string]float64{"STR": 10}
	lastActive := time.Now().Add(-600 * time.Second)

	_, granted, err := svc.GrantOffline(context.Background(), 1, baseStats, lastActive)
	if err == nil {
		t.Fatalf("expected error from AddRewards, got nil")
	}
	if granted {
		t.Fatalf("expected granted to be false on error")
	}
}
