package inventory

import (
	"context"
	"sync"
	"testing"

	"github.com/singoesdeep/zzrpg/backend/internal/character"
	"github.com/singoesdeep/zzrpg/backend/internal/events"
)

// TestConcurrentAddItemNoSlotCollision proves that concurrent AddItem calls for
// the same character are serialized: each item lands in a distinct slot and none
// are lost. Without the per-character lock the find-empty-slot/insert sequence
// races (two goroutines pick the same slot) and, under -race, the mock repo's
// map write is flagged. Bag capacity is 0..99, so 50 concurrent adds fit.
func TestConcurrentAddItemNoSlotCollision(t *testing.T) {
	repo := newMockInventoryRepository()
	charService := &mockCharacterService{
		char: &character.CharacterWithStats{
			Character: character.Character{ID: 1, ClassName: "WARRIOR", Level: 1},
		},
	}
	service := NewInventoryService(repo, charService, events.Global())

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = service.AddItem(context.Background(), &InventoryItem{
				CharacterID:      1,
				ItemDefinitionID: "red_potion_1",
				Quantity:         1,
				Durability:       100,
			})
		}()
	}
	wg.Wait()

	list, err := repo.ListByCharacter(context.Background(), 1)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != n {
		t.Fatalf("expected %d items, got %d (adds were lost)", n, len(list))
	}
	seen := make(map[int32]bool)
	for _, it := range list {
		if seen[it.SlotIndex] {
			t.Fatalf("duplicate slot index %d — per-character serialization failed", it.SlotIndex)
		}
		seen[it.SlotIndex] = true
	}
}
