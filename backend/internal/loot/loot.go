package loot

import (
	"context"
	"errors"
)

var (
	ErrLootTableNotFound = errors.New("loot table not found")
)

type LootEntry struct {
	ItemDefinitionID string `json:"item_definition_id"` // "gold" or an item definition ID
	Rate             int32  `json:"rate"`               // e.g. 1000 = 10% (out of 10000)
	MinQuantity      int32  `json:"min"`
	MaxQuantity      int32  `json:"max"`
}

type LootTable struct {
	ID          string      `json:"id"`
	Description string      `json:"description"`
	Entries     []LootEntry `json:"entries"`
}

type DroppedItem struct {
	ItemDefinitionID string `json:"item_definition_id"`
	Quantity         int32  `json:"quantity"`
}

type LootRepository interface {
	CreateLootTable(ctx context.Context, lt *LootTable) error
	GetLootTable(ctx context.Context, id string) (*LootTable, error)
	ListLootTables(ctx context.Context, limit, offset int) ([]LootTable, error)
}
