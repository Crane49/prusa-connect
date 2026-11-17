package devicestore

import (
	"embed"
	"io/fs"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationFS embed.FS

// Migrations is the schema of the device records, the prints and the
// registrations. The host migrates it after internal/authz's, because the
// records and the prints reference authz_device.
func Migrations() sqlstore.Migrations {
	files, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		// The directory is the embed pattern above, so it is always there.
		panic(err)
	}

	return sqlstore.Migrations{Owner: "devicestore", Files: files}
}
