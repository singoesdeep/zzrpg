package entity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/singoesdeep/zzrpg/sdk/engine/store"
)

// pgRepo is the Postgres-backed Repo over the sdk store seam.
type pgRepo struct{ db store.Store }

// NewPgRepo returns a Postgres entity Repo.
func NewPgRepo(db store.Store) Repo { return &pgRepo{db: db} }

func (r *pgRepo) Create(ctx context.Context, kind string, ownerID int64) (Entity, error) {
	e := Entity{Kind: kind, OwnerID: ownerID}
	err := r.db.QueryRow(ctx,
		`INSERT INTO entities (kind, owner_id) VALUES ($1, $2) RETURNING id, created_at`,
		kind, ownerID).Scan(&e.ID, &e.CreatedAt)
	return e, err
}

func (r *pgRepo) Get(ctx context.Context, id int64) (Entity, error) {
	var e Entity
	err := r.db.QueryRow(ctx,
		`SELECT id, kind, owner_id, created_at FROM entities WHERE id = $1`, id).
		Scan(&e.ID, &e.Kind, &e.OwnerID, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entity{}, ErrNotFound{ID: id}
	}
	return e, err
}

func (r *pgRepo) ListByOwner(ctx context.Context, ownerID int64) ([]Entity, error) {
	return r.list(ctx, `SELECT id, kind, owner_id, created_at FROM entities WHERE owner_id = $1 ORDER BY id`, ownerID)
}

func (r *pgRepo) ListByKind(ctx context.Context, kind string) ([]Entity, error) {
	return r.list(ctx, `SELECT id, kind, owner_id, created_at FROM entities WHERE kind = $1 ORDER BY id`, kind)
}

func (r *pgRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.Exec(ctx, `DELETE FROM entities WHERE id = $1`, id)
	return err
}

func (r *pgRepo) list(ctx context.Context, sql string, arg any) ([]Entity, error) {
	rows, err := r.db.Query(ctx, sql, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entity
	for rows.Next() {
		var e Entity
		if err := rows.Scan(&e.ID, &e.Kind, &e.OwnerID, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
