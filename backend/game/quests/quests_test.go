package quests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/hooks"
	"github.com/singoesdeep/zzrpg/backend/game/character"
	"github.com/singoesdeep/zzrpg/backend/game/inventory"
)

type mockQuestRepository struct {
	definitions map[string]*QuestDefinition
	active      map[string]*CharacterQuest
}

func newMockQuestRepository() *mockQuestRepository {
	return &mockQuestRepository{
		definitions: make(map[string]*QuestDefinition),
		active:      make(map[string]*CharacterQuest),
	}
}

func (m *mockQuestRepository) CreateDefinition(ctx context.Context, q *QuestDefinition) error {
	m.definitions[q.ID] = q
	return nil
}

func (m *mockQuestRepository) GetDefinition(ctx context.Context, id string) (*QuestDefinition, error) {
	q, ok := m.definitions[id]
	if !ok {
		return nil, ErrQuestNotFound
	}
	return q, nil
}

func (m *mockQuestRepository) ListDefinitions(ctx context.Context, limit, offset int) ([]QuestDefinition, error) {
	var list []QuestDefinition
	for _, q := range m.definitions {
		list = append(list, *q)
	}
	return list, nil
}

func (m *mockQuestRepository) AcceptQuest(ctx context.Context, charID int32, questID string, initialProgress []int32) error {
	qd := m.definitions[questID]
	key := string(rune(charID)) + "_" + questID
	m.active[key] = &CharacterQuest{
		CharacterID:      charID,
		QuestID:          questID,
		Status:           "ACTIVE",
		CurrentStepIndex: 0,
		Progress:         initialProgress,
		UpdatedAt:        time.Now(),
		Definition:       qd,
	}
	return nil
}

func (m *mockQuestRepository) GetCharacterQuest(ctx context.Context, charID int32, questID string) (*CharacterQuest, error) {
	key := string(rune(charID)) + "_" + questID
	cq, ok := m.active[key]
	if !ok {
		return nil, ErrQuestNotFound
	}
	return cq, nil
}

func (m *mockQuestRepository) ListCharacterQuests(ctx context.Context, charID int32) ([]CharacterQuest, error) {
	var list []CharacterQuest
	for _, cq := range m.active {
		if cq.CharacterID == charID {
			list = append(list, *cq)
		}
	}
	return list, nil
}

func (m *mockQuestRepository) UpdateProgress(ctx context.Context, charID int32, questID string, currentStep int32, progress []int32) error {
	key := string(rune(charID)) + "_" + questID
	cq, ok := m.active[key]
	if !ok {
		return ErrQuestNotFound
	}
	cq.CurrentStepIndex = currentStep
	cq.Progress = progress
	cq.UpdatedAt = time.Now()
	return nil
}

func (m *mockQuestRepository) CompleteQuest(ctx context.Context, charID int32, questID string) error {
	key := string(rune(charID)) + "_" + questID
	cq, ok := m.active[key]
	if !ok {
		return ErrQuestNotFound
	}
	cq.Status = "COMPLETED"
	cq.UpdatedAt = time.Now()
	return nil
}

type mockCharService struct {
	char *character.CharacterWithStats
	gold int64
	exp  int64
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
	m.gold += gold
	m.exp += exp
	return false, 0, nil
}
func (m *mockCharService) UpdateLastActive(ctx context.Context, charID int64) error {
	return nil
}

type mockInventoryService struct {
	addedItems []*inventory.InventoryItem
}

func (m *mockInventoryService) MoveItem(ctx context.Context, charID int32, fromSlot, toSlot int32) error {
	return nil
}
func (m *mockInventoryService) GetInventory(ctx context.Context, charID int32) ([]inventory.InventoryItem, error) {
	return nil, nil
}
func (m *mockInventoryService) AddItem(ctx context.Context, item *inventory.InventoryItem) error {
	m.addedItems = append(m.addedItems, item)
	return nil
}
func (m *mockInventoryService) GetEquippedModifiers(ctx context.Context, charID int32) ([]character.EquipmentModifier, error) {
	return nil, nil
}

func (m *mockInventoryService) VerifyOwnership(ctx context.Context, userID int64, charID int32) error {
	return nil
}

func TestQuestProgressionAndRewards(t *testing.T) {
	repo := newMockQuestRepository()
	charService := &mockCharService{
		char: &character.CharacterWithStats{
			Character: character.Character{
				ID:        1,
				Name:      "SavasciKral",
				ClassName: "WARRIOR",
				Level:     10,
			},
		},
	}
	inventorySvc := &mockInventoryService{}

	service := NewQuestService(repo, charService, inventorySvc, nil, nil)

	// 1. Create a 2-step Quest Definition
	questDef := &QuestDefinition{
		ID:          "wolf_slayer",
		Title:       "Wolf Slayer Quest",
		Description: "Kill wolves and talk to Blacksmith",
		MinLevel:    5,
		Steps: []QuestStep{
			{Type: "KILL_MOB", Target: "wolf", Count: 3},
			{Type: "TALK_NPC", Target: "blacksmith", Count: 1},
		},
		Rewards: QuestRewards{
			Gold:       1000,
			Experience: 2500,
			Items: []RewardItem{
				{ItemDefinitionID: "red_potion_1", Quantity: 5},
			},
		},
	}
	_ = service.CreateDefinition(context.Background(), questDef)

	// 2. Test Level Restriction: Accept quest level 12 (character is 10)
	highLvlQuest := &QuestDefinition{
		ID:       "orc_lord",
		MinLevel: 12,
	}
	_ = service.CreateDefinition(context.Background(), highLvlQuest)

	err := service.AcceptQuest(context.Background(), 1, "orc_lord")
	if err != ErrLevelRequirement {
		t.Errorf("expected ErrLevelRequirement, got %v", err)
	}

	// 3. Test Accept Quest Success
	err = service.AcceptQuest(context.Background(), 1, "wolf_slayer")
	if err != nil {
		t.Fatalf("expected successful accept, got %v", err)
	}

	// Check status is ACTIVE and progress is [0, 0]
	cq, _ := repo.GetCharacterQuest(context.Background(), 1, "wolf_slayer")
	if cq.Status != "ACTIVE" || cq.CurrentStepIndex != 0 || cq.Progress[0] != 0 {
		t.Errorf("unexpected quest status details: %+v", cq)
	}

	// 4. Test Update Progress on Step 0 (KILL_MOB wolf)
	// Add 2 wolves
	err = service.UpdateQuestProgress(context.Background(), 1, "KILL_MOB", "wolf", 2)
	if err != nil {
		t.Fatalf("expected no error updating progress, got %v", err)
	}
	cq, _ = repo.GetCharacterQuest(context.Background(), 1, "wolf_slayer")
	if cq.Progress[0] != 2 || cq.CurrentStepIndex != 0 {
		t.Errorf("expected step progress 2 at step 0, got progress %+v index %d", cq.Progress, cq.CurrentStepIndex)
	}

	// Add 1 more wolf to complete Step 0 -> should auto promote to Step 1
	err = service.UpdateQuestProgress(context.Background(), 1, "KILL_MOB", "wolf", 1)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	cq, _ = repo.GetCharacterQuest(context.Background(), 1, "wolf_slayer")
	if cq.CurrentStepIndex != 1 || cq.Progress[0] != 3 {
		t.Errorf("expected promotion to step 1, got index %d, progress %+v", cq.CurrentStepIndex, cq.Progress)
	}

	// 5. Complete Step 1 (TALK_NPC blacksmith) -> should complete Quest and distribute Rewards
	err = service.UpdateQuestProgress(context.Background(), 1, "TALK_NPC", "blacksmith", 1)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	cq, _ = repo.GetCharacterQuest(context.Background(), 1, "wolf_slayer")
	if cq.Status != "COMPLETED" {
		t.Errorf("expected quest status to be COMPLETED, got %s", cq.Status)
	}

	// Verify Rewards
	if charService.gold != 1000 || charService.exp != 2500 {
		t.Errorf("rewards not matched: gold=%d exp=%d", charService.gold, charService.exp)
	}

	if len(inventorySvc.addedItems) != 1 || inventorySvc.addedItems[0].ItemDefinitionID != "red_potion_1" || inventorySvc.addedItems[0].Quantity != 5 {
		t.Errorf("reward item not matched: %+v", inventorySvc.addedItems)
	}
}

// TestQuestEmitsLifecycleEvents proves the quest seam: a bus subscriber receives
// QuestAccepted on accept and QuestCompleted when a single-step quest finishes —
// without the quest service depending on the consumer.
func TestQuestEmitsLifecycleEvents(t *testing.T) {
	repo := newMockQuestRepository()
	charService := &mockCharService{
		char: &character.CharacterWithStats{
			Character: character.Character{ID: 1, Name: "Hero", ClassName: "WARRIOR", Level: 10},
		},
	}
	eventBus := bus.NewInProc(nil)
	accepted := make(chan QuestAccepted, 1)
	completed := make(chan QuestCompleted, 1)
	eventBus.Subscribe(EventQuestAccepted, func(_ context.Context, ev bus.Event) {
		accepted <- ev.(QuestAccepted)
	})
	eventBus.Subscribe(EventQuestCompleted, func(_ context.Context, ev bus.Event) {
		completed <- ev.(QuestCompleted)
	})

	service := NewQuestService(repo, charService, &mockInventoryService{}, eventBus, nil)

	def := &QuestDefinition{
		ID:       "slime_hunt",
		MinLevel: 1,
		Steps:    []QuestStep{{Type: "KILL_MOB", Target: "slime", Count: 1}},
		Rewards:  QuestRewards{Gold: 50, Experience: 100},
	}
	_ = service.CreateDefinition(context.Background(), def)

	if err := service.AcceptQuest(context.Background(), 1, "slime_hunt"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	select {
	case ev := <-accepted:
		if ev.CharacterID != 1 || ev.QuestID != "slime_hunt" {
			t.Errorf("unexpected QuestAccepted: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for QuestAccepted")
	}

	if err := service.UpdateQuestProgress(context.Background(), 1, "KILL_MOB", "slime", 1); err != nil {
		t.Fatalf("progress: %v", err)
	}
	select {
	case ev := <-completed:
		if ev.CharacterID != 1 || ev.QuestID != "slime_hunt" {
			t.Errorf("unexpected QuestCompleted: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for QuestCompleted")
	}
}

// TestQuestAcceptHookVeto proves an action hook can block accepting a quest.
func TestQuestAcceptHookVeto(t *testing.T) {
	repo := newMockQuestRepository()
	charSvc := &mockCharService{char: &character.CharacterWithStats{
		Character: character.Character{ID: 1, Level: 10},
	}}
	hks := hooks.New(nil)
	hooks.AddAction(hks, HookAccept, 10, func(_ context.Context, qa QuestAccept) error {
		return errors.New("locked: finish the prologue first")
	})
	service := NewQuestService(repo, charSvc, &mockInventoryService{}, nil, hks)
	_ = service.CreateDefinition(context.Background(), &QuestDefinition{ID: "q1", MinLevel: 1})

	err := service.AcceptQuest(context.Background(), 1, "q1")
	if err == nil || err.Error() != "locked: finish the prologue first" {
		t.Errorf("expected the accept hook to veto, got %v", err)
	}
}

// TestQuestProgressHookFilter proves a filter can scale quest progress.
func TestQuestProgressHookFilter(t *testing.T) {
	repo := newMockQuestRepository()
	charSvc := &mockCharService{char: &character.CharacterWithStats{
		Character: character.Character{ID: 1, Level: 10},
	}}
	hks := hooks.New(nil)
	hooks.AddFilter(hks, HookProgress, 10, func(_ context.Context, f QuestProgressFilter) QuestProgressFilter {
		f.Amount *= 2 // double-progress event
		return f
	})
	service := NewQuestService(repo, charSvc, &mockInventoryService{}, nil, hks)
	_ = service.CreateDefinition(context.Background(), &QuestDefinition{
		ID: "kill5", MinLevel: 1,
		Steps: []QuestStep{{Type: "KILL_MOB", Target: "slime", Count: 5}},
	})
	_ = service.AcceptQuest(context.Background(), 1, "kill5")

	if err := service.UpdateQuestProgress(context.Background(), 1, "KILL_MOB", "slime", 1); err != nil {
		t.Fatalf("progress: %v", err)
	}
	cq, _ := repo.GetCharacterQuest(context.Background(), 1, "kill5")
	if cq.Progress[0] != 2 {
		t.Errorf("expected progress doubled to 2, got %d", cq.Progress[0])
	}
}
