package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/singoesdeep/zzrpg/backend/engine/store"
	"github.com/singoesdeep/zzrpg/backend/pkg/config"
)

type DB struct {
	Pool  *pgxpool.Pool
	Store store.Store
	log   *slog.Logger
}

func NewConnectionPool(cfg *config.Config, log *slog.Logger) (*DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Info("Connecting to PostgreSQL", "url", cfg.DatabaseURL)
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse database url: %w", err)
	}

	// Adjust pool settings
	poolConfig.MaxConns = 25
	poolConfig.MinConns = 5
	poolConfig.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("database connection ping failed: %w", err)
	}

	log.Info("Successfully connected to PostgreSQL")
	return &DB{
		Pool:  pool,
		Store: store.New(pool),
		log:   log,
	}, nil
}

func (db *DB) Close() {
	if db.Pool != nil {
		db.log.Info("Closing PostgreSQL connection pool...")
		db.Pool.Close()
	}
}
