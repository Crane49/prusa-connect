// Package hostschema is the schema of the host's own database: every package
// that keeps tables there, in the order they are migrated, and the
// compatibility floor over all of them.
//
// Each owner keeps its migrations beside the Go code that reads and writes its
// tables. They share one database, one ledger and one migration lock, because
// the device tables reference the device directory and the print history is
// filtered by the access projection in one statement.
package hostschema

import (
	"database/sql"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
	"github.com/prusa3d/prusa-connect/internal/platform/auth/dbstore"
	"github.com/prusa3d/prusa-connect/internal/platform/dashboards"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/platform/instanceid"
)

// compat is the floor as a count of migrations. A build older than dated
// migration names reads this count. It stays at the count of the numbered
// files. A new migration an older build cannot survive is named in
// Schema.Floor instead.
const compat = 9

// Schema is the host's schema. authz comes first: a table that references the
// device directory is migrated after it.
func Schema() sqlstore.Schema {
	return sqlstore.Schema{
		Owners: []sqlstore.Migrations{
			authz.Migrations(),
			devicestore.Migrations(),
			dbstore.Migrations(),
			dashboards.Migrations(),
			instanceid.Migrations(),
		},
		Compat: compat,
	}
}

// Migrator is the host's schema over one database. version is what this build
// calls itself, and is recorded against every migration it applies.
func Migrator(db *sql.DB, engine sqlstore.Engine, version string) (*sqlstore.Migrator, error) {
	return sqlstore.NewMigrator(db, engine, Schema(), version)
}
