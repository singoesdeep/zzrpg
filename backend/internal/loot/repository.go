package loot

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/singoesdeep/zzrpg/backend/engine/store"
)

type pgLootRepository struct {
	db store.Store
}

func NewLootRepository(db store.Store) LootRepository {
	return &pgLootRepository{db: db}
}

func (r *pgLootRepository) CreateLootTable(ctx context.Context, lt *LootTable) error {
	entriesJSON, err := json.Marshal(lt.Entries)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO loot_tables (id, description, entries)
		VALUES ($1, $2, $3)
	`
	_, err = r.db.Exec(ctx, query, lt.ID, lt.Description, entriesJSON)
	return err
}

func (r *pgLootRepository) GetLootTable(ctx context.Context, id string) (*LootTable, error) {
	query := `
		SELECT id, description, entries
		FROM loot_tables
		WHERE id = $1
	`
	var lt LootTable
	var entriesBytes []byte

	err := r.db.QueryRow(ctx, query, id).Scan(&lt.ID, &lt.Description, &entriesBytes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLootTableNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(entriesBytes, &lt.Entries); err != nil {
		return nil, err
	}

	return &lt, nil
}

func (r *pgLootRepository) ListLootTables(ctx context.Context, limit, offset int) ([]LootTable, error) {
	query := `
		SELECT id, description, entries
		FROM loot_tables
		ORDER BY id ASC
		LIMIT $1 OFFSET $2
	`
	rows, err := r.db.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []LootTable
	for rows.Next() {
		var lt LootTable
		var entriesBytes []byte

		err := rows.Scan(&lt.ID, &lt.Description, &entriesBytes)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(entriesBytes, &lt.Entries); err != nil {
			return nil, err
		}

		list = append(list, lt)
	}

	return list, nil
}
