// Package store is a thin persistence seam over pgx. Repositories depend on the
// Store/Querier interfaces here rather than a concrete *pgxpool.Pool, which
// decouples them from the connection type, lets a single repository method run
// either standalone or inside a transaction, and makes repositories fakeable in
// tests. It stays deliberately pgx-flavoured (pgx.Rows/pgx.Row leak through) —
// the goal is composition + testability, not hiding the driver entirely.
package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier is the subset of pgx used by repositories. Both *pgxpool.Pool and
// pgx.Tx satisfy it, so a repository method written against a Querier works
// unchanged whether it runs on the pool or on a transaction.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Store is a Querier plus transactional composition. Single-statement repository
// methods use the embedded Querier directly; multi-statement methods run inside
// WithinTx so their writes commit atomically.
type Store interface {
	Querier
	// WithinTx runs fn inside a database transaction, passing a tx-scoped
	// Querier. It commits if fn returns nil, otherwise rolls back. The
	// transaction is always finalised (a rollback after a successful commit is a
	// harmless no-op).
	WithinTx(ctx context.Context, fn func(q Querier) error) error
}

// pgxStore is the pgx-backed Store.
type pgxStore struct {
	pool *pgxpool.Pool
}

// New wraps a pgx connection pool as a Store.
func New(pool *pgxpool.Pool) Store {
	return &pgxStore{pool: pool}
}

func (s *pgxStore) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return s.pool.Query(ctx, sql, args...)
}

func (s *pgxStore) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return s.pool.QueryRow(ctx, sql, args...)
}

func (s *pgxStore) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return s.pool.Exec(ctx, sql, args...)
}

func (s *pgxStore) WithinTx(ctx context.Context, fn func(q Querier) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // no-op once committed

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
