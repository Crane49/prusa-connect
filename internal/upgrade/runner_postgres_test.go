package upgrade

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
)

// A PostgreSQL database in external or none mode has no dump in the backup.
// A failed trial still restores the files and the previous binary, finishes
// as rolled back and names the databases the operator restores.
func TestRunnerRollsBackWhenPostgresIsProtectedOutsideTheServer(t *testing.T) {
	t.Parallel()

	for _, mode := range []PostgresMode{PostgresExternal, PostgresNone} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			fixture := newRunnerFixture(t)
			runner, err := NewRunner(RunnerOptions{
				Store: fixture.store, Backups: fixture.backups,
				BackupPlan: func(journal Journal) (BackupPlan, error) {
					plan, planErr := fixture.plan(journal)
					plan.Postgres = []PostgresSource{
						{Name: "host", DSN: "postgres://pcl@db/pcl", Mode: mode, Acknowledged: true},
						{Name: "module-spools", DSN: "postgres://spools@db/spools", Mode: mode, Acknowledged: true},
					}

					return plan, planErr
				},
				Trial: func(context.Context, Journal) error {
					if err := os.WriteFile(fixture.data, []byte("migrated"), 0o600); err != nil {
						t.Fatal(err)
					}

					return errors.New("readiness timed out")
				},
				Now: fixture.now,
			})
			if err != nil {
				t.Fatal(err)
			}

			journal, err := runner.Apply(t.Context())
			if err == nil {
				t.Fatal("failed trial returned no error")
			}
			if journal.State != StateRolledBack {
				t.Fatalf("state is %s: %v", journal.State, err)
			}
			if !slices.Equal(journal.ExternalRestore, []string{"host", "module-spools"}) {
				t.Fatalf("databases left for the operator are %v", journal.ExternalRestore)
			}
			stored, err := fixture.store.Load()
			if err != nil {
				t.Fatal(err)
			}
			if stored.State != StateRolledBack || len(stored.ExternalRestore) != 2 {
				t.Fatalf("stored journal is %s with %v", stored.State, stored.ExternalRestore)
			}
			if got := readRunnerFile(t, fixture.current); got != "old" {
				t.Fatalf("current binary is %q", got)
			}
			if got := readRunnerFile(t, fixture.data); got != "before" {
				t.Fatalf("restored data is %q", got)
			}
		})
	}
}
