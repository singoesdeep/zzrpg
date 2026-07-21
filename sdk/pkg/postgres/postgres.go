// Package postgres is the sdk's Postgres bootstrap: connect a pool, wrap it as
// an engine/store.Store, and apply versioned per-module SQL migrations. It has
// no built-in schema of its own — a game supplies every migration set it needs
// (gamekit's kit.MigrationSource() plus its own), so this package carries zero
// game concepts. Any project depending only on sdk (and optionally gamekit) can
// use it to stand up its database without depending on this repo's own RPG.
package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/singoesdeep/zzrpg/sdk/engine/store"
)

// DB is a connected Postgres pool plus its engine/store.Store adapter.
type DB struct {
	Pool  *pgxpool.Pool
	Store store.Store
	log   *slog.Logger
}

// Connect parses databaseURL, opens a pool (with sane pool-size defaults), and
// verifies connectivity with a ping.
func Connect(ctx context.Context, databaseURL string, log *slog.Logger) (*DB, error) {
	if log == nil {
		log = slog.Default()
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse database url: %w", err)
	}
	poolConfig.MaxConns = 25
	poolConfig.MinConns = 5
	poolConfig.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database connection ping failed: %w", err)
	}

	log.Info("connected to PostgreSQL")
	return &DB{Pool: pool, Store: store.New(pool), log: log}, nil
}

// Close releases the pool.
func (db *DB) Close() {
	if db.Pool != nil {
		db.log.Info("closing PostgreSQL connection pool")
		db.Pool.Close()
	}
}

// MigrationSet is a named group of versioned SQL migrations. Module namespaces
// the versions so independently-shipped schema (gamekit's, a plugin's, the
// game's own) never collides. FS holds files named "NNNNNN_name.up.sql" under
// Dir (default "migrations"); a Module's versions are tracked independently in
// schema_migrations(module, version).
type MigrationSet struct {
	Module string
	FS     fs.FS
	Dir    string
}

// RunMigrations applies every given migration set, in the order given, each
// tracked independently. There is no built-in schema — callers supply every
// set (see gamekit's kit.MigrationSource() for the framework's own).
func (db *DB) RunMigrations(ctx context.Context, sets ...MigrationSet) error {
	log := db.log
	if log == nil {
		log = slog.Default()
	}
	log.Info("running database migrations")

	if err := db.ensureMigrationsTable(ctx); err != nil {
		return err
	}
	for _, set := range sets {
		if err := db.applyMigrationSet(ctx, log, set); err != nil {
			return err
		}
	}

	log.Info("all database migrations completed successfully")
	return nil
}

func (db *DB) ensureMigrationsTable(ctx context.Context) error {
	const ddl = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INT NOT NULL,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS module TEXT NOT NULL DEFAULT 'core';
		DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'schema_migrations_pkey') THEN
				ALTER TABLE schema_migrations DROP CONSTRAINT schema_migrations_pkey;
			END IF;
		END $$;
		CREATE UNIQUE INDEX IF NOT EXISTS schema_migrations_module_version
			ON schema_migrations(module, version);
	`
	if _, err := db.Pool.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("failed to prepare migrations table: %w", err)
	}
	return nil
}

func (db *DB) applyMigrationSet(ctx context.Context, log *slog.Logger, set MigrationSet) error {
	dir := set.Dir
	if dir == "" {
		dir = "migrations"
	}
	module := set.Module
	if module == "" {
		return fmt.Errorf("migration set has no Module name")
	}

	entries, err := fs.ReadDir(set.FS, dir)
	if err != nil {
		return fmt.Errorf("module %q: read migrations dir: %w", module, err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, file := range files {
		parts := strings.SplitN(file, "_", 2)
		if len(parts) < 2 {
			continue
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("module %q: invalid migration filename %s: %w", module, file, err)
		}

		var exists bool
		if err := db.Pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE module = $1 AND version = $2)",
			module, version).Scan(&exists); err != nil {
			return fmt.Errorf("module %q: check migration %d: %w", module, version, err)
		}
		if exists {
			log.Debug("migration already applied", "module", module, "version", version, "file", file)
			continue
		}

		log.Info("applying migration", "module", module, "version", version, "file", file)
		content, err := fs.ReadFile(set.FS, path.Join(dir, file))
		if err != nil {
			return fmt.Errorf("module %q: read migration %s: %w", module, file, err)
		}

		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("module %q: begin migration tx: %w", module, err)
		}
		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("module %q: exec migration %s: %w", module, file, err)
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_migrations (module, version) VALUES ($1, $2)", module, version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("module %q: record migration %d: %w", module, version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("module %q: commit migration %d: %w", module, version, err)
		}
		log.Info("successfully applied migration", "module", module, "version", version)
	}
	return nil
}
