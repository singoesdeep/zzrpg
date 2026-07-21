package entity

import (
	"embed"
	"io/fs"

	"github.com/singoesdeep/zzrpg/sdk/engine/plugin"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MigrationSource ships the entities-table schema under the "gamekit_entity"
// module, so a game registers it via a plugin.Migrator without touching the
// framework.
func MigrationSource() plugin.MigrationSource {
	return plugin.MigrationSource{Module: "gamekit_entity", FS: fs.FS(migrationsFS), Dir: "migrations"}
}
