package instanceid

import (
	"embed"
	"io/fs"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

//go:embed migrations/postgres/*.sql migrations/sqlite/*.sql
var migrationFS embed.FS

// Migrations is the schema of the one row that holds the id.
func Migrations() sqlstore.Migrations {
	files, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		// The directory is the embed pattern above, so it is always there.
		panic(err)
	}

	return sqlstore.Migrations{Owner: "instanceid", Files: files}
}
