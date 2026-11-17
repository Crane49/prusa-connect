package authz

import (
	"embed"
	"io/fs"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationFS embed.FS

// Migrations is the schema of the authorization graph and the device directory.
// The host migrates it before every other owner's, because their tables
// reference authz_device.
func Migrations() sqlstore.Migrations {
	files, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		// The directory is the embed pattern above, so it is always there.
		panic(err)
	}

	return sqlstore.Migrations{Owner: "authz", Files: files}
}
