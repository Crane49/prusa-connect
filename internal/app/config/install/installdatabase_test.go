package install_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// moduleDatabase renders a service block with the given database lines.
func moduleDatabase(database ...string) []byte {
	block := strings.Builder{}

	block.Write(moduleService("com.github.aux.acme.notes/notes"))
	block.WriteString(lines("        database:"))

	for _, line := range database {
		block.WriteString(lines("          " + line))
	}

	return []byte(block.String())
}

// The database an operator granted is read with its engine, and a Postgres
// one with its connection string (ADR-20261021-bqc rule 9).
func TestTheGrantedDatabaseIsReadAtLoad(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		lines []string
		want  install.Database
	}{
		"sqlite": {
			lines: []string{"engine: sqlite"},
			want:  install.Database{Engine: wirerpc.DatabaseSQLite},
		},
		"postgres the operator named": {
			lines: []string{"engine: postgres", "dsn: postgres://notes:s3cret@db.lan:5432/notes?sslmode=require"},
			want: install.Database{
				Engine: wirerpc.DatabasePostgres, DSN: "postgres://notes:s3cret@db.lan:5432/notes?sslmode=require",
			},
		},
		"postgres the server made": {
			lines: []string{"engine: postgres", "dsn: postgresql://module_notes:x@127.0.0.1/module_notes", "provisioned: true"},
			want: install.Database{
				Engine: wirerpc.DatabasePostgres, DSN: "postgresql://module_notes:x@127.0.0.1/module_notes", Provisioned: true,
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml", moduleDatabase(c.lines...))
			require.NoError(t, err)
			require.Empty(t, doc.Failures)
			require.Len(t, doc.Instances, 1)
			require.Equal(t, &c.want, doc.Instances[0].Service.Database)
		})
	}
}

// The listing names the server and never the password, and the secret sweep
// reads the password (ADR-20261021-bqc rule 10).
func TestADatabaseNamesItsServerWithoutItsPassword(t *testing.T) {
	t.Parallel()

	held := &install.Database{Engine: wirerpc.DatabasePostgres, DSN: "postgres://notes:s3cret@db.lan:5432/notes"}

	require.Equal(t, "db.lan:5432/notes", held.Server())
	require.Equal(t, "s3cret", held.Password())
	require.Empty(t, (&install.Database{Engine: wirerpc.DatabaseSQLite}).Server())
	require.Empty(t, (*install.Database)(nil).Password())
}

// An engine the host does not know, a sqlite grant with a connection string,
// and a Postgres grant whose connection string is not a URL are refused at
// load, naming the key.
func TestADatabaseGrantInAnotherShapeIsRefusedAtLoad(t *testing.T) {
	t.Parallel()

	for name, database := range map[string][]string{
		"another engine":           {"engine: mysql"},
		"no engine":                {"dsn: postgres://notes@db.lan/notes"},
		"sqlite with a dsn":        {"engine: sqlite", "dsn: /srv/notes.sqlite"},
		"sqlite made by a server":  {"engine: sqlite", "provisioned: true"},
		"postgres with no dsn":     {"engine: postgres"},
		"postgres in key=value":    {"engine: postgres", "dsn: host=db.lan dbname=notes"},
		"postgres with no name":    {"engine: postgres", "dsn: postgres://notes@db.lan/"},
		"postgres of another kind": {"engine: postgres", "dsn: mysql://notes@db.lan/notes"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, err := install.Parse("config.yaml", moduleDatabase(database...))
			require.NoError(t, err)
			require.Empty(t, doc.Instances)
			require.Len(t, doc.Failures, 1)
			require.ErrorIs(t, doc.Failures[0].Err, install.ErrInvalid)
			require.Contains(t, doc.Failures[0].Err.Error(), "service.database")
		})
	}
}
