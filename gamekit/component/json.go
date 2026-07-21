package component

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/singoesdeep/zzrpg/sdk/engine/store"
)

// jsonStore is a generic Postgres component store that serialises T to a JSONB
// column. Its table must be (entity_id BIGINT PRIMARY KEY, data JSONB NOT NULL);
// see SchemaFor. It is the convenient default for simple components; a component
// with its own queryable columns should implement Store[T] with a relational
// table instead.
type jsonStore[T any] struct {
	name  string
	table string
	db    store.Store
}

// NewJSONStore returns a JSONB-backed component store. The table is created by a
// migration (see SchemaFor); name is the component's registry name.
func NewJSONStore[T any](db store.Store, name, table string) Store[T] {
	return &jsonStore[T]{name: name, table: table, db: db}
}

// SchemaFor returns the CREATE TABLE statement for a JSONB component table, for
// a plugin to embed in a migration.
func SchemaFor(table string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    entity_id BIGINT PRIMARY KEY REFERENCES entities(id) ON DELETE CASCADE,
    data      JSONB NOT NULL DEFAULT '{}'
);`, table)
}

func (s *jsonStore[T]) Name() string { return s.name }

func (s *jsonStore[T]) Get(ctx context.Context, entityID int64) (T, bool, error) {
	var zero T
	var raw []byte
	err := s.db.QueryRow(ctx, fmt.Sprintf(`SELECT data FROM %s WHERE entity_id = $1`, s.table), entityID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, err
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return zero, false, err
	}
	return v, true, nil
}

func (s *jsonStore[T]) Set(ctx context.Context, entityID int64, v T) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (entity_id, data) VALUES ($1, $2)
		ON CONFLICT (entity_id) DO UPDATE SET data = EXCLUDED.data`, s.table), entityID, raw)
	return err
}

func (s *jsonStore[T]) Delete(ctx context.Context, entityID int64) error {
	_, err := s.db.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE entity_id = $1`, s.table), entityID)
	return err
}

func (s *jsonStore[T]) Has(ctx context.Context, entityID int64) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s WHERE entity_id = $1)`, s.table), entityID).Scan(&ok)
	return ok, err
}

func (s *jsonStore[T]) EntityIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.db.Query(ctx, fmt.Sprintf(`SELECT entity_id FROM %s ORDER BY entity_id`, s.table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
