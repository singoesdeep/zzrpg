package relation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/singoesdeep/zzrpg/sdk/engine/store"
)

// pgRepo is the Postgres-backed Repo over the sdk store seam. Edges live in a
// single table (from_id, edge_type, to_id) with the triple as primary key.
type pgRepo struct {
	db    store.Store
	table string
}

// NewPgRepo returns a Postgres relation Repo over the given table.
func NewPgRepo(db store.Store, table string) Repo { return &pgRepo{db: db, table: table} }

func (r *pgRepo) Link(ctx context.Context, from int64, t string, to int64) error {
	_, err := r.db.Exec(ctx,
		fmt.Sprintf(`INSERT INTO %s (from_id, edge_type, to_id) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, r.table),
		from, t, to)
	return err
}

func (r *pgRepo) Unlink(ctx context.Context, from int64, t string, to int64) error {
	_, err := r.db.Exec(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE from_id=$1 AND edge_type=$2 AND to_id=$3`, r.table),
		from, t, to)
	return err
}

func (r *pgRepo) To(ctx context.Context, from int64, t string) ([]int64, error) {
	return r.ids(ctx,
		fmt.Sprintf(`SELECT to_id FROM %s WHERE from_id=$1 AND edge_type=$2 ORDER BY to_id`, r.table),
		from, t)
}

func (r *pgRepo) From(ctx context.Context, to int64, t string) ([]int64, error) {
	return r.ids(ctx,
		fmt.Sprintf(`SELECT from_id FROM %s WHERE to_id=$1 AND edge_type=$2 ORDER BY from_id`, r.table),
		to, t)
}

func (r *pgRepo) Exists(ctx context.Context, from int64, t string, to int64) (bool, error) {
	var one int
	err := r.db.QueryRow(ctx,
		fmt.Sprintf(`SELECT 1 FROM %s WHERE from_id=$1 AND edge_type=$2 AND to_id=$3`, r.table),
		from, t, to).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *pgRepo) ids(ctx context.Context, sql string, args ...any) ([]int64, error) {
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
