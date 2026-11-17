package dbstore

import (
	"embed"
	"io/fs"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationFS embed.FS

// Migrations is the schema of the accounts, the credentials, the pairings and
// the subject generations.
func Migrations() sqlstore.Migrations {
	files, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		// The directory is the embed pattern above, so it is always there.
		panic(err)
	}

	return sqlstore.Migrations{Owner: "dbstore", Files: files}
}
