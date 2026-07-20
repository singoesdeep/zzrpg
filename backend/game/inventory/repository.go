package inventory

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/singoesdeep/zzrpg/backend/engine/store"
	"github.com/singoesdeep/zzrpg/backend/game/items"
)

type pgInventoryRepository struct {
	db store.Store
}

func NewInventoryRepository(db store.Store) InventoryRepository {
	return &pgInventoryRepository{db: db}
}

func (r *pgInventoryRepository) GetBySlot(ctx context.Context, charID int32, slot int32) (*InventoryItem, error) {
	query := `
		SELECT i.id, i.character_id, i.slot_index, i.item_definition_id, i.quantity, i.durability, i.custom_modifiers, i.created_at, i.updated_at,
		       d.name, d.description, d.slot_type, d.min_level, d.class_restrictions, d.stats_modifiers, d.metadata
		FROM inventories i
		JOIN item_definitions d ON i.item_definition_id = d.id
		WHERE i.character_id = $1 AND i.slot_index = $2
	`
	var item InventoryItem
	item.ItemDetails = &items.ItemDefinition{}
	var customModsBytes, statsModsBytes, metaBytes []byte

	err := r.db.QueryRow(ctx, query, charID, slot).Scan(
		&item.ID, &item.CharacterID, &item.SlotIndex, &item.ItemDefinitionID, &item.Quantity, &item.Durability, &customModsBytes, &item.CreatedAt, &item.UpdatedAt,
		&item.ItemDetails.Name, &item.ItemDetails.Description, &item.ItemDetails.SlotType, &item.ItemDetails.MinLevel, &item.ItemDetails.ClassRestrictions, &statsModsBytes, &metaBytes,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrItemNotFound
		}
		return nil, err
	}

	item.ItemDetails.ID = item.ItemDefinitionID
	if err := json.Unmarshal(customModsBytes, &item.CustomModifiers); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(statsModsBytes, &item.ItemDetails.StatsModifiers); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(metaBytes, &item.ItemDetails.Metadata); err != nil {
		return nil, err
	}

	return &item, nil
}

func (r *pgInventoryRepository) ListByCharacter(ctx context.Context, charID int32) ([]InventoryItem, error) {
	query := `
		SELECT i.id, i.character_id, i.slot_index, i.item_definition_id, i.quantity, i.durability, i.custom_modifiers, i.created_at, i.updated_at,
		       d.name, d.description, d.slot_type, d.min_level, d.class_restrictions, d.stats_modifiers, d.metadata
		FROM inventories i
		JOIN item_definitions d ON i.item_definition_id = d.id
		WHERE i.character_id = $1
		ORDER BY i.slot_index ASC
	`
	rows, err := r.db.Query(ctx, query, charID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var itemsList []InventoryItem
	for rows.Next() {
		var item InventoryItem
		item.ItemDetails = &items.ItemDefinition{}
		var customModsBytes, statsModsBytes, metaBytes []byte

		err := rows.Scan(
			&item.ID, &item.CharacterID, &item.SlotIndex, &item.ItemDefinitionID, &item.Quantity, &item.Durability, &customModsBytes, &item.CreatedAt, &item.UpdatedAt,
			&item.ItemDetails.Name, &item.ItemDetails.Description, &item.ItemDetails.SlotType, &item.ItemDetails.MinLevel, &item.ItemDetails.ClassRestrictions, &statsModsBytes, &metaBytes,
		)
		if err != nil {
			return nil, err
		}

		item.ItemDetails.ID = item.ItemDefinitionID
		if err := json.Unmarshal(customModsBytes, &item.CustomModifiers); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(statsModsBytes, &item.ItemDetails.StatsModifiers); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metaBytes, &item.ItemDetails.Metadata); err != nil {
			return nil, err
		}

		itemsList = append(itemsList, item)
	}

	return itemsList, nil
}

func (r *pgInventoryRepository) Move(ctx context.Context, charID int32, fromSlot, toSlot int32) error {
	query := `
		UPDATE inventories
		SET slot_index = $1, updated_at = NOW()
		WHERE character_id = $2 AND slot_index = $3
	`
	res, err := r.db.Exec(ctx, query, toSlot, charID, fromSlot)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrItemNotFound
	}
	return nil
}

func (r *pgInventoryRepository) Swap(ctx context.Context, charID int32, slotA, slotB int32) error {
	return r.db.WithinTx(ctx, func(q store.Querier) error {
		// Temporal helper slot (-99) to avoid unique key violation during swap
		_, err := q.Exec(ctx, "UPDATE inventories SET slot_index = -99 WHERE character_id = $1 AND slot_index = $2", charID, slotA)
		if err != nil {
			return err
		}

		_, err = q.Exec(ctx, "UPDATE inventories SET slot_index = $1 WHERE character_id = $2 AND slot_index = $3", slotA, charID, slotB)
		if err != nil {
			return err
		}

		_, err = q.Exec(ctx, "UPDATE inventories SET slot_index = $1 WHERE character_id = $2 AND slot_index = -99", slotB, charID)
		return err
	})
}

func (r *pgInventoryRepository) AddItem(ctx context.Context, item *InventoryItem) error {
	modsJSON, err := json.Marshal(item.CustomModifiers)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO inventories (character_id, slot_index, item_definition_id, quantity, durability, custom_modifiers)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`
	err = r.db.QueryRow(ctx, query, item.CharacterID, item.SlotIndex, item.ItemDefinitionID, item.Quantity, item.Durability, modsJSON).
		Scan(&item.ID, &item.CreatedAt, &item.UpdatedAt)
	return err
}

func (r *pgInventoryRepository) RemoveItem(ctx context.Context, id int64) error {
	query := `DELETE FROM inventories WHERE id = $1`
	res, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrItemNotFound
	}
	return nil
}
