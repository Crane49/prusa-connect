package dashboards

import (
	"embed"
	"io/fs"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// The schema is compiled into every build, the nodashboards one too, so every
// build migrates one database to the same schema: a database a full build wrote
// opens under a build without the dashboards, and the other way round.
//
//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationFS embed.FS

// Migrations is the schema of the dashboards and the sequence their ids come
// from.
func Migrations() sqlstore.Migrations {
	files, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		// The directory is the embed pattern above, so it is always there.
		panic(err)
	}

	return sqlstore.Migrations{Owner: "dashboards", Files: files}
}
