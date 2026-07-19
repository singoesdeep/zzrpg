package inventory

import (
	"context"
	"sync"
	"testing"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
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
	service := NewInventoryService(repo, charService, bus.NewInProc(nil))

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

// TestKeyedMutexEvictsIdleEntries proves the per-key lock map does not grow
// unbounded: after all lock/unlock pairs complete (even under contention), no
// entry remains for any key.
func TestKeyedMutexEvictsIdleEntries(t *testing.T) {
	k := newKeyedMutex()

	// Sequential: lock+unlock a key, entry must be gone afterwards.
	k.lock(1)()
	k.mu.Lock()
	if _, ok := k.locks[1]; ok {
		t.Errorf("entry for key 1 was not evicted after unlock")
	}
	k.mu.Unlock()

	// Concurrent contention on several keys; the map must be empty at the end.
	var wg sync.WaitGroup
	for _, key := range []int32{1, 2, 3} {
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(key int32) {
				defer wg.Done()
				unlock := k.lock(key)
				unlock()
			}(key)
		}
	}
	wg.Wait()

	k.mu.Lock()
	if n := len(k.locks); n != 0 {
		t.Errorf("expected empty lock map after all releases, got %d entries", n)
	}
	k.mu.Unlock()
}
