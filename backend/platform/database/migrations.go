package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MigrationSet is a named group of versioned SQL migrations. Module namespaces
// the versions so plugins can ship their own schema without colliding with the
// core versions or each other. FS holds files named "NNNNNN_name.up.sql" under
// Dir (default "migrations").
type MigrationSet struct {
	Module string
	FS     fs.FS
	Dir    string
}

// RunMigrations applies the engine's own core migrations plus any extra
// per-module sets (e.g. contributed by plugins), each tracked independently in
// schema_migrations keyed by (module, version).
func (db *DB) RunMigrations(ctx context.Context, extra ...MigrationSet) error {
	log := db.log
	if log == nil {
		log = slog.Default()
	}
	log.Info("Running database migrations...")

	if err := db.ensureMigrationsTable(ctx); err != nil {
		return err
	}

	sets := append([]MigrationSet{{Module: "core", FS: migrationsFS, Dir: "migrations"}}, extra...)
	for _, set := range sets {
		if err := db.applyMigrationSet(ctx, log, set); err != nil {
			return err
		}
	}

	log.Info("All database migrations completed successfully")
	return nil
}

// ensureMigrationsTable creates or upgrades schema_migrations to be keyed by
// (module, version). It tolerates the legacy single-column (version) primary key
// from before module namespacing existed.
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
		module = "core"
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
			log.Debug("Migration already applied", "module", module, "version", version, "file", file)
			continue
		}

		log.Info("Applying migration", "module", module, "version", version, "file", file)
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
		log.Info("Successfully applied migration", "module", module, "version", version)
	}
	return nil
}
