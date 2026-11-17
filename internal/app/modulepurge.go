package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// Purging a module's own database that no declaration grants. A removal and
// an update that moved the database leave it where it is, with the module's
// data, and record it in the overlay. A purge is the operator's explicit act
// that deletes it: it drops a database and a role this server made, deletes a
// SQLite file, and only forgets a database the operator named, which belongs
// to them and not to this server.

// purgeVerb is the command that deletes a database a module left behind.
const purgeVerb = "purge"

// What a purge did to one database.
const (
	purgeDropped   = "dropped"
	purgeDeleted   = "deleted"
	purgeForgotten = "forgotten"
)

// purgedDatabase is one database a purge dealt with.
type purgedDatabase struct {
	left resource.LeftDatabase
	done string
}

// databaseNotPurgedError is a database the server could not drop. It is a
// type so the API answers it with its own code.
type databaseNotPurgedError struct {
	name   string
	reason error
}

func (e databaseNotPurgedError) Error() string {
	return fmt.Sprintf("this server could not drop the database %s: %v", e.name, e.reason)
}

func (e databaseNotPurgedError) Unwrap() error { return e.reason }

// errNothingLeft reports a name no database left behind is recorded under.
var errNothingLeft = errors.New("no database is left behind under that name")

// purgeAction is what a purge of one record does, before it is done.
func purgeAction(left resource.LeftDatabase, forget bool) string {
	switch {
	case forget:
		return purgeForgotten
	case left.Database.Engine == string(wirerpc.DatabaseSQLite):
		return purgeDeleted
	case left.Database.Provisioned:
		return purgeDropped
	}

	return purgeForgotten
}

// leftNamed is every database left behind under a name the operator typed:
// the instance as kind:name, its name alone, or the module's type.
func leftNamed(core config.Core, named string) ([]resource.LeftDatabase, error) {
	held, err := resource.LeftDatabases(core.Overlay)
	if err != nil {
		return nil, err
	}

	var out []resource.LeftDatabase

	for _, one := range held {
		instance, parseErr := one.Ref()
		if parseErr != nil {
			continue
		}

		if one.Instance == named || instance.ID() == named || one.Type == named {
			out = append(out, one)
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("%w: %q", errNothingLeft, named)
	}

	return out, nil
}

// purgeLeft deletes each database and forgets its record. A database a
// declaration grants again is refused, whatever the record says.
func purgeLeft(
	ctx context.Context, core config.Core, configPath string, records []resource.LeftDatabase, forget bool,
) ([]purgedDatabase, error) {
	declared, err := resource.Installed(resource.Paths{File: configPath, Overlay: core.Overlay})
	if err != nil {
		return nil, err
	}

	var done []purgedDatabase

	for _, left := range records {
		if err = refuseGranted(declared, left); err != nil {
			return done, err
		}

		action := purgeAction(left, forget)

		switch action {
		case purgeDeleted:
			err = deleteSQLiteDatabase(core, left)
		case purgeDropped:
			err = dropLeftDatabase(ctx, core.Persistence.Postgres, left)
		}

		if err != nil {
			return done, err
		}

		if _, err = resource.ForgetLeftDatabase(core.Overlay, left); err != nil {
			return done, err
		}

		done = append(done, purgedDatabase{left: left, done: action})
	}

	return done, nil
}

// refuseGranted refuses a record whose database a declaration grants again.
func refuseGranted(declared []resource.Declaration, left resource.LeftDatabase) error {
	for _, one := range declared {
		if one.Spec.Service == nil || one.Spec.Service.Database == nil {
			continue
		}

		if resource.SamePlace(one.Ref, left, one.Spec.Service.Database) {
			return fmt.Errorf("%w: %s uses this database again, so it is not purged. Remove the module first",
				ErrUsage, one.Ref)
		}
	}

	return nil
}

// deleteSQLiteDatabase deletes the instance's SQLite file and the files
// SQLite keeps beside it. The path is the one the host grants, built from the
// instance name, and never the one the record carries.
func deleteSQLiteDatabase(core config.Core, left resource.LeftDatabase) error {
	instance, err := left.Ref()
	if err != nil {
		return err
	}

	file := filepath.Join(moduleDataRoot(core), instance.ID(), sqliteDatabaseName)

	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err = os.Remove(file + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("delete %s: %w", file+suffix, err)
		}
	}

	return nil
}

// dropLeftDatabase drops a database and a role this server made, on the
// server it made them on. It drops nothing it cannot prove is the module's:
// a name that is not one this server makes, a role whose comment names
// another module, and a database another role owns are all refused.
func dropLeftDatabase(ctx context.Context, postgres config.PostgresEngine, left resource.LeftDatabase) error {
	held, err := url.Parse(left.Database.DSN)
	if err != nil {
		return fmt.Errorf("%w: the recorded connection string of %s does not parse", ErrUsage, left.Instance)
	}

	name := held.User.Username()
	if name != strings.Trim(held.Path, "/") || !strings.HasPrefix(name, moduleDatabasePrefix) || !plainName(name) {
		return fmt.Errorf("%w: %s is not a database this server made, so it is only forgotten. Purge it with "+
			"forget", ErrUsage, held.Host+held.Path)
	}

	admin, base, err := moduleAdmin(postgres)
	if err != nil {
		return fmt.Errorf("%w. Drop the database by hand with DROP DATABASE %s and DROP ROLE %s, then purge it "+
			"with forget", err, name, name)
	}

	if base.Host != held.Host {
		return fmt.Errorf("%w: %s was made on %s, and this server makes databases on %s. Drop it there by hand "+
			"with DROP DATABASE %s and DROP ROLE %s, then purge it with forget",
			ErrUsage, name, held.Host, base.Host, name, name)
	}

	db, err := authz.OpenPostgres(admin)
	if err != nil {
		return databaseNotPurgedError{name: name, reason: err}
	}

	defer func() { _ = db.Close() }() // a handle used for a few statements and nothing else

	if err = dropOwned(ctx, db, name, databaseCommentOf(left.Type)); err != nil {
		return databaseNotPurgedError{name: name, reason: err}
	}

	return nil
}

// dropOwned checks that the role and the database are the module's, and
// drops the database and then the role.
func dropOwned(ctx context.Context, db *sql.DB, name, comment string) error {
	var held string

	err := db.QueryRowContext(ctx, roleCommentSQL, name).Scan(&held)

	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return fmt.Errorf("look for the role: %w", err)
	case held != comment:
		return fmt.Errorf("the role %s: %w", name, errDatabaseTaken)
	}

	var owner string

	err = db.QueryRowContext(ctx, databaseOwnerSQL, name).Scan(&owner)

	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return fmt.Errorf("look for the database: %w", err)
	case owner != name:
		return fmt.Errorf("the database %s: %w", name, errDatabaseTaken)
	}

	quoted := quotedName(name)

	if _, err = db.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quoted); err != nil {
		return fmt.Errorf("drop the database: %w", err)
	}

	if _, err = db.ExecContext(ctx, "DROP ROLE IF EXISTS "+quoted); err != nil {
		return fmt.Errorf("drop the role: %w", err)
	}

	return nil
}

// plainName reports whether a name holds only lower case letters, digits and
// underscores, which is every name this server makes and the only kind it
// quotes into DDL.
func plainName(name string) bool {
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}

	return name != ""
}

// leftPlace is where a database left behind is, without its credential.
func leftPlace(core config.Core, left resource.LeftDatabase) string {
	if left.Database.Engine == string(wirerpc.DatabaseSQLite) {
		if instance, err := left.Ref(); err == nil {
			return filepath.ToSlash(filepath.Join(moduleDataRoot(core), instance.ID(), sqliteDatabaseName))
		}
	}

	return left.Grant().Server()
}

// purgeModule is `module purge`: without -yes it says what it would do and
// does nothing.
func purgeModule(
	ctx context.Context, out io.Writer, core config.Core, configPath, named string, yes, forget bool,
) error {
	if strings.TrimSpace(named) == "" {
		return fmt.Errorf("%w: %s %s <module or kind:name> [-yes] [-forget]", ErrUsage, ModuleCommand, purgeVerb)
	}

	records, err := leftNamed(core, strings.TrimSpace(named))
	if err != nil {
		return err
	}

	if !yes {
		for _, left := range records {
			fmt.Fprintf(out, "would %s  %s %s, left behind by %s\n", verbOf(purgeAction(left, forget)),
				left.Database.Engine, leftPlace(core, left), left.Instance)
		}

		fmt.Fprintf(out, "Nothing was purged. A purge cannot be undone: run it again with -yes.\n")

		return nil
	}

	done, err := purgeLeft(ctx, core, configPath, records, forget)

	for _, one := range done {
		fmt.Fprintf(out, "%-9s %s %s, left behind by %s\n", one.done, one.left.Database.Engine,
			leftPlace(core, one.left), one.left.Instance)
	}

	return err
}

// verbOf is the word `module purge` prints for what it would do.
func verbOf(action string) string {
	switch action {
	case purgeDropped:
		return "drop  "
	case purgeDeleted:
		return "delete"
	}

	return "forget"
}

// listLeft prints the databases left behind, under the modules `module list`
// prints.
func listLeft(out io.Writer, core config.Core) error {
	records, err := resource.LeftDatabases(core.Overlay)
	if err != nil || len(records) == 0 {
		return err
	}

	fmt.Fprintf(out, "\nDatabases no module uses any more. `%s %s <name>` deletes one:\n", ModuleCommand, purgeVerb)

	table := tabwriter.NewWriter(out, 0, 0, columnGap, ' ', 0)

	fmt.Fprintln(table, "INSTANCE\tTYPE\tENGINE\tWHERE\tWHY\tSINCE\tPURGE")

	for _, left := range records {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", left.Instance, left.Type, left.Database.Engine,
			leftPlace(core, left), left.Why, left.LeftAt.Format(time.DateOnly), purgeAction(left, false))
	}

	return table.Flush()
}

// reportDatabase prints the database an install granted and the one it left
// behind.
func reportDatabase(out io.Writer, held moduleReport) {
	if held.database != "" {
		back := ""
		if held.databaseBack {
			back = ", with the data an earlier install left there"
		}

		fmt.Fprintf(out, "  database    %s%s\n", held.database, back)
	}

	if held.databaseLeft != "" {
		fmt.Fprintf(out, "  left behind %s, which the module no longer uses. `%s %s %s` deletes it\n",
			held.databaseLeft, ModuleCommand, purgeVerb, held.instance)
	}
}

// LeftDatabases is httpapi.ModuleInstaller: every database left behind on an
// instance of a kind this caller administers.
func (m *moduleInstaller) LeftDatabases(ctx context.Context, principal string) ([]httpapi.ModuleDatabaseLeft, error) {
	who, err := authz.ParseSubject(principal)
	if err != nil {
		return nil, err
	}

	records, err := resource.LeftDatabases(m.core.Overlay)
	if err != nil {
		return nil, installFault("", err)
	}

	out := []httpapi.ModuleDatabaseLeft{}

	for _, left := range records {
		instance, parseErr := left.Ref()
		if parseErr != nil || m.mayConfigure(ctx, who, instance.Kind()) != nil {
			continue
		}

		out = append(out, httpapi.ModuleDatabaseLeft{
			Instance:    left.Instance,
			Type:        left.Type,
			Engine:      left.Database.Engine,
			Where:       leftPlace(m.core, left),
			Provisioned: left.Database.Provisioned,
			LeftAt:      left.LeftAt,
			Why:         left.Why,
			Purge:       strings.TrimSpace(verbOf(purgeAction(left, false))),
		})
	}

	return out, nil
}

// PurgeDatabases is httpapi.ModuleInstaller: it deletes the databases left
// behind under one instance, one install, removal or purge at a time.
func (m *moduleInstaller) PurgeDatabases(
	ctx context.Context, principal string, instance ref.Ref, forget bool,
) ([]httpapi.ModuleDatabasePurged, error) {
	who, err := authz.ParseSubject(principal)
	if err != nil {
		return nil, err
	}

	if err = m.mayConfigure(ctx, who, instance.Kind()); err != nil {
		return nil, err
	}

	m.writing.Lock()
	defer m.writing.Unlock()

	path := moduleDatabasesPathOf(instance)

	records, err := leftNamed(m.core, instance.String())
	if errors.Is(err, errNothingLeft) {
		return nil, httpfault.RouteNotFound(http.MethodDelete, path)
	}

	if err != nil {
		return nil, installFault(instance.String(), err)
	}

	done, err := purgeLeft(ctx, m.core, m.configPath, records, forget)
	if err != nil {
		if notPurged, ok := errors.AsType[databaseNotPurgedError](err); ok {
			return nil, httpfault.ModuleDatabaseNotPurged(instance.String(), notPurged.name, notPurged.reason.Error())
		}

		return nil, installFault(instance.String(), err)
	}

	out := make([]httpapi.ModuleDatabasePurged, 0, len(done))

	for _, one := range done {
		m.log.InfoContext(ctx, "a database a module left behind was purged",
			"resource", one.left.Instance, "engine", one.left.Database.Engine, "done", one.done,
			"where", leftPlace(m.core, one.left))

		out = append(out, httpapi.ModuleDatabasePurged{
			Instance: one.left.Instance,
			Engine:   one.left.Database.Engine,
			Where:    leftPlace(m.core, one.left),
			Done:     one.done,
		})
	}

	return out, nil
}

// moduleDatabasesPathOf is the purge address of one instance, for a 404.
func moduleDatabasesPathOf(instance ref.Ref) string {
	return "/api/v1/modules/databases/" + instance.Kind().String() + "/" + instance.ID()
}
