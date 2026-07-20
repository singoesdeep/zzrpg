package database

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func (db *DB) RunMigrations(ctx context.Context) error {
	// Nil-safe logger so callers that construct a bare DB (e.g. tests) can run
	// migrations without wiring a logger.
	log := db.log
	if log == nil {
		log = slog.Default()
	}
	log.Info("Running database migrations...")

	// 1. Create migrations table if not exists
	_, err := db.Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INT PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// 2. Read migration files
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations dir: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)

	// 3. Apply migrations
	for _, file := range files {
		// Parse version from filename (e.g. "000001_create_users_table.up.sql" -> 1)
		parts := strings.SplitN(file, "_", 2)
		if len(parts) < 2 {
			continue
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid migration filename format %s: %w", file, err)
		}

		// Check if migration is already applied
		var exists bool
		err = db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check migration state for version %d: %w", version, err)
		}

		if exists {
			log.Debug("Migration already applied", "version", version, "file", file)
			continue
		}

		log.Info("Applying migration", "version", version, "file", file)

		content, err := migrationsFS.ReadFile(filepath.Join("migrations", file))
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", file, err)
		}

		// Execute migration
		tx, err := db.Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to start migration transaction: %w", err)
		}
		defer tx.Rollback(ctx)

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			return fmt.Errorf("failed to execute migration content for %s: %w", file, err)
		}

		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			return fmt.Errorf("failed to log migration version %d: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit migration transaction: %w", err)
		}

		log.Info("Successfully applied migration", "version", version)
	}

	log.Info("All database migrations completed successfully")
	return nil
}
