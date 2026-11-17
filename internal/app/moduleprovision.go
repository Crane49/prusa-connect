package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The server makes a module's database and its role on its own Postgres
// server.

// databaseNotMadeError is a database the server could not make for a module.
// It is a type so the install API answers it with its own code.
type databaseNotMadeError struct {
	name   string
	reason error
}

func (e databaseNotMadeError) Error() string {
	return fmt.Sprintf("this server could not make the database %s for the module: %v", e.name, e.reason)
}

func (e databaseNotMadeError) Unwrap() error { return e.reason }

// errDatabaseTaken reports a role or a database of the module's name that
// another module or another person made.
var errDatabaseTaken = errors.New("it exists and belongs to someone else")

// moduleDatabasePrefix starts the name of every role and database the server
// makes for a module, so an operator reading the server's list sees whose
// they are.
const moduleDatabasePrefix = "module_"

// postgresNameLimit is the longest name Postgres keeps. A longer one is cut
// short with only a notice, and two modules could then share one database.
const postgresNameLimit = 63

// The statements over the catalogue. A role and a database are named in DDL,
// which takes no parameter, so the name is built from the instance name alone:
// lower case letters, digits and underscores, never anything a caller wrote.
const (
	roleCommentSQL = `SELECT COALESCE(shobj_description(oid, 'pg_authid'), '')
		FROM pg_roles WHERE rolname = $1`
	databaseOwnerSQL = `SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = $1`
)

// databaseNameOf is the name of the role and the database the server makes
// for an instance: the prefix, the name of the core's own database, and the
// instance name, each written in lower case letters, digits and underscores.
// The core's database is in the name so two servers that keep their own
// databases on one Postgres server each make a database of their own for the
// same module, and neither takes the other's.
func databaseNameOf(postgres config.PostgresEngine, instance ref.Ref) (string, error) {
	name := moduleDatabasePrefix + plainPart(instance.ID())
	if core := plainPart(coreDatabaseOf(postgres.DSN)); core != "" {
		name = moduleDatabasePrefix + core + "_" + plainPart(instance.ID())
	}

	if len(name) > postgresNameLimit {
		return "", fmt.Errorf("%w: the instance name %s makes the database name %s, which is longer than the "+
			"%d characters Postgres keeps. Choose sqlite, or postgres with a dsn", ErrUsage,
			instance.ID(), name, postgresNameLimit)
	}

	return name, nil
}

// keyValueDatabase is the dbname field of lib/pq's key=value form.
var keyValueDatabase = regexp.MustCompile(`(?:^|\s)dbname\s*=\s*('[^']*'|\S+)`)

// coreDatabaseOf is the name of the database the core's own connection names,
// and empty when it names none this server can read.
func coreDatabaseOf(dsn string) string {
	if strings.Contains(dsn, "://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return ""
		}

		return strings.Trim(parsed.Path, "/")
	}

	if match := keyValueDatabase.FindStringSubmatch(dsn); match != nil {
		return strings.Trim(match[1], "'")
	}

	return ""
}

// plainPart writes a name in lower case letters, digits and underscores, the
// only characters this server puts into a name it quotes into DDL.
func plainPart(name string) string {
	var out strings.Builder

	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)

			continue
		}

		out.WriteByte('_')
	}

	return out.String()
}

// databaseCommentOf is what the role and the database say about whose they
// are. A later install reads it back before it takes either over.
func databaseCommentOf(typeName string) string {
	return "prusa-connect module " + typeName
}

// provisionDatabase makes the module's role and database, or takes back the
// ones an earlier install of the same module made, and answers the grant with
// the module's own connection string.
func provisionDatabase(
	ctx context.Context, postgres config.PostgresEngine, instance ref.Ref, typeName string,
) (*install.Database, error) {
	name, err := databaseNameOf(postgres, instance)
	if err != nil {
		return nil, err
	}

	admin, base, err := moduleAdmin(postgres)
	if err != nil {
		return nil, err
	}

	db, err := authz.OpenPostgres(admin)
	if err != nil {
		return nil, databaseNotMadeError{name: name, reason: err}
	}

	defer func() { _ = db.Close() }() // a handle used for a few statements and nothing else

	password := rand.Text() + rand.Text()

	if err = makeModuleDatabase(ctx, db, name, databaseCommentOf(typeName), password); err != nil {
		return nil, databaseNotMadeError{name: name, reason: err}
	}

	return &install.Database{
		Engine:      wirerpc.DatabasePostgres,
		DSN:         moduleDSN(base, name, password),
		Provisioned: true,
	}, nil
}

// moduleAdmin is the connection the server makes and drops a module's
// database with: persistence.postgres.module_admin_dsn, or
// persistence.postgres.dsn when that is empty. It is a postgres:// URL,
// because the module's own connection string is built from it.
func moduleAdmin(postgres config.PostgresEngine) (string, *url.URL, error) {
	admin := postgres.ModuleAdminDSN
	if admin == "" {
		admin = postgres.DSN
	}

	base, err := url.Parse(admin)
	if err != nil || install.CheckPostgresURL(admin) != nil {
		return "", nil, fmt.Errorf("%w: this server makes and drops a module's database with "+
			"persistence.postgres.module_admin_dsn, or with persistence.postgres.dsn when that is empty, and it "+
			"is not a postgres:// URL", ErrUsage)
	}

	return admin, base, nil
}

// makeModuleDatabase runs the statements. A database of the name that
// another role owns is refused before anything is made. A role and a database
// this server made earlier for the same module are kept, with the data.
func makeModuleDatabase(ctx context.Context, db *sql.DB, name, comment, password string) error {
	var owner string

	err := db.QueryRowContext(ctx, databaseOwnerSQL, name).Scan(&owner)

	exists := err == nil

	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return fmt.Errorf("look for the database: %w", err)
	case owner != name:
		return fmt.Errorf("the database %s: %w", name, errDatabaseTaken)
	}

	if err = ensureModuleRole(ctx, db, name, comment, password); err != nil {
		return err
	}

	if exists {
		return nil
	}

	return createModuleDatabase(ctx, db, name, comment)
}

// ensureModuleRole makes the module's role, or gives a new password to the
// one an earlier install of the same module made. A role whose comment names
// anything else is refused.
func ensureModuleRole(ctx context.Context, db *sql.DB, name, comment, password string) error {
	role := quotedName(name)

	var held string

	err := db.QueryRowContext(ctx, roleCommentSQL, name).Scan(&held)

	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return fmt.Errorf("look for the role: %w", err)
	case held != comment:
		return fmt.Errorf("the role %s: %w", name, errDatabaseTaken)
	default:
		if _, err = db.ExecContext(ctx, "ALTER ROLE "+role+" WITH LOGIN PASSWORD "+quotedText(password)); err != nil {
			return fmt.Errorf("give the role a new password: %w", err)
		}

		return nil
	}

	// One transaction, so a role never exists without the comment that says
	// whose it is, which a later install would refuse.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin the role: %w", err)
	}

	defer func() { _ = tx.Rollback() }() // a rollback after a commit is a no-op

	if _, err = tx.ExecContext(ctx, "CREATE ROLE "+role+
		" WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD "+quotedText(password)); err != nil {
		return fmt.Errorf("create the role: %w", err)
	}

	if _, err = tx.ExecContext(ctx, "COMMENT ON ROLE "+role+" IS "+quotedText(comment)); err != nil {
		return fmt.Errorf("name the module on the role: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit the role: %w", err)
	}

	return nil
}

// createModuleDatabase makes the database owned by the module's role. The
// connecting role joins the module's role first, because Postgres lets a role
// that is not a superuser give a database only to a role it may act as.
func createModuleDatabase(ctx context.Context, db *sql.DB, name, comment string) error {
	quoted := quotedName(name)

	for _, statement := range []struct{ what, sql string }{
		{"join the module's role", "GRANT " + quoted + " TO CURRENT_USER"},
		{"create the database", "CREATE DATABASE " + quoted + " OWNER " + quoted},
		{"take every right on the database from PUBLIC", "REVOKE ALL ON DATABASE " + quoted + " FROM PUBLIC"},
		{"name the module on the database", "COMMENT ON DATABASE " + quoted + " IS " + quotedText(comment)},
	} {
		if _, err := db.ExecContext(ctx, statement.sql); err != nil {
			return fmt.Errorf("%s: %w", statement.what, err)
		}
	}

	return nil
}

// moduleDSN is the module's connection string: the server, the port and the
// TLS settings of the connection that made the database, with the module's
// role, its password and its database. Every setting that names another login
// is dropped, so the module never holds the admin's.
func moduleDSN(base *url.URL, name, password string) string {
	out := *base
	out.User = url.UserPassword(name, password)
	out.Path = "/" + name
	out.RawPath = ""

	query := out.Query()
	for _, key := range []string{"user", "password", "dbname", "sslcert", "sslkey", "sslpassword"} {
		query.Del(key)
	}

	out.RawQuery = query.Encode()

	return out.String()
}

// quotedName quotes an identifier the server built. It holds only lower case
// letters, digits and underscores, so the quotes are all it needs.
func quotedName(name string) string {
	return `"` + name + `"`
}

// quotedText quotes a literal for DDL, which takes no parameter. A single
// quote is doubled, which is how standard SQL writes one inside a literal.
func quotedText(text string) string {
	return "'" + strings.ReplaceAll(text, "'", "''") + "'"
}
