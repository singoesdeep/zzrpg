package loot

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

type LootService interface {
	CreateLootTable(ctx context.Context, lt *LootTable) error
	RollLoot(ctx context.Context, tableID string) ([]DroppedItem, error)
	ListLootTables(ctx context.Context) ([]LootTable, error)
}

type lootService struct {
	repo LootRepository
	// rand is a *math/rand.Rand, which is NOT safe for concurrent use. RollLoot
	// is called concurrently (one goroutine per combat/offline roll), so every
	// access is serialized by randMu.
	randMu sync.Mutex
	rand   *rand.Rand
}

func NewLootService(repo LootRepository) LootService {
	return &lootService{
		repo: repo,
		rand: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *lootService) CreateLootTable(ctx context.Context, lt *LootTable) error {
	return s.repo.CreateLootTable(ctx, lt)
}

func (s *lootService) ListLootTables(ctx context.Context) ([]LootTable, error) {
	return s.repo.ListLootTables(ctx)
}

func (s *lootService) RollLoot(ctx context.Context, tableID string) ([]DroppedItem, error) {
	lt, err := s.repo.GetLootTable(ctx, tableID)
	if err != nil {
		// Mock fallback if table not found in database (e.g. for default testing dummy)
		if tableID == "dummy_drops" {
			return []DroppedItem{
				{ItemDefinitionID: "gold", Quantity: int32(s.rollIntn(41) + 10)}, // 10..50 gold
				{ItemDefinitionID: "dragon_sword_0", Quantity: 1},                // 100% rate fallback sword
			}, nil
		}
		return nil, err
	}

	var drops []DroppedItem
	for _, entry := range lt.Entries {
		roll := s.roll31n(10000)
		if roll < entry.Rate {
			qty := entry.MinQuantity
			if entry.MaxQuantity > entry.MinQuantity {
				qty = entry.MinQuantity + s.roll31n(entry.MaxQuantity-entry.MinQuantity+1)
			}
			drops = append(drops, DroppedItem{
				ItemDefinitionID: entry.ItemDefinitionID,
				Quantity:         qty,
			})
		}
	}

	return drops, nil
}

// roll31n / rollIntn serialize access to the non-thread-safe *rand.Rand.
func (s *lootService) roll31n(n int32) int32 {
	s.randMu.Lock()
	defer s.randMu.Unlock()
	return s.rand.Int31n(n)
}

func (s *lootService) rollIntn(n int) int {
	s.randMu.Lock()
	defer s.randMu.Unlock()
	return s.rand.Intn(n)
}
