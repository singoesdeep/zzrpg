package items

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/singoesdeep/zzrpg/backend/engine/store"
)

type pgItemRepository struct {
	db store.Store
}

func NewItemRepository(db store.Store) ItemRepository {
	return &pgItemRepository{db: db}
}

func (r *pgItemRepository) Create(ctx context.Context, item *ItemDefinition) error {
	modsJSON, err := json.Marshal(item.StatsModifiers)
	if err != nil {
		return err
	}
	metaJSON, err := json.Marshal(item.Metadata)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO item_definitions (id, name, description, slot_type, min_level, class_restrictions, stats_modifiers, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at
	`
	err = r.db.QueryRow(ctx, query, item.ID, item.Name, item.Description, item.SlotType, item.MinLevel, item.ClassRestrictions, modsJSON, metaJSON).
		Scan(&item.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrItemAlreadyExists
		}
		return err
	}
	return nil
}

func (r *pgItemRepository) Update(ctx context.Context, item *ItemDefinition) error {
	modsJSON, err := json.Marshal(item.StatsModifiers)
	if err != nil {
		return err
	}
	metaJSON, err := json.Marshal(item.Metadata)
	if err != nil {
		return err
	}

	query := `
		UPDATE item_definitions
		SET name = $1, description = $2, slot_type = $3, min_level = $4, class_restrictions = $5, stats_modifiers = $6, metadata = $7
		WHERE id = $8
	`
	res, err := r.db.Exec(ctx, query, item.Name, item.Description, item.SlotType, item.MinLevel, item.ClassRestrictions, modsJSON, metaJSON, item.ID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrItemNotFound
	}
	return nil
}

func (r *pgItemRepository) GetByID(ctx context.Context, id string) (*ItemDefinition, error) {
	query := `
		SELECT id, name, description, slot_type, min_level, class_restrictions, stats_modifiers, metadata, created_at
		FROM item_definitions
		WHERE id = $1
	`
	var item ItemDefinition
	var modsBytes, metaBytes []byte

	err := r.db.QueryRow(ctx, query, id).Scan(
		&item.ID, &item.Name, &item.Description, &item.SlotType, &item.MinLevel, &item.ClassRestrictions, &modsBytes, &metaBytes, &item.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrItemNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(modsBytes, &item.StatsModifiers); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(metaBytes, &item.Metadata); err != nil {
		return nil, err
	}

	return &item, nil
}

func (r *pgItemRepository) List(ctx context.Context, limit, offset int) ([]ItemDefinition, error) {
	query := `
		SELECT id, name, description, slot_type, min_level, class_restrictions, stats_modifiers, metadata, created_at
		FROM item_definitions
		ORDER BY id ASC
		LIMIT $1 OFFSET $2
	`
	rows, err := r.db.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ItemDefinition
	for rows.Next() {
		var item ItemDefinition
		var modsBytes, metaBytes []byte
		err := rows.Scan(
			&item.ID, &item.Name, &item.Description, &item.SlotType, &item.MinLevel, &item.ClassRestrictions, &modsBytes, &metaBytes, &item.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(modsBytes, &item.StatsModifiers); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metaBytes, &item.Metadata); err != nil {
			return nil, err
		}

		list = append(list, item)
	}

	return list, nil
}

func (r *pgItemRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM item_definitions WHERE id = $1`
	res, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrItemNotFound
	}
	return nil
}
