package idle_test

import (
	"context"
	"errors"
	"testing"

	"github.com/singoesdeep/zzrpg/backend/game/idle"
)

func TestActivities_UnlockFlags(t *testing.T) {
	svc, _ := newService(&mockCharRewarder{}, &mockLootRoller{}, &mockInventoryWriter{})
	acts := svc.Activities(100, 5) // level 5, power 100

	byID := map[string]idle.ActivityView{}
	for _, a := range acts {
		byID[a.ID] = a
	}
	if !byID["training_yard"].Unlocked {
		t.Fatal("training_yard should be unlocked")
	}
	if byID["dragon_peak"].Unlocked {
		t.Fatal("dragon_peak should be locked at level 5 / power 100")
	}
	if !byID["mining"].Unlocked {
		t.Fatal("lifeskills should always be selectable")
	}
}

func TestAssign(t *testing.T) {
	ctx := context.Background()
	svc, r := newService(&mockCharRewarder{}, &mockLootRoller{}, &mockInventoryWriter{})

	// unknown activity
	if err := svc.Assign(ctx, 1, 100, 5, idle.StageAssignment("nope")); !errors.Is(err, idle.ErrActivityNotFound) {
		t.Fatalf("expected ErrActivityNotFound, got %v", err)
	}
	// locked stage
	if err := svc.Assign(ctx, 1, 100, 5, idle.StageAssignment("dragon_peak")); !errors.Is(err, idle.ErrActivityLocked) {
		t.Fatalf("expected ErrActivityLocked, got %v", err)
	}
	// valid stage (goblin_forest needs power >= 120)
	if err := svc.Assign(ctx, 1, 200, 5, idle.StageAssignment("goblin_forest")); err != nil {
		t.Fatalf("expected goblin_forest assignable, got %v", err)
	}
	if a, ok, _ := r.assign.Get(ctx, 1); !ok || a.ID != "goblin_forest" {
		t.Fatalf("assignment not persisted: %+v", a)
	}
	// valid lifeskill
	if err := svc.Assign(ctx, 1, 0, 1, idle.LifeskillAssignment("fishing")); err != nil {
		t.Fatalf("expected fishing assignable, got %v", err)
	}
}

func TestUpgradeBuilding_GoldCostBootstraps(t *testing.T) {
	ctx := context.Background()
	chars := &mockCharRewarder{gold: 500}
	svc, r := newService(chars, &mockLootRoller{}, &mockInventoryWriter{})

	// lumber_mill level 1 costs 100 gold — payable from combat gold with an
	// empty wallet (the bootstrap case).
	lvl, err := svc.UpgradeBuilding(ctx, 1, "lumber_mill")
	if err != nil || lvl != 1 {
		t.Fatalf("expected level 1, got lvl=%d err=%v", lvl, err)
	}
	if chars.spent != 100 {
		t.Fatalf("expected 100 gold spent, got %d", chars.spent)
	}
	if bl, _ := r.build.Get(ctx, 1, "lumber_mill"); bl != 1 {
		t.Fatalf("building level should be 1, got %d", bl)
	}
	// unknown generator
	if _, err := svc.UpgradeBuilding(ctx, 1, "nope"); !errors.Is(err, idle.ErrNotAGenerator) {
		t.Fatalf("expected ErrNotAGenerator, got %v", err)
	}
}

func TestUpgradeBuilding_InsufficientGold(t *testing.T) {
	ctx := context.Background()
	poor := &mockCharRewarder{gold: 10}
	svc, _ := newService(poor, &mockLootRoller{}, &mockInventoryWriter{})

	if _, err := svc.UpgradeBuilding(ctx, 2, "lumber_mill"); !errors.Is(err, idle.ErrInsufficientGold) {
		t.Fatalf("expected ErrInsufficientGold, got %v", err)
	}
	if poor.spent != 0 {
		t.Fatalf("no gold should be spent on a failed upgrade, spent=%d", poor.spent)
	}
}

func TestUpgradeBuilding_MixedGoldAndResource(t *testing.T) {
	ctx := context.Background()
	chars := &mockCharRewarder{gold: 500}
	svc, r := newService(chars, &mockLootRoller{}, &mockInventoryWriter{})

	// forge costs 150 gold + 20 stone. Gold is affordable but there is no stone.
	if _, err := svc.UpgradeBuilding(ctx, 1, "forge"); !errors.Is(err, idle.ErrInsufficientResources) {
		t.Fatalf("expected ErrInsufficientResources, got %v", err)
	}
	if chars.spent != 0 {
		t.Fatalf("gold must not be spent when the resource check fails first, spent=%d", chars.spent)
	}
	// Fund stone (e.g. from the quarry) and retry.
	_ = r.wallet.Credit(ctx, 1, "stone", 50)
	lvl, err := svc.UpgradeBuilding(ctx, 1, "forge")
	if err != nil || lvl != 1 {
		t.Fatalf("expected forge level 1, got lvl=%d err=%v", lvl, err)
	}
	if chars.spent != 150 {
		t.Fatalf("expected 150 gold spent, got %d", chars.spent)
	}
	if b, _ := r.wallet.Balances(ctx, 1); b["stone"] != 30 { // 50 - 20
		t.Fatalf("expected 30 stone left, got %d", b["stone"])
	}
}

func TestState(t *testing.T) {
	ctx := context.Background()
	svc, r := newService(&mockCharRewarder{}, &mockLootRoller{}, &mockInventoryWriter{})

	_ = r.assign.Set(ctx, 1, idle.LifeskillAssignment("mining"))
	_ = r.ls.Upsert(ctx, 1, idle.LifeskillState{SkillID: "mining", Level: 4, XP: 10})
	_ = r.build.Set(ctx, 1, "quarry", 2)
	_ = r.wallet.Credit(ctx, 1, "stone", 55)

	v, err := svc.State(ctx, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Assignment.ID != "mining" {
		t.Fatalf("assignment = %+v", v.Assignment)
	}
	if v.Buildings["quarry"] != 2 || v.Wallet["stone"] != 55 {
		t.Fatalf("buildings/wallet wrong: %+v %+v", v.Buildings, v.Wallet)
	}
	var mining bool
	for _, l := range v.Lifeskills {
		if l.SkillID == "mining" && l.Level == 4 {
			mining = true
		}
	}
	if !mining {
		t.Fatalf("mining level not reflected: %+v", v.Lifeskills)
	}
}
