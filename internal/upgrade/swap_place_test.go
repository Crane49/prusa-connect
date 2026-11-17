package upgrade

import (
	"context"
	"errors"
	"os"
	"testing"
)

// The swap renames a copy placed beside the current binary, never the staged
// file itself, so the staging directory may sit on another filesystem.
func TestRunnerSwapsACopyPlacedBesideTheCurrentBinary(t *testing.T) {
	t.Parallel()

	fixture := newRunnerFixture(t)
	runner, err := NewRunner(RunnerOptions{
		Store: fixture.store, Backups: fixture.backups, BackupPlan: fixture.plan,
		Trial: func(context.Context, Journal) error { return nil }, Now: fixture.now,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = runner.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := readRunnerFile(t, fixture.current); got != "new" {
		t.Fatalf("current binary is %q", got)
	}
	if got := readRunnerFile(t, fixture.staged); got != "new" {
		t.Fatalf("the staged file was moved: it reads %q", got)
	}
	if _, err = os.Stat(fixture.current + nextSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the candidate copy is left beside the binary: %v", err)
	}
}

// A crash between moving the running binary aside and publishing the
// candidate leaves no file at the service path. Recovery puts the copy at
// .previous back.
func TestRunnerRecoveryRestoresAMissingCurrentBinary(t *testing.T) {
	t.Parallel()

	fixture := newStagedRunnerFixture(t)
	journal := advanceRecoveryFixture(t, fixture, StateSwapped, false)
	if err := copyExecutable(fixture.current, journal.PreviousBinary); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(fixture.current, fixture.current+".old"); err != nil {
		t.Fatal(err)
	}

	recovered, err := newRecoveryRunner(t, fixture, nil).Recover(t.Context())
	if err == nil {
		t.Fatal("recovery returned no interruption cause")
	}
	if recovered.State != StateRolledBack {
		t.Fatalf("recovered state is %s: %v", recovered.State, err)
	}
	if got := readRunnerFile(t, fixture.current); got != "old" {
		t.Fatalf("current binary is %q", got)
	}
}
