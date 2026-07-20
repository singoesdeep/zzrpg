package character

import (
	"context"
	"testing"

	"github.com/singoesdeep/zzrpg/backend/engine/hooks"
)

type mockCharacterRepository struct {
	characters map[int64]*CharacterWithStats
	names      map[string]*Character
}

func newMockCharacterRepository() *mockCharacterRepository {
	return &mockCharacterRepository{
		characters: make(map[int64]*CharacterWithStats),
		names:      make(map[string]*Character),
	}
}

func (m *mockCharacterRepository) Create(ctx context.Context, char *Character, baseStats map[string]float64) error {
	if _, ok := m.names[char.Name]; ok {
		return ErrCharacterNameTaken
	}

	// Enforce artificial limit in mock (max 4)
	count := 0
	for _, c := range m.characters {
		if c.UserID == char.UserID {
			count++
		}
	}
	if count >= 4 {
		return ErrCharacterLimitReached
	}

	char.ID = int64(len(m.characters) + 1)
	cws := &CharacterWithStats{
		Character: *char,
		Stats: CharacterStats{
			CharacterID: char.ID,
			BaseStats:   baseStats,
			DerivedStats: map[string]float64{
				"HP":      baseStats["CON"] * 15,
				"MP":      baseStats["INT"] * 10,
				"ATTACK":  baseStats["STR"] * 2,
				"DEFENSE": baseStats["CON"] * 1,
			},
		},
	}
	m.characters[char.ID] = cws
	m.names[char.Name] = char
	return nil
}

func (m *mockCharacterRepository) GetByID(ctx context.Context, id int64) (*CharacterWithStats, error) {
	c, ok := m.characters[id]
	if !ok {
		return nil, ErrCharacterNotFound
	}
	return c, nil
}

func (m *mockCharacterRepository) GetByName(ctx context.Context, name string) (*Character, error) {
	c, ok := m.names[name]
	if !ok {
		return nil, ErrCharacterNotFound
	}
	return c, nil
}

func (m *mockCharacterRepository) ListByUserID(ctx context.Context, userID int64) ([]Character, error) {
	var chars []Character
	for _, c := range m.characters {
		if c.UserID == userID {
			chars = append(chars, c.Character)
		}
	}
	return chars, nil
}

func (m *mockCharacterRepository) UpdateStats(ctx context.Context, charID int64, derivedStats map[string]float64) error {
	c, ok := m.characters[charID]
	if !ok {
		return ErrCharacterNotFound
	}
	c.Stats.DerivedStats = derivedStats
	return nil
}

func (m *mockCharacterRepository) UpdateLastActive(ctx context.Context, charID int64) error {
	return nil
}

func (m *mockCharacterRepository) AddRewards(ctx context.Context, charID int64, gold int64, exp int64) (bool, int32, error) {
	c, ok := m.characters[charID]
	if !ok {
		return false, 0, ErrCharacterNotFound
	}
	c.Character.Gold += gold
	c.Character.Experience += exp
	return false, c.Character.Level, nil
}

func TestCreateCharacter(t *testing.T) {
	repo := newMockCharacterRepository()
	service := NewCharacterService(repo, nil, nil, nil, nil)

	// 1. Success case: Warrior
	char, err := service.Create(context.Background(), 100, "WarriorGod", "WARRIOR")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if char.Name != "WarriorGod" || char.ClassName != "WARRIOR" {
		t.Errorf("unexpected character details: %+v", char)
	}

	// Verify Warrior base stats
	if char.Stats.BaseStats["STR"] != 15 || char.Stats.BaseStats["CON"] != 15 {
		t.Errorf("unexpected base stats: %+v", char.Stats.BaseStats)
	}

	// Verify Warrior derived stats (HP should be CON * 15 = 225)
	if char.Stats.DerivedStats["HP"] != 225 {
		t.Errorf("unexpected HP: %f", char.Stats.DerivedStats["HP"])
	}

	// 2. Failure: Duplicate Name
	_, err = service.Create(context.Background(), 100, "WarriorGod", "MAGE")
	if err != ErrCharacterNameTaken {
		t.Errorf("expected ErrCharacterNameTaken, got %v", err)
	}

	// 3. Failure: Invalid Class
	_, err = service.Create(context.Background(), 100, "InvalidHero", "ARCHER")
	if err != ErrInvalidClass {
		t.Errorf("expected ErrInvalidClass, got %v", err)
	}

	// 4. Failure: Name too short
	_, err = service.Create(context.Background(), 100, "ab", "WARRIOR")
	if err != ErrNameTooShort {
		t.Errorf("expected ErrNameTooShort, got %v", err)
	}

	// 5. Failure: Name too long
	_, err = service.Create(context.Background(), 100, "namesuperlongfortherpgcharacter", "WARRIOR")
	if err != ErrNameTooLong {
		t.Errorf("expected ErrNameTooLong, got %v", err)
	}
}

func TestCharacterLimit(t *testing.T) {
	repo := newMockCharacterRepository()
	service := NewCharacterService(repo, nil, nil, nil, nil)

	// Create 4 characters successfully
	for i := 1; i <= 4; i++ {
		name := "Hero" + string(rune(48+i)) // Hero1, Hero2...
		_, err := service.Create(context.Background(), 100, name, "WARRIOR")
		if err != nil {
			t.Fatalf("expected no error on character %d creation, got %v", i, err)
		}
	}

	// 5th character should fail due to limit
	_, err := service.Create(context.Background(), 100, "Hero5", "MAGE")
	if err != ErrCharacterLimitReached {
		t.Errorf("expected ErrCharacterLimitReached, got %v", err)
	}
}

// RewardsGranted / CharacterLeveledUp are now emitted transactionally through the
// outbox inside repo.AddRewards (see engine/outbox tests for the dispatch path
// and the live-Postgres integration test for the end-to-end atomic guarantee),
// so there is no direct-publish unit test for them here.

// TestRewardsHookFilter proves a plugin filter can adjust rewards before they are
// applied: a HookRewards filter that doubles gold means the repo receives the
// doubled amount.
func TestRewardsHookFilter(t *testing.T) {
	repo := newMockCharacterRepository()
	hks := hooks.New(nil)
	hooks.AddFilter(hks, HookRewards, 10, func(_ context.Context, r RewardsFilter) RewardsFilter {
		r.Gold *= 2 // a "double gold weekend" plugin
		return r
	})
	service := NewCharacterService(repo, nil, nil, nil, hks)

	char, err := service.Create(context.Background(), 1, "RewardHero", "WARRIOR")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := service.AddRewards(context.Background(), char.ID, 100, 50); err != nil {
		t.Fatalf("add rewards: %v", err)
	}

	// The mock repo accumulated the filtered gold (100*2) and unfiltered exp.
	got := repo.characters[char.ID]
	if got.Character.Gold != 200 {
		t.Errorf("expected doubled gold 200 to reach the repo, got %d", got.Character.Gold)
	}
	if got.Character.Experience != 50 {
		t.Errorf("expected exp 50 unchanged, got %d", got.Character.Experience)
	}
}
