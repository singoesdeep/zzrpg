package system

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/singoesdeep/zzrpg/sdk/engine/store"
)

// pgLastRun persists per-(system, entity) last-run timestamps so tick systems
// resume with the real elapsed time across restarts — enabling true offline
// catch-up. Its table is (system TEXT, entity_id BIGINT, ran_at TIMESTAMPTZ,
// PRIMARY KEY(system, entity_id)).
type pgLastRun struct {
	db    store.Store
	table string
}

// NewPgLastRun returns a Postgres-backed LastRunStore.
func NewPgLastRun(db store.Store, table string) LastRunStore {
	return &pgLastRun{db: db, table: table}
}

func (s *pgLastRun) Get(ctx context.Context, system string, entityID int64) (time.Time, bool, error) {
	var t time.Time
	err := s.db.QueryRow(ctx,
		"SELECT ran_at FROM "+s.table+" WHERE system = $1 AND entity_id = $2", system, entityID).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	return t, err == nil, err
}

func (s *pgLastRun) Set(ctx context.Context, system string, entityID int64, at time.Time) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO `+s.table+` (system, entity_id, ran_at) VALUES ($1, $2, $3)
		ON CONFLICT (system, entity_id) DO UPDATE SET ran_at = EXCLUDED.ran_at`,
		system, entityID, at)
	return err
}
