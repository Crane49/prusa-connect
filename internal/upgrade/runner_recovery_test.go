package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunnerRecoveryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state       State
		interrupted bool
	}{
		{state: StateStaged},
		{state: StateVerified},
		{state: StatePreflighted},
		{state: StateRehearsed},
		{state: StateScheduled},
		{state: StateDraining, interrupted: true},
		{state: StateBackedUp, interrupted: true},
		{state: StateSwapped, interrupted: true},
		{state: StateTrial, interrupted: true},
		{state: StateRollingBack, interrupted: true},
	}

	for _, test := range tests {
		t.Run(string(test.state), func(t *testing.T) {
			t.Parallel()

			fixture := newStagedRunnerFixture(t)
			before := advanceRecoveryFixture(t, fixture, test.state, true)
			assertRecoveryFiles(t, fixture, expectedBeforeRecovery(test.state))

			runner := newRecoveryRunner(t, fixture, nil)
			after, err := runner.Recover(t.Context())

			if !test.interrupted {
				if err != nil {
					t.Fatalf("recover prepared transaction: %v", err)
				}
				if after.State != test.state || after.Revision != before.Revision {
					t.Fatalf("recover changed prepared journal from %#v to %#v", before, after)
				}
				assertRecoveryFiles(t, fixture, recoveryFiles{current: "old", staged: "new", data: "before"})

				return
			}

			if err == nil {
				t.Fatal("recover interrupted transaction returned no interruption cause")
			}
			if after.State != StateRolledBack {
				t.Fatalf("recovered state is %s, expected %s", after.State, StateRolledBack)
			}
			assertRecoveryFiles(t, fixture, recoveryFiles{current: "old", staged: "new", data: "before"})
			assertStoredState(t, fixture.store, StateRolledBack)
		})
	}
}

func TestRunnerRecoversEveryDurableSwapBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		crash func(*testing.T, *runnerFixture, Journal)
	}{
		{
			name: "before preserving old binary",
			crash: func(*testing.T, *runnerFixture, Journal) {
			},
		},
		{
			name: "after preserving old binary",
			crash: func(t *testing.T, fixture *runnerFixture, journal Journal) {
				t.Helper()
				if err := os.Link(fixture.current, journal.PreviousBinary); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "after publishing new binary",
			crash: func(t *testing.T, fixture *runnerFixture, journal Journal) {
				t.Helper()
				if err := os.Link(fixture.current, journal.PreviousBinary); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(fixture.staged, fixture.current); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newStagedRunnerFixture(t)
			journal := advanceRecoveryFixture(t, fixture, StateSwapped, false)
			test.crash(t, fixture, journal)

			recovered, err := newRecoveryRunner(t, fixture, nil).Recover(t.Context())
			if err == nil {
				t.Fatal("recover interrupted swap returned no interruption cause")
			}
			if recovered.State != StateRolledBack {
				t.Fatalf("recovered state is %s", recovered.State)
			}
			assertRecoveryFiles(t, fixture, recoveryFiles{current: "old", data: "before"})
		})
	}
}

func TestRunnerFailureMatrixKeepsPreSwapStateOrRollsBack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		configure func(*testing.T, *runnerFixture, *RunnerOptions)
		wantState State
	}{
		{
			name: "backup plan",
			configure: func(_ *testing.T, _ *runnerFixture, options *RunnerOptions) {
				options.BackupPlan = func(Journal) (BackupPlan, error) {
					return BackupPlan{}, errors.New("injected backup plan failure")
				}
			},
			wantState: StateRolledBack,
		},
		{
			name: "backup creation",
			configure: func(_ *testing.T, fixture *runnerFixture, options *RunnerOptions) {
				options.BackupPlan = func(journal Journal) (BackupPlan, error) {
					plan, err := fixture.plan(journal)
					if err != nil {
						return BackupPlan{}, err
					}
					plan.Files = append(plan.Files, FileSource{})

					return plan, nil
				}
			},
			wantState: StateRolledBack,
		},
		{
			name: "rehash before drain",
			configure: func(t *testing.T, fixture *runnerFixture, _ *RunnerOptions) {
				writeRunnerFile(t, fixture.staged, "changed", 0o700)
			},
			wantState: StateFailedBeforeSwap,
		},
		{
			name: "rehash after backup",
			configure: func(t *testing.T, fixture *runnerFixture, options *RunnerOptions) {
				options.BackupPlan = func(journal Journal) (BackupPlan, error) {
					writeRunnerFile(t, fixture.staged, "changed", 0o700)

					return fixture.plan(journal)
				}
			},
			wantState: StateRolledBack,
		},
		{
			name: "swap before publish",
			configure: func(_ *testing.T, _ *runnerFixture, options *RunnerOptions) {
				options.SyncParent = failDirectorySyncCall(1)
			},
			wantState: StateRolledBack,
		},
		{
			name: "swap after publish",
			configure: func(_ *testing.T, _ *runnerFixture, options *RunnerOptions) {
				options.SyncParent = failDirectorySyncCall(2)
			},
			wantState: StateRolledBack,
		},
		{
			name: "trial",
			configure: func(_ *testing.T, fixture *runnerFixture, options *RunnerOptions) {
				options.Trial = func(context.Context, Journal) error {
					if err := os.WriteFile(fixture.data, []byte("migrated"), 0o600); err != nil {
						return err
					}

					return errors.New("injected trial failure")
				}
			},
			wantState: StateRolledBack,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fixture := newRunnerFixture(t)
			options := recoveryRunnerOptions(fixture)
			test.configure(t, fixture, &options)
			runner, err := NewRunner(options)
			if err != nil {
				t.Fatal(err)
			}

			journal, err := runner.Apply(t.Context())
			if err == nil {
				t.Fatal("injected failure returned no error")
			}
			if journal.State != test.wantState {
				t.Fatalf("state is %s, expected %s: %v", journal.State, test.wantState, err)
			}
			assertRecoveryFiles(t, fixture, recoveryFiles{current: "old", data: "before"})
			assertStoredState(t, fixture.store, test.wantState)
		})
	}
}

func TestRunnerRetriesRestoreAfterBackupFailure(t *testing.T) {
	t.Parallel()

	fixture := newStagedRunnerFixture(t)
	_ = advanceRecoveryFixture(t, fixture, StateTrial, true)
	writeRunnerFile(t, fixture.data, "migrated", 0o600)

	manifest, err := fixture.backups.Read("transaction")
	if err != nil {
		t.Fatal(err)
	}
	backupFile := filepath.Join(fixture.backups.Root, manifest.ID, manifest.Files[0].Path)
	writeRunnerFile(t, backupFile, "broken", 0o600)

	runner := newRecoveryRunner(t, fixture, nil)
	journal, err := runner.Recover(t.Context())
	if err == nil {
		t.Fatal("corrupt backup returned no recovery error")
	}
	if journal.State != StateRollingBack {
		t.Fatalf("state is %s, expected %s", journal.State, StateRollingBack)
	}

	writeRunnerFile(t, backupFile, "before", 0o600)
	journal, err = runner.Recover(t.Context())
	if err == nil {
		t.Fatal("successful retry did not retain the original interruption cause")
	}
	if journal.State != StateRolledBack {
		t.Fatalf("retried state is %s, expected %s", journal.State, StateRolledBack)
	}
	assertRecoveryFiles(t, fixture, recoveryFiles{current: "old", staged: "new", data: "before"})
}

func TestRunnerRetriesRecoveryAfterBinaryRestoreSyncFailure(t *testing.T) {
	t.Parallel()

	fixture := newStagedRunnerFixture(t)
	_ = advanceRecoveryFixture(t, fixture, StateTrial, true)
	writeRunnerFile(t, fixture.data, "migrated", 0o600)

	runner := newRecoveryRunner(t, fixture, failDirectorySyncCall(1))
	journal, err := runner.Recover(t.Context())
	if err == nil {
		t.Fatal("directory sync failure returned no recovery error")
	}
	if journal.State != StateRollingBack {
		t.Fatalf("state is %s, expected %s", journal.State, StateRollingBack)
	}

	runner = newRecoveryRunner(t, fixture, nil)
	journal, err = runner.Recover(t.Context())
	if err == nil {
		t.Fatal("successful retry did not retain the original interruption cause")
	}
	if journal.State != StateRolledBack {
		t.Fatalf("retried state is %s, expected %s", journal.State, StateRolledBack)
	}
	assertRecoveryFiles(t, fixture, recoveryFiles{current: "old", staged: "new", data: "before"})
}

type recoveryFiles struct {
	current string
	staged  string
	data    string
}

func newStagedRunnerFixture(t *testing.T) *runnerFixture {
	t.Helper()

	root := t.TempDir()
	fixture := &runnerFixture{
		store:   NewStore(filepath.Join(root, "upgrade", "journal.json")),
		backups: BackupManager{Root: filepath.Join(root, "backups"), Keep: 5},
		current: filepath.Join(root, "bin", "core"),
		staged:  filepath.Join(root, "upgrade", "staged", "core"),
		data:    filepath.Join(root, "config", "state.json"),
		clock:   time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC),
	}
	writeRunnerFile(t, fixture.current, "old", 0o700)
	writeRunnerFile(t, fixture.staged, "new", 0o700)
	writeRunnerFile(t, fixture.data, "before", 0o600)

	journal, err := NewJournal("transaction", "1.0.0", "1.1.0", fixture.staged, fixture.now())
	if err != nil {
		t.Fatal(err)
	}
	journal.CurrentBinary = fixture.current
	if err = fixture.store.Save(journal); err != nil {
		t.Fatal(err)
	}

	return fixture
}

func advanceRecoveryFixture(
	t *testing.T,
	fixture *runnerFixture,
	target State,
	performSwap bool,
) Journal {
	t.Helper()

	journal, err := fixture.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if target == StateStaged {
		return journal
	}

	digest := sha256.Sum256([]byte("new"))
	journal = transitionRecoveryState(t, fixture, journal, StateVerified, func(next *Journal) {
		next.Verified = &Verified{Name: "core", SHA256: hex.EncodeToString(digest[:]), Size: 3}
	})
	if target == StateVerified {
		return journal
	}
	journal = transitionRecoveryState(t, fixture, journal, StatePreflighted, nil)
	if target == StatePreflighted {
		return journal
	}
	journal = transitionRecoveryState(t, fixture, journal, StateRehearsed, nil)
	if target == StateRehearsed {
		return journal
	}
	if target == StateScheduled {
		return transitionRecoveryState(t, fixture, journal, StateScheduled, nil)
	}

	journal = transitionRecoveryState(t, fixture, journal, StateDraining, nil)
	if target == StateDraining {
		return journal
	}
	journal = transitionRecoveryState(t, fixture, journal, StateBackedUp, func(next *Journal) {
		next.BackupID = next.ID
	})
	if target == StateBackedUp {
		return journal
	}
	if _, err = fixture.backups.Create(t.Context(), mustPlan(t, fixture, journal)); err != nil {
		t.Fatal(err)
	}
	journal = transitionRecoveryState(t, fixture, journal, StateSwapped, func(next *Journal) {
		next.PreviousBinary = fixture.current + ".previous"
	})
	if performSwap {
		if err = swapBinaries(fixture.current, fixture.staged, journal.PreviousBinary, syncDirectory); err != nil {
			t.Fatal(err)
		}
	}
	if target == StateSwapped {
		return journal
	}

	journal = transitionRecoveryState(t, fixture, journal, StateTrial, nil)
	if target == StateTrial {
		return journal
	}

	return transitionRecoveryState(t, fixture, journal, StateRollingBack, func(next *Journal) {
		next.Failure = "injected interruption"
	})
}

func transitionRecoveryState(
	t *testing.T,
	fixture *runnerFixture,
	journal Journal,
	next State,
	change func(*Journal),
) Journal {
	t.Helper()

	transitioned, err := fixture.store.Transition(journal, next, fixture.now(), change)
	if err != nil {
		t.Fatalf("transition to %s: %v", next, err)
	}

	return transitioned
}

func newRecoveryRunner(
	t *testing.T,
	fixture *runnerFixture,
	syncParent func(string) error,
) *Runner {
	t.Helper()

	options := recoveryRunnerOptions(fixture)
	options.SyncParent = syncParent
	runner, err := NewRunner(options)
	if err != nil {
		t.Fatal(err)
	}

	return runner
}

func recoveryRunnerOptions(fixture *runnerFixture) RunnerOptions {
	return RunnerOptions{
		Store: fixture.store, Backups: fixture.backups, BackupPlan: fixture.plan,
		Trial: func(context.Context, Journal) error { return nil }, Now: fixture.now,
	}
}

func failDirectorySyncCall(want int) func(string) error {
	call := 0

	return func(string) error {
		call++
		if call == want {
			return errors.New("injected directory sync failure")
		}

		return nil
	}
}

func expectedBeforeRecovery(state State) recoveryFiles {
	switch state {
	case StateSwapped, StateTrial, StateRollingBack:
		return recoveryFiles{current: "new", data: "before"}
	case StateStaged, StateVerified, StatePreflighted, StateRehearsed, StateScheduled,
		StateDraining, StateBackedUp, StateCommitted, StateRolledBack, StateFailedBeforeSwap:
		return recoveryFiles{current: "old", staged: "new", data: "before"}
	}

	panic("unreachable")
}

func assertRecoveryFiles(t *testing.T, fixture *runnerFixture, want recoveryFiles) {
	t.Helper()

	for name, check := range map[string]struct {
		path string
		want string
	}{
		"current binary": {path: fixture.current, want: want.current},
		"staged binary":  {path: fixture.staged, want: want.staged},
		"protected data": {path: fixture.data, want: want.data},
	} {
		if check.want == "" {
			continue
		}
		content, err := os.ReadFile(check.path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(content) != check.want {
			t.Fatalf("%s is %q, expected %q", name, content, check.want)
		}
	}
}

func assertStoredState(t *testing.T, store Store, want State) {
	t.Helper()

	journal, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if journal.State != want {
		t.Fatalf("stored state is %s, expected %s", journal.State, want)
	}
}
