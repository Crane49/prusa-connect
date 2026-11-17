//go:build linux

package upgrade

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// On Linux /dev/shm is a separate tmpfs, so a stage there cannot be renamed
// over a binary in the test directory. Both the swap and the rollback must
// still finish.
func TestRunnerAppliesAndRollsBackAcrossFilesystems(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		trial   error
		state   State
		current string
	}{
		{name: "commit", state: StateCommitted, current: "new"},
		{name: "rollback", trial: errors.New("readiness timed out"), state: StateRolledBack, current: "old"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// t.TempDir cannot pick another filesystem.
			shm, err := os.MkdirTemp("/dev/shm", "pcl-upgrade-*") //nolint:usetesting // see above
			if err != nil {
				t.Skip("no /dev/shm on this system")
			}
			t.Cleanup(func() { _ = os.RemoveAll(shm) })

			fixture := newRunnerFixture(t)
			if sameDevice(t, shm, fixture.current) {
				t.Skip("/dev/shm is on the same filesystem as the test directory")
			}
			journal, err := fixture.store.Load()
			if err != nil {
				t.Fatal(err)
			}
			staged := filepath.Join(shm, "core")
			writeRunnerFile(t, staged, "new", 0o700)
			journal.StagedBinary = staged
			if err = fixture.store.Save(journal); err != nil {
				t.Fatal(err)
			}

			runner, err := NewRunner(RunnerOptions{
				Store: fixture.store, Backups: fixture.backups, BackupPlan: fixture.plan,
				Trial: func(context.Context, Journal) error { return tc.trial },
				Now:   fixture.now,
			})
			if err != nil {
				t.Fatal(err)
			}

			journal, err = runner.Apply(t.Context())
			if journal.State != tc.state {
				t.Fatalf("state is %s, error %v", journal.State, err)
			}
			if got := readRunnerFile(t, fixture.current); got != tc.current {
				t.Fatalf("current binary is %q", got)
			}
		})
	}
}

func sameDevice(t *testing.T, left, right string) bool {
	t.Helper()

	var a, b syscall.Stat_t
	if syscall.Stat(left, &a) != nil || syscall.Stat(right, &b) != nil {
		t.Fatal("stat for device comparison failed")
	}

	return a.Dev == b.Dev
}
