package moduledb_test

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/prusa3d/prusa-connect/sdk/moduledb"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The real engines are exercised by examples/database-module, which may
// import the drivers this module may not. These cases need no driver.

func TestAChecksumReadsCRLFAsLF(t *testing.T) {
	t.Parallel()

	unix, err := moduledb.Checksum([]byte("CREATE TABLE a (id INTEGER);\n"))
	if err != nil {
		t.Fatalf("checksum: %v", err)
	}

	windows, err := moduledb.Checksum([]byte("CREATE TABLE a (id INTEGER);\r\n"))
	if err != nil {
		t.Fatalf("checksum: %v", err)
	}

	if unix != windows {
		t.Errorf("the two line endings hash differently: %s and %s", unix, windows)
	}

	_, err = moduledb.Checksum([]byte("-- a comment\rDROP TABLE a;\n"))
	if !errors.Is(err, moduledb.ErrMigrationCarriageReturn) {
		t.Errorf("a bare carriage return: err = %v", err)
	}
}

func TestAGrantIsRefusedBeforeAnythingIsOpened(t *testing.T) {
	t.Parallel()

	schema := moduledb.Schema{Migrations: fstest.MapFS{}}

	for name, tc := range map[string]struct {
		grant wirerpc.DatabaseGrant
		want  error
	}{
		"an unknown engine":   {wirerpc.DatabaseGrant{Engine: "mysql", DSN: "x"}, moduledb.ErrEngine},
		"a relative path":     {wirerpc.DatabaseGrant{Engine: wirerpc.DatabaseSQLite, DSN: "notes.sqlite"}, moduledb.ErrSQLitePath},
		"a path with a query": {wirerpc.DatabaseGrant{Engine: wirerpc.DatabaseSQLite, DSN: "/srv/notes.sqlite?mode=ro"}, moduledb.ErrSQLitePath},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db, _, err := moduledb.Open(t.Context(), tc.grant, schema)
			if !errors.Is(err, tc.want) || db != nil {
				t.Errorf("Open = %v, %v, want %v", db, err, tc.want)
			}
		})
	}
}
