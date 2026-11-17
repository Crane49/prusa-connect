package arch_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/moduledb"
)

// ADR-20261027-kqd: a migration is named by the UTC date and time it was
// written, such as 20261027T093015_device_notes.sql, and never by the next
// number. scripts/new-migration.sh writes one, through make migration-new.
//
// Numbers made two pull requests that each added a migration take the same
// one, and a runner that numbered files across owners moved every later file
// when one owner grew, so every database a release had made refused to start.
// A date is picked alone, and the order of two migrations is the order they
// were written in.
//
// The numbered files below shipped before the dates. Each is in the ledger of
// a real database under its name, so it stays exactly where it is, and no new
// file takes a number.

// numberedMigrations is every numbered migration file. A file here is never
// renamed or deleted, and no file is added here.
//
//nolint:gochecknoglobals // a closed list read by the gate below.
var numberedMigrations = []string{
	"adapters/db/internal/histstore/migrations/postgres/0001_schema.sql",
	"adapters/db/internal/histstore/migrations/sqlite/0001_schema.sql",
	"adapters/db/internal/recstore/migrations/postgres/0001_schema.sql",
	"adapters/db/internal/recstore/migrations/sqlite/0001_schema.sql",
	"examples/database-module/migrations/postgres/0001_notes.sql",
	"examples/database-module/migrations/postgres/0002_note_written.sql",
	"examples/database-module/migrations/postgres/0003_note_words.sql",
	"examples/database-module/migrations/sqlite/0001_notes.sql",
	"examples/database-module/migrations/sqlite/0002_note_written.sql",
	"examples/database-module/migrations/sqlite/0003_note_words.sql",
	"extensions/parts/migrations/postgres/0001_catalogue.sql",
	"extensions/parts/migrations/sqlite/0001_catalogue.sql",
	"internal/authz/migrations/postgres/0001_schema.sql",
	"internal/authz/migrations/sqlite/0001_schema.sql",
	"internal/platform/auth/dbstore/migrations/postgres/0001_schema.sql",
	"internal/platform/auth/dbstore/migrations/postgres/0002_oidc_subject.sql",
	"internal/platform/auth/dbstore/migrations/postgres/0003_scopes_and_app_pairings.sql",
	"internal/platform/auth/dbstore/migrations/sqlite/0001_schema.sql",
	"internal/platform/auth/dbstore/migrations/sqlite/0002_oidc_subject.sql",
	"internal/platform/auth/dbstore/migrations/sqlite/0003_scopes_and_app_pairings.sql",
	"internal/platform/dashboards/migrations/postgres/0001_schema.sql",
	"internal/platform/dashboards/migrations/sqlite/0001_schema.sql",
	"internal/platform/devicestore/migrations/postgres/0001_schema.sql",
	"internal/platform/devicestore/migrations/postgres/0002_job_objects.sql",
	"internal/platform/devicestore/migrations/postgres/0003_printer_job_by_start.sql",
	"internal/platform/devicestore/migrations/sqlite/0001_schema.sql",
	"internal/platform/devicestore/migrations/sqlite/0002_job_objects.sql",
	"internal/platform/devicestore/migrations/sqlite/0003_printer_job_by_start.sql",
	"internal/platform/instanceid/migrations/postgres/0001_schema.sql",
	"internal/platform/instanceid/migrations/sqlite/0001_schema.sql",
}

// datedSince is the first date a dated migration may carry: the day the
// scheme began. An earlier one was backdated, and would sort before files
// that shipped before it.
const datedSince = "20261027T000000"

// migrationEngines are the two directories every migration directory holds.
//
//nolint:gochecknoglobals // a closed list read by the gate below.
var migrationEngines = []string{"sqlite", "postgres"}

func TestEveryMigrationIsDatedAndNoneIsRenamed(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	files, problems := migrationFileProblems(root, numberedMigrations, time.Now().UTC())
	for _, problem := range problems {
		t.Error(problem)
	}

	// The tree held 30 migration files when this was written.
	requireScanned(t, "migration files under the walked tree", files, len(numberedMigrations))
}

// migrationFileProblems walks root for migration files and reports every one
// the rules refuse, with how many files it read. now bounds a stamp from
// above, so a date in the future is caught.
func migrationFileProblems(root string, numbered []string, now time.Time) (int, []string) {
	var (
		problems []string
		read     int
		dirs     = map[string]map[string][]string{} // migrations dir -> engine -> names
	)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return skipDir(root, path)
		}

		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		relative = filepath.ToSlash(relative)
		engineDir := filepath.ToSlash(filepath.Dir(relative))
		migrationsDir, engine := filepath.ToSlash(filepath.Dir(engineDir)), filepath.Base(engineDir)

		if filepath.Base(migrationsDir) != "migrations" || !slices.Contains(migrationEngines, engine) {
			return nil
		}

		read++

		if dirs[migrationsDir] == nil {
			dirs[migrationsDir] = map[string][]string{}
		}

		dirs[migrationsDir][engine] = append(dirs[migrationsDir][engine], entry.Name())

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		problems = append(problems, migrationFileProblem(relative, entry.Name(), content, numbered, now)...)

		return nil
	})
	if err != nil {
		return read, append(problems, fmt.Sprintf("walking the tree: %v", err))
	}

	for _, path := range numbered {
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(path))); statErr != nil {
			problems = append(problems, fmt.Sprintf("%s shipped and is gone: %v. A shipped migration is in "+
				"the ledger of a real database under its name. Restore it as it was, and put the change in a "+
				"new dated file (make migration-new)", path, statErr))
		}
	}

	for dir, engines := range dirs {
		sqlite, postgres := slices.Sorted(slices.Values(engines["sqlite"])),
			slices.Sorted(slices.Values(engines["postgres"]))
		if !slices.Equal(sqlite, postgres) {
			problems = append(problems, fmt.Sprintf("%s/sqlite holds %v and %s/postgres holds %v. Both "+
				"engines ship the same file names, because the ledger records one name for both", dir, sqlite,
				dir, postgres))
		}
	}

	slices.Sort(problems)

	return read, problems
}

// migrationFileProblem is what the rules refuse in one file.
func migrationFileProblem(relative, name string, content []byte, numbered []string, now time.Time) []string {
	var problems []string

	parsed, err := moduledb.ParseMigrationName(name)

	switch {
	case err != nil || parsed.Step:
		problems = append(problems, fmt.Sprintf("%s is not named by the date it was written. Name it "+
			"<date>T<time>_<words>.sql in UTC, such as 20261027T093015_device_notes.sql, with make "+
			"migration-new (ADR-20261027-kqd)", relative))
	case !parsed.Dated() && !slices.Contains(numbered, relative):
		problems = append(problems, fmt.Sprintf("%s takes a number. A new migration is named by the date it "+
			"was written, with make migration-new, so two pull requests never take the same name "+
			"(ADR-20261027-kqd)", relative))
	case parsed.Dated() && parsed.Stamp < datedSince:
		problems = append(problems, fmt.Sprintf("%s is dated before %s, when dated names began. It would "+
			"sort before files that shipped before it", relative, datedSince))
	case parsed.Dated() && parsed.Stamp > now.Add(24*time.Hour).Format(moduledb.StampLayout):
		problems = append(problems, fmt.Sprintf("%s is dated in the future. The date is when the file was "+
			"written, in UTC", relative))
	}

	if err = moduledb.RequireTransactional(content); err != nil {
		problems = append(problems, fmt.Sprintf("%s: %v. Do such work in code after the server starts",
			relative, err))
	}

	return problems
}

// TestTheMigrationNameGateRefusesEachRule is the mutation check: a tree that
// breaks each rule once, and the gate names each break.
func TestTheMigrationNameGateRefusesEachRule(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

	for path, content := range map[string]string{
		"store/migrations/sqlite/0001_schema.sql":                 "CREATE TABLE a (id INTEGER);",
		"store/migrations/postgres/0001_schema.sql":               "CREATE TABLE a (id INTEGER);",
		"store/migrations/sqlite/0002_next.sql":                   "ALTER TABLE a ADD COLUMN b TEXT;",
		"store/migrations/postgres/0002_next.sql":                 "ALTER TABLE a ADD COLUMN b TEXT;",
		"store/migrations/sqlite/add_column.sql":                  "ALTER TABLE a ADD COLUMN c TEXT;",
		"store/migrations/postgres/add_column.sql":                "ALTER TABLE a ADD COLUMN c TEXT;",
		"store/migrations/sqlite/20261020T090000_backdated.sql":   "SELECT 1;",
		"store/migrations/postgres/20261020T090000_backdated.sql": "SELECT 1;",
		"store/migrations/sqlite/20271020T090000_future.sql":      "SELECT 1;",
		"store/migrations/postgres/20271020T090000_future.sql":    "SELECT 1;",
		"store/migrations/sqlite/20261028T090000_commits.sql":     "UPDATE a SET b = 1;\nCOMMIT;",
		"store/migrations/postgres/20261028T090000_commits.sql":   "UPDATE a SET b = 1;\nCOMMIT;",
		"store/migrations/sqlite/20261028T100000_index.sql":       "CREATE INDEX CONCURRENTLY i ON a (b);",
		"store/migrations/postgres/20261028T100000_index.sql":     "CREATE INDEX CONCURRENTLY i ON a (b);",
		"store/migrations/sqlite/20261029T090000_one_engine.sql":  "SELECT 1;",
		"store/migrations/sqlite/20261029T100000_fine.sql":        "SELECT 1;",
		"store/migrations/postgres/20261029T100000_fine.sql":      "SELECT 1;",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	numbered := []string{
		"store/migrations/sqlite/0001_schema.sql", "store/migrations/postgres/0001_schema.sql",
		"store/migrations/sqlite/0001_renamed.sql",
	}

	_, problems := migrationFileProblems(root, numbered, now)
	joined := strings.Join(problems, "\n")

	for _, want := range []string{
		"store/migrations/sqlite/0002_next.sql takes a number",
		"store/migrations/postgres/0002_next.sql takes a number",
		"store/migrations/sqlite/add_column.sql is not named by the date",
		"20261020T090000_backdated.sql is dated before",
		"20271020T090000_future.sql is dated in the future",
		"20261028T090000_commits.sql: moduledb: a migration holds a statement",
		"20261028T100000_index.sql: moduledb: a migration holds a statement",
		"store/migrations/sqlite/0001_renamed.sql shipped and is gone",
		"store/migrations/sqlite holds",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the gate did not report %q. It reported:\n%s", want, joined)
		}
	}

	for _, problem := range problems {
		if strings.HasPrefix(problem, "store/migrations/sqlite/20261029T100000_fine.sql") ||
			strings.Contains(problem, "0001_schema.sql shipped") || strings.Contains(problem, "0001_schema.sql takes") {
			t.Errorf("the gate refused a file that keeps the rules: %s", problem)
		}
	}
}
