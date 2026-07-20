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
	gold       int64 // available gold for SpendGold
	spent      int64 // total gold spent
}

func (m *mockCharRewarder) AddRewards(ctx context.Context, charID int64, gold int64, exp int64) (bool, int32, error) {
	m.lastCharID = charID
	m.lastGold = gold
	m.lastExp = exp
	return m.leveledUp, m.newLevel, m.err
}

func (m *mockCharRewarder) SpendGold(ctx context.Context, charID int64, amount int64) (bool, error) {
	if m.gold < amount {
		return false, nil
	}
	m.gold -= amount
	m.spent += amount
	return true, nil
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

type repos struct {
	assign idle.AssignmentRepo
	ls     idle.LifeskillRepo
	build  idle.BuildingRepo
	wallet idle.WalletRepo
}

func newService(chars idle.CharacterRewarder, lootSvc idle.LootRoller, inv idle.InventoryWriter) (*idle.Service, repos) {
	r := repos{
		assign: idle.NewMemAssignmentRepo(),
		ls:     idle.NewMemLifeskillRepo(),
		build:  idle.NewMemBuildingRepo(),
		wallet: idle.NewMemWalletRepo(),
	}
	svc := idle.NewService(idle.Deps{
		Chars: chars, Loot: lootSvc, Inv: inv,
		Assignments: r.assign, Lifeskills: r.ls, Buildings: r.build, Wallet: r.wallet,
	})
	return svc, r
}

func stageReq(charID int64, away time.Duration) idle.AccrualRequest {
	return idle.AccrualRequest{
		CharacterID: charID,
		Since:       time.Now().Add(-away),
		Power:       100,
		Level:       5,
	}
}

func TestAccrue_TooShort(t *testing.T) {
	chars := &mockCharRewarder{}
	svc, _ := newService(chars, &mockLootRoller{}, &mockInventoryWriter{})

	grant, granted, err := svc.Accrue(context.Background(), stageReq(1, 5*time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if granted || grant.Gold != 0 {
		t.Fatalf("expected no grant for short elapsed, got granted=%v gold=%d", granted, grant.Gold)
	}
}

func TestAccrue_DefaultStageSuccess(t *testing.T) {
	chars := &mockCharRewarder{leveledUp: true, newLevel: 2}
	lootRoller := &mockLootRoller{drops: []loot.DroppedItem{
		{ItemDefinitionID: "gold", Quantity: 5},
		{ItemDefinitionID: "iron_sword", Quantity: 1},
	}}
	inv := &mockInventoryWriter{}
	svc, _ := newService(chars, lootRoller, inv)

	// No assignment set -> defaults to the starter stage.
	grant, granted, err := svc.Accrue(context.Background(), stageReq(101, 600*time.Second))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !granted {
		t.Fatal("expected a grant")
	}
	if grant.Gold == 0 && grant.Exp == 0 {
		t.Fatal("expected non-zero gold/exp")
	}
	if !grant.LeveledUp || grant.NewLevel != 2 {
		t.Fatalf("expected level up to 2, got %+v", grant)
	}
	if len(inv.added) == 0 {
		t.Fatal("expected loot items in inventory")
	}
}

func TestAccrue_LockedStageNoActiveButGeneratorsRun(t *testing.T) {
	chars := &mockCharRewarder{}
	inv := &mockInventoryWriter{}
	svc, r := newService(chars, &mockLootRoller{}, inv)

	// Assign a stage the character is too weak for, and build a quarry: the
	// active focus produces nothing but the generator still runs in parallel.
	_ = r.assign.Set(context.Background(), 7, idle.StageAssignment("dragon_peak"))
	_ = r.build.Set(context.Background(), 7, "quarry", 2)

	grant, granted, err := svc.Accrue(context.Background(), idle.AccrualRequest{
		CharacterID: 7, Since: time.Now().Add(-600 * time.Second), Power: 100, Level: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !granted {
		t.Fatal("expected the generator to produce even with a locked stage")
	}
	if grant.Gold != 0 {
		t.Fatalf("locked stage must not grant combat gold, got %d", grant.Gold)
	}
	if grant.Resources["stone"] == 0 {
		t.Fatalf("expected quarry to produce stone, got %+v", grant.Resources)
	}
	if b, _ := r.wallet.Balances(context.Background(), 7); b["stone"] != grant.Resources["stone"] {
		t.Fatalf("wallet should hold the credited stone")
	}
}

func TestAccrue_Lifeskill(t *testing.T) {
	chars := &mockCharRewarder{}
	inv := &mockInventoryWriter{}
	svc, r := newService(chars, &mockLootRoller{}, inv)

	ctx := context.Background()
	_ = r.assign.Set(ctx, 9, idle.LifeskillAssignment("mining"))
	_ = r.ls.Upsert(ctx, 9, idle.LifeskillState{SkillID: "mining", Level: 8, XP: 0})

	grant, granted, err := svc.Accrue(ctx, idle.AccrualRequest{
		CharacterID: 9, Since: time.Now().Add(-600 * time.Second), Power: 0, Level: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !granted {
		t.Fatal("expected mining to grant")
	}
	if len(grant.Loot) == 0 || len(inv.added) == 0 {
		t.Fatalf("expected gathered ore, got loot=%d added=%d", len(grant.Loot), len(inv.added))
	}
	if grant.Output.Amounts["mining_xp"] == 0 {
		t.Fatal("expected mining xp in the ledger")
	}
	// xp was applied to the persisted lifeskill.
	if s, _ := r.ls.Get(ctx, 9, "mining"); s.XP == 0 && s.Level == 8 {
		t.Fatal("expected mining progress to be persisted")
	}
}

func TestAccrue_RewardError(t *testing.T) {
	chars := &mockCharRewarder{err: fmt.Errorf("db error")}
	svc, _ := newService(chars, &mockLootRoller{}, &mockInventoryWriter{})

	_, granted, err := svc.Accrue(context.Background(), stageReq(1, 600*time.Second))
	if err == nil {
		t.Fatal("expected error from AddRewards")
	}
	if granted {
		t.Fatal("expected granted=false on error")
	}
}
