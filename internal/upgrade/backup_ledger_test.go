package upgrade

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The host migrator leaves schema_migration.version empty and records its
// floor as a migration name. A snapshot reads numbers from the names and
// reports the named floor.
func TestSQLiteSnapshotReadsTheLedgerWithoutTheVersionColumn(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "host.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE schema_migration (name TEXT PRIMARY KEY, checksum TEXT NOT NULL, version INTEGER)`,
		`INSERT INTO schema_migration (name, checksum) VALUES ('authz/0001_schema.sql', 'a'), ('authz/0012_grants.sql', 'b')`,
		`CREATE TABLE schema_meta (id INTEGER PRIMARY KEY, min_schema_version INTEGER NOT NULL,
			min_migration TEXT NOT NULL DEFAULT '')`,
		`INSERT INTO schema_meta (id, min_schema_version, min_migration) VALUES (1, 9, 'authz/0012_grants.sql')`,
	} {
		if _, err = db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	state, err := inspectSQLite(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != 12 || state.Floor != 9 || state.FloorMigration != "authz/0012_grants.sql" {
		t.Fatalf("state is %+v", state)
	}
	if len(state.Ledger) != 2 || state.Ledger[0].Version != 1 {
		t.Fatalf("ledger is %+v", state.Ledger)
	}
}

// The module data root is restored exactly, files a failed trial wrote and
// changed included, while the SQLite database inside it comes back from its
// own snapshot.
func TestBackupRestoresTheModuleDataRootAroundItsDatabases(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	data := filepath.Join(root, "module-data")
	database := filepath.Join(data, "notes", "database.sqlite")
	kept := filepath.Join(data, "notes", "cache.json")
	writeSQLite(t, database, "before")
	writeFile(t, kept, []byte("before"), 0o600)

	manager := BackupManager{Root: filepath.Join(root, "backups")}
	manifest, err := manager.Create(t.Context(), BackupPlan{
		ID:     "data",
		SQLite: []SQLiteSource{{Name: "module-notes", Path: database}},
		Files: []FileSource{{
			Name: "module-data", Path: data, Directory: true,
			Exclude: []string{database, database + "-wal", database + "-shm"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range manifest.Files {
		if entry.Kind == protectedKindFile && entry.Source == database {
			t.Fatal("the database was copied as a plain file as well")
		}
	}

	writeSQLite(t, database, "migrated")
	writeFile(t, kept, []byte("migrated"), 0o600)
	added := filepath.Join(data, "notes", "v2-index")
	writeFile(t, added, []byte("new"), 0o600)

	if err = manager.Restore(t.Context(), "data", nil); err != nil {
		t.Fatal(err)
	}
	if got := readSQLite(t, database); got != "before" {
		t.Fatalf("database reads %q", got)
	}
	if got := readRunnerFile(t, kept); got != "before" {
		t.Fatalf("module file reads %q", got)
	}
	if _, err = os.Stat(added); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a file the trial added is left: %v", err)
	}
}
