package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// A module's own database: the request, where the operator puts it, and the
// grant the declaration records.

// databaseRequest is the request line of a manifest that asks for a database
// of its own, in the words requests and approved both use.
const databaseRequest = "database of its own"

// The places an operator may put a module's database.
const (
	databaseModeSQLite    = "sqlite"
	databaseModePostgres  = "postgres"
	databaseModeProvision = "provision"
)

// databaseChoice is where the operator asked an install to put the module's
// database: a mode, and the connection string for mode postgres.
type databaseChoice struct {
	mode string
	dsn  string
}

// databaseDecision is what an install grants for the module's database: the
// grant the declaration records, or a database the server has to make first.
type databaseDecision struct {
	granted   *install.Database
	provision bool
}

// decideDatabase is the database an approved install grants. No choice on an
// update keeps what the declaration grants, and no choice on a first install
// takes back the database the same module left behind, or is a SQLite file.
// A choice is refused without approve, for a manifest that asks for no
// database, and for provision on a server that does not run on Postgres.
// Provision is always made again, which gives the role a new password and
// keeps its database.
func decideDatabase(
	core config.Core, request moduleRequest, asked bool, held *install.Database, left []resource.LeftDatabase,
) (databaseDecision, error) {
	choice := request.database
	if choice != nil && !request.approve {
		return databaseDecision{}, fmt.Errorf("%w: database is chosen only with approve", ErrUsage)
	}

	if choice != nil && !asked {
		return databaseDecision{}, fmt.Errorf("%w: database names where the module's database goes, and the "+
			"manifest asks for no database", ErrUsage)
	}

	if !request.approve || !asked {
		return databaseDecision{}, nil
	}

	if choice == nil {
		switch {
		case held != nil:
			return databaseDecision{granted: held}, nil
		case len(left) > 0:
			return databaseDecision{granted: left[len(left)-1].Grant()}, nil
		}

		return databaseDecision{granted: &install.Database{Engine: wirerpc.DatabaseSQLite}}, nil
	}

	if choice.dsn != "" && choice.mode != databaseModePostgres {
		return databaseDecision{}, fmt.Errorf("%w: a database dsn is given only with the mode postgres", ErrUsage)
	}

	switch choice.mode {
	case databaseModeSQLite:
		return databaseDecision{granted: &install.Database{Engine: wirerpc.DatabaseSQLite}}, nil
	case databaseModePostgres:
		if err := install.CheckPostgresURL(choice.dsn); err != nil {
			return databaseDecision{}, fmt.Errorf("%w: the database dsn: %w", ErrUsage, err)
		}

		return databaseDecision{granted: &install.Database{
			Engine: wirerpc.DatabasePostgres, DSN: strings.TrimSpace(choice.dsn),
		}}, nil
	case databaseModeProvision:
		if core.Persistence.Type != config.EnginePostgres {
			return databaseDecision{}, fmt.Errorf("%w: the database mode provision makes the database on this "+
				"server's own Postgres server, and persistence.type is %q. Choose sqlite, or postgres with a dsn",
				ErrUsage, core.Persistence.Type)
		}

		return databaseDecision{provision: true}, nil
	}

	return databaseDecision{}, fmt.Errorf("%w: the database mode is %q, expected %s, %s or %s", ErrUsage,
		choice.mode, databaseModeSQLite, databaseModePostgres, databaseModeProvision)
}

// grantedDatabase is the database an install writes into the declaration,
// made on the core's Postgres server first when the operator asked for that,
// and what the install does to the databases left behind: the ones it grants
// again, and the one it gives up. It runs after every check and before
// anything is written, so a database the server cannot make installs nothing.
func grantedDatabase(
	ctx context.Context, core config.Core, request moduleRequest, asked bool, declared resource.Declaration,
	instance ref.Ref, typeName string,
) (*install.Database, resource.LeftChange, error) {
	var held *install.Database
	if declared.Spec.Service != nil {
		held = declared.Spec.Service.Database
	}

	left, err := leftFor(core, instance, typeName)
	if err != nil {
		return nil, resource.LeftChange{}, err
	}

	decided, err := decideDatabase(core, request, asked, held, left)
	if err != nil {
		return nil, resource.LeftChange{}, err
	}

	granted := decided.granted
	if decided.provision {
		if granted, err = provisionDatabase(ctx, core.Persistence.Postgres, instance, typeName); err != nil {
			return nil, resource.LeftChange{}, err
		}
	}

	return granted, leftChangeOf(instance, typeName, held, granted, left), nil
}

// leftChangeOf is what granting one database does to the records: each
// record of the same place is taken back, and the database the declaration
// granted before is left behind when the new grant names another place, or
// none.
func leftChangeOf(
	instance ref.Ref, typeName string, held, granted *install.Database, left []resource.LeftDatabase,
) resource.LeftChange {
	var change resource.LeftChange

	for _, one := range left {
		if resource.SamePlace(instance, one, granted) {
			change.Take = append(change.Take, one)
		}
	}

	if held == nil || samePlace(held, granted) {
		return change
	}

	why := resource.LeftMoved
	if granted == nil {
		why = resource.LeftNotGranted
	}

	leave := resource.LeftBehind(instance, typeName, held, why, time.Now())
	change.Leave = &leave

	return change
}

// samePlace reports whether two grants name the same database.
func samePlace(a, b *install.Database) bool {
	if a == nil || b == nil || a.Engine != b.Engine {
		return false
	}

	return a.Engine == wirerpc.DatabaseSQLite || a.Server() == b.Server()
}

// leftFor is the databases the same module left behind under this instance
// name, oldest first. A database another module left under the name is not
// handed to this one.
func leftFor(core config.Core, instance ref.Ref, typeName string) ([]resource.LeftDatabase, error) {
	held, err := resource.LeftDatabases(core.Overlay)
	if err != nil {
		return nil, err
	}

	var out []resource.LeftDatabase

	for _, one := range held {
		if one.Instance == instance.String() && one.Type == typeName {
			out = append(out, one)
		}
	}

	return out, nil
}

// keptState is what an install writes beside the release: the settings, the
// database it grants, what it does to the databases left behind, and those
// records as they were, for an update that is taken back.
type keptState struct {
	settings   json.RawMessage
	database   *install.Database
	left       resource.LeftChange
	leftBefore []resource.LeftDatabase
}

// keptSettingsAndDatabase is the settings an install writes and the database
// it grants. The database comes last, because it may make a database on the
// core's Postgres server, and every refusal before it writes nothing.
func keptSettingsAndDatabase(
	ctx context.Context, core config.Core, namespace string, held manifest.Manifest, request moduleRequest,
	declared resource.Declaration, instance ref.Ref,
) (keptState, error) {
	settings, err := keptSettings(namespace, held, request, declared)
	if err != nil {
		return keptState{}, err
	}

	before, err := resource.LeftDatabases(core.Overlay)
	if err != nil {
		return keptState{}, err
	}

	database, left, err := grantedDatabase(ctx, core, request, held.Needs.Database, declared, instance,
		namespace+extension.QualifiedSeparator+held.Name)
	if err != nil {
		return keptState{}, err
	}

	return keptState{settings: settings, database: database, left: left, leftBefore: before}, nil
}

// databaseChoiceOf is an install request's database in the words the command
// reads, and nil for none.
func databaseChoiceOf(chosen *httpapi.ModuleDatabaseChoice) *databaseChoice {
	if chosen == nil {
		return nil
	}

	return &databaseChoice{mode: strings.TrimSpace(chosen.Mode), dsn: strings.TrimSpace(chosen.DSN)}
}

// databaseFlag reads -database and -database-dsn, and nil when neither is
// given.
func databaseFlag(mode, dsn string) *databaseChoice {
	if strings.TrimSpace(mode) == "" && strings.TrimSpace(dsn) == "" {
		return nil
	}

	return &databaseChoice{mode: strings.TrimSpace(mode), dsn: strings.TrimSpace(dsn)}
}

// databaseGranted is what `module add -approve` prints for the database it
// granted, without its credential.
func databaseGranted(granted *install.Database) string {
	switch {
	case granted == nil:
		return ""
	case granted.Engine == wirerpc.DatabaseSQLite:
		return "sqlite, " + sqliteDatabaseName + " in the module's data directory"
	case granted.Provisioned:
		return "postgres " + granted.Server() + ", made by this server"
	}

	return "postgres " + granted.Server()
}

// databaseLeft is what the install and removal output prints for a database
// they left behind, and empty for none.
func databaseLeft(change resource.LeftChange) string {
	if change.Leave == nil {
		return ""
	}

	return databaseGranted(change.Leave.Grant())
}

// sqliteDatabaseName is the file a module's SQLite database is, in its data
// directory. The host names the same file when it sends the grant.
const sqliteDatabaseName = "database.sqlite"

// databaseOf is the schema version a module that started reported for its own
// database, and the migrations that start applied.
func (m *moduleInstaller) databaseOf(instance ref.Ref, started moduleStart) (int, []string) {
	if m.plane == nil || !started.running {
		return 0, nil
	}

	state, reported := m.plane.Database(instance)
	if !reported {
		return 0, nil
	}

	return state.Version, state.Applied
}
