package combat

import (
	"context"
	"testing"
	"time"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/killreward"
	"github.com/singoesdeep/zzrpg/backend/internal/quests"
	"github.com/singoesdeep/zzrpg/backend/internal/socket"
	"github.com/singoesdeep/zzrpg/backend/internal/statclient"
)

type mockCharService struct {
	char *character.CharacterWithStats
}

func (m *mockCharService) Create(ctx context.Context, userID int64, name, className string) (*character.CharacterWithStats, error) {
	return nil, nil
}
func (m *mockCharService) GetByID(ctx context.Context, id int64) (*character.CharacterWithStats, error) {
	return m.char, nil
}
func (m *mockCharService) ListByUserID(ctx context.Context, userID int64) ([]character.Character, error) {
	return nil, nil
}
func (m *mockCharService) RecalculateStats(ctx context.Context, id int64) error {
	return nil
}
func (m *mockCharService) SetEquipmentProvider(p character.EquipmentProvider) {}
func (m *mockCharService) AddRewards(ctx context.Context, charID int64, gold int64, exp int64) (bool, int32, error) {
	return false, 0, nil
}
func (m *mockCharService) UpdateLastActive(ctx context.Context, charID int64) error {
	return nil
}

type mockStatClient struct {
	damageRes statclient.DamageResult
}

func (m *mockStatClient) Calculate(ctx context.Context, state statclient.CharacterState) (map[string]float64, error) {
	return nil, nil
}
func (m *mockStatClient) CalculateDamage(ctx context.Context, req statclient.CalculateDamageReq) (statclient.DamageResult, error) {
	return m.damageRes, nil
}
func (m *mockStatClient) Close() error {
	return nil
}

type mockQuestService struct {
	actionCalled bool
	targetKilled string
}

func (m *mockQuestService) CreateDefinition(ctx context.Context, q *quests.QuestDefinition) error {
	return nil
}
func (m *mockQuestService) GetDefinition(ctx context.Context, id string) (*quests.QuestDefinition, error) {
	return nil, nil
}
func (m *mockQuestService) ListDefinitions(ctx context.Context, limit, offset int) ([]quests.QuestDefinition, error) {
	return nil, nil
}
func (m *mockQuestService) AcceptQuest(ctx context.Context, charID int32, questID string) error {
	return nil
}
func (m *mockQuestService) GetQuestLog(ctx context.Context, charID int32) ([]quests.CharacterQuest, error) {
	return nil, nil
}
func (m *mockQuestService) UpdateQuestProgress(ctx context.Context, charID int32, actionType string, target string, amount int32) error {
	m.actionCalled = true
	m.targetKilled = target
	return nil
}

func TestCombatExecutionPvE(t *testing.T) {
	registry := socket.GetRegistry()
	// Setup attacker session in in-memory registry
	_ = registry.StartSession(1, 100.0, 50.0)

	charService := &mockCharService{
		char: &character.CharacterWithStats{
			Character: character.Character{
				ID:        1,
				Name:      "HeroWarrior",
				ClassName: "WARRIOR",
				Level:     10,
			},
			Stats: character.CharacterStats{
				BaseStats: map[string]float64{
					"STR": 15, "CON": 15, "INT": 5, "DEX": 10,
				},
				DerivedStats: map[string]float64{
					"ATTACK": 150, "DEFENSE": 50, "HP": 225, "MP": 50, "CRIT_RATE": 5,
				},
			},
		},
	}

	statClient := &mockStatClient{
		damageRes: statclient.DamageResult{
			IsHit:  true,
			Damage: 120, // fixed test damage
			IsCrit: true,
		},
	}

	questSvc := &mockQuestService{}

	rewarder := killreward.New(charService, questSvc, nil, nil)
	service := NewCombatService(charService, statClient, registry, rewarder, nil)

	// 1. First Attack (Hit dummy)
	req := AttackRequest{
		AttackerID: 1,
		DefenderID: 9999, // dummy
	}

	res, err := service.ExecuteAttack(context.Background(), req)
	if err != nil {
		t.Fatalf("expected successful attack, got: %v", err)
	}

	if !res.IsHit || res.Damage != 120 || !res.IsCrit {
		t.Errorf("unexpected damage result: %+v", res)
	}

	if res.DefenderHP != 880 || res.DefenderIsDead {
		t.Errorf("unexpected defender state: HP=%f, Dead=%v", res.DefenderHP, res.DefenderIsDead)
	}

	// 2. High damage attack to kill dummy -> should trigger quest progress update
	statClient.damageRes = statclient.DamageResult{
		IsHit:  true,
		Damage: 900,
		IsCrit: false,
	}

	res, err = service.ExecuteAttack(context.Background(), req)
	if err != nil {
		t.Fatalf("expected successful kill attack, got: %v", err)
	}

	if res.DefenderHP != 0 || !res.DefenderIsDead {
		t.Errorf("expected defender death: HP=%f, Dead=%v", res.DefenderHP, res.DefenderIsDead)
	}

	// Verify quest progress trigger
	if !questSvc.actionCalled || questSvc.targetKilled != "wolf" {
		t.Errorf("expected quest progress to be updated on target wolf kill, got called=%v target=%s", questSvc.actionCalled, questSvc.targetKilled)
	}

	// Clean sessions
	registry.EndSession(1)
	registry.EndSession(9999)
}

// TestCombatEmitsDomainEvents proves the extension seam: a consumer that only
// subscribes to the bus (never touching combat) receives CombatAttackResolved on
// every attack and MobKilled on the killing blow — without altering combat's
// synchronous result. This is the payoff of activating the event catalog.
func TestCombatEmitsDomainEvents(t *testing.T) {
	registry := socket.GetRegistry()
	_ = registry.StartSession(2, 100.0, 50.0)

	charService := &mockCharService{
		char: &character.CharacterWithStats{
			Character: character.Character{ID: 2, Name: "Hero", ClassName: "WARRIOR", Level: 10},
			Stats: character.CharacterStats{
				BaseStats:    map[string]float64{"STR": 15, "DEX": 10},
				DerivedStats: map[string]float64{"ATTACK": 150, "CRIT_RATE": 5},
			},
		},
	}
	statClient := &mockStatClient{damageRes: statclient.DamageResult{IsHit: true, Damage: 9999}}

	eventBus := bus.NewInProc(nil)
	attackResolved := make(chan CombatAttackResolved, 1)
	mobKilled := make(chan MobKilled, 1)
	eventBus.Subscribe(EventCombatAttackResolved, func(_ context.Context, ev bus.Event) {
		attackResolved <- ev.(CombatAttackResolved)
	})
	eventBus.Subscribe(EventMobKilled, func(_ context.Context, ev bus.Event) {
		mobKilled <- ev.(MobKilled)
	})

	service := NewCombatService(charService, statClient, registry, killreward.New(charService, &mockQuestService{}, nil, nil), eventBus)

	res, err := service.ExecuteAttack(context.Background(), AttackRequest{AttackerID: 2, DefenderID: 9999})
	if err != nil {
		t.Fatalf("attack failed: %v", err)
	}
	if !res.DefenderIsDead {
		t.Fatalf("expected the dummy to die from 9999 damage, got HP=%v", res.DefenderHP)
	}

	select {
	case ev := <-attackResolved:
		if ev.AttackerID != 2 || ev.DefenderID != 9999 || !ev.DefenderIsDead {
			t.Errorf("unexpected CombatAttackResolved: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for CombatAttackResolved")
	}

	select {
	case ev := <-mobKilled:
		if ev.KillerID != 2 || ev.VictimID != 9999 || ev.LootTableID != "dummy_drops" {
			t.Errorf("unexpected MobKilled: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for MobKilled")
	}

	registry.EndSession(2)
	registry.EndSession(9999)
}
