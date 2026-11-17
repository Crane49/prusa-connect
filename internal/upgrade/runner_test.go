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

func TestRunnerCommitsAHealthyBinaryAfterBackup(t *testing.T) {
	t.Parallel()

	fixture := newRunnerFixture(t)
	runner, err := NewRunner(RunnerOptions{
		Store: fixture.store, Backups: fixture.backups,
		BackupPlan: fixture.plan,
		Trial:      func(context.Context, Journal) error { return nil },
		Now:        fixture.now,
	})
	if err != nil {
		t.Fatal(err)
	}

	journal, err := runner.Apply(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if journal.State != StateCommitted {
		t.Fatalf("state is %s", journal.State)
	}
	if got := readRunnerFile(t, fixture.current); got != "new" {
		t.Fatalf("current binary is %q", got)
	}
	if got := readRunnerFile(t, fixture.current+".previous"); got != "old" {
		t.Fatalf("previous binary is %q", got)
	}
	if _, err = fixture.backups.Verify(journal.BackupID); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerActivatesBundledExtensionsAfterBackup(t *testing.T) {
	t.Parallel()

	fixture := newRunnerFixture(t)
	journal, err := fixture.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	extensionPath := filepath.Join(t.TempDir(), "spools.tar.gz")
	writeRunnerFile(t, extensionPath, "extension", 0o600)
	extensionDigest := sha256.Sum256([]byte("extension"))
	journal.Extensions = []StagedExtension{{
		BundleExtension: BundleExtension{
			Module: "example.test/spools", Size: int64(len("extension")),
			SHA256: hex.EncodeToString(extensionDigest[:]),
		},
		StagedPath: extensionPath,
	}}
	if err = fixture.store.Save(journal); err != nil {
		t.Fatal(err)
	}
	drained := false
	activated := false
	runner, err := NewRunner(RunnerOptions{
		Store: fixture.store, Backups: fixture.backups, BackupPlan: fixture.plan,
		Trial: func(context.Context, Journal) error { return nil },
		Drain: func(context.Context) error {
			drained = true

			return nil
		},
		Activate: func(context.Context, Journal) error {
			activated = true

			return nil
		},
		Now: fixture.now,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err = runner.Apply(t.Context()); err != nil {
		t.Fatalf("apply error is %v", err)
	}
	if !drained || !activated {
		t.Fatalf("drained=%t activated=%t", drained, activated)
	}
}

func TestRunnerRestoresExactDirectoryAfterExtensionActivationFails(t *testing.T) {
	t.Parallel()

	fixture := newRunnerFixture(t)
	modules := filepath.Join(t.TempDir(), "modules")
	kept := filepath.Join(modules, "spools", "module")
	extra := filepath.Join(modules, "spools", "extra")
	writeRunnerFile(t, kept, "old module", 0o700)

	runner, err := NewRunner(RunnerOptions{
		Store: fixture.store, Backups: fixture.backups,
		BackupPlan: func(journal Journal) (BackupPlan, error) {
			plan, planErr := fixture.plan(journal)
			plan.Files = append(plan.Files, FileSource{Name: "modules", Path: modules})

			return plan, planErr
		},
		Activate: func(context.Context, Journal) error {
			if writeErr := os.WriteFile(kept, []byte("new module"), 0o700); writeErr != nil {
				return writeErr
			}
			if writeErr := os.WriteFile(extra, []byte("extra"), 0o600); writeErr != nil {
				return writeErr
			}

			return errors.New("extension did not activate")
		},
		Trial: func(context.Context, Journal) error { return nil },
		Now:   fixture.now,
	})
	if err != nil {
		t.Fatal(err)
	}

	journal, err := runner.Apply(t.Context())
	if err == nil {
		t.Fatal("failed extension activation returned no error")
	}
	if journal.State != StateRolledBack {
		t.Fatalf("state is %s", journal.State)
	}
	if got := readRunnerFile(t, kept); got != "old module" {
		t.Fatalf("restored module is %q", got)
	}
	if _, statErr := os.Stat(extra); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("rollback left extra file: %v", statErr)
	}
	if got := readRunnerFile(t, fixture.current); got != "old" {
		t.Fatalf("core changed before activation succeeded: %q", got)
	}
}

func TestRunnerRestoresBinaryAndDataAfterFailedTrial(t *testing.T) {
	t.Parallel()

	fixture := newRunnerFixture(t)
	runner, err := NewRunner(RunnerOptions{
		Store: fixture.store, Backups: fixture.backups,
		BackupPlan: fixture.plan,
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
		t.Fatalf("state is %s", journal.State)
	}
	if got := readRunnerFile(t, fixture.current); got != "old" {
		t.Fatalf("current binary is %q", got)
	}
	if got := readRunnerFile(t, fixture.data); got != "before" {
		t.Fatalf("restored data is %q", got)
	}
}

func TestRunnerRecoversAnInterruptedSwap(t *testing.T) {
	t.Parallel()

	fixture := newRunnerFixture(t)
	journal, err := fixture.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	journal, err = fixture.store.Transition(journal, StateDraining, fixture.now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	journal, err = fixture.store.Transition(journal, StateBackedUp, fixture.now(), func(next *Journal) {
		next.BackupID = journal.ID
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fixture.backups.Create(t.Context(), mustPlan(t, fixture, journal)); err != nil {
		t.Fatal(err)
	}
	journal, err = fixture.store.Transition(journal, StateSwapped, fixture.now(), func(next *Journal) {
		next.PreviousBinary = fixture.current + ".previous"
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = swapBinaries(fixture.current, fixture.staged, journal.PreviousBinary, syncDirectory); err != nil {
		t.Fatal(err)
	}

	runner, err := NewRunner(RunnerOptions{
		Store: fixture.store, Backups: fixture.backups, BackupPlan: fixture.plan,
		Trial: func(context.Context, Journal) error { return nil }, Now: fixture.now,
	})
	if err != nil {
		t.Fatal(err)
	}

	journal, err = runner.Recover(t.Context())
	if err == nil {
		t.Fatal("interrupted recovery returned no cause")
	}
	if journal.State != StateRolledBack || readRunnerFile(t, fixture.current) != "old" {
		t.Fatalf("recovered journal is %#v", journal)
	}
}

func TestRunnerRetriesFailedInterruptedRestore(t *testing.T) {
	t.Parallel()

	fixture := newRunnerFixture(t)
	journal, err := fixture.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	journal, err = fixture.store.Transition(journal, StateDraining, fixture.now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	journal, err = fixture.store.Transition(journal, StateBackedUp, fixture.now(), func(next *Journal) {
		next.BackupID = journal.ID
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := fixture.backups.Create(t.Context(), mustPlan(t, fixture, journal))
	if err != nil {
		t.Fatal(err)
	}
	journal, err = fixture.store.Transition(journal, StateSwapped, fixture.now(), func(next *Journal) {
		next.PreviousBinary = fixture.current + ".previous"
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = swapBinaries(fixture.current, fixture.staged, journal.PreviousBinary, syncDirectory); err != nil {
		t.Fatal(err)
	}
	writeRunnerFile(t, fixture.data, "candidate", 0o600)

	backupPath := filepath.Join(fixture.backups.Root, manifest.ID, manifest.Files[0].Path)
	writeRunnerFile(t, backupPath, "tampered", 0o600)

	runner, err := NewRunner(RunnerOptions{
		Store: fixture.store, Backups: fixture.backups, BackupPlan: fixture.plan,
		Trial: func(context.Context, Journal) error { return nil }, Now: fixture.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	journal, err = runner.Recover(t.Context())
	if err == nil || journal.State != StateRollingBack {
		t.Fatalf("first recovery returned state %s and error %v", journal.State, err)
	}
	if got := readRunnerFile(t, fixture.data); got != "candidate" {
		t.Fatalf("failed restore changed data to %q", got)
	}

	writeRunnerFile(t, backupPath, "before", 0o600)
	journal, err = runner.Recover(t.Context())
	if err == nil || journal.State != StateRolledBack {
		t.Fatalf("second recovery returned state %s and error %v", journal.State, err)
	}
	if got := readRunnerFile(t, fixture.data); got != "before" {
		t.Fatalf("retried restore left data as %q", got)
	}
}

func TestRunnerRejectsAStagedBinaryChangedAfterVerification(t *testing.T) {
	t.Parallel()

	fixture := newRunnerFixture(t)
	writeRunnerFile(t, fixture.staged, "tampered", 0o700)
	runner, err := NewRunner(RunnerOptions{
		Store: fixture.store, Backups: fixture.backups, BackupPlan: fixture.plan,
		Trial: func(context.Context, Journal) error { return nil }, Now: fixture.now,
	})
	if err != nil {
		t.Fatal(err)
	}

	journal, err := runner.Apply(t.Context())
	if err == nil {
		t.Fatal("changed staged binary returned no error")
	}
	if journal.State != StateFailedBeforeSwap {
		t.Fatalf("state is %s", journal.State)
	}
	if got := readRunnerFile(t, fixture.current); got != "old" {
		t.Fatalf("current binary is %q", got)
	}
}

func TestRunnerRejectsAStagedExtensionChangedAfterVerification(t *testing.T) {
	t.Parallel()

	fixture := newRunnerFixture(t)
	extension := filepath.Join(t.TempDir(), "spools.tar.gz")
	writeRunnerFile(t, extension, "signed", 0o600)
	digest := sha256.Sum256([]byte("signed"))
	journal, err := fixture.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	journal.Extensions = []StagedExtension{{
		BundleExtension: BundleExtension{
			Module: "example.test/spools", Size: int64(len("signed")),
			SHA256: hex.EncodeToString(digest[:]),
		},
		StagedPath: extension,
	}}
	if err = fixture.store.Save(journal); err != nil {
		t.Fatal(err)
	}
	writeRunnerFile(t, extension, "tampered", 0o600)
	drained := false
	runner, err := NewRunner(RunnerOptions{
		Store: fixture.store, Backups: fixture.backups, BackupPlan: fixture.plan,
		Drain: func(context.Context) error {
			drained = true

			return nil
		},
		Trial: func(context.Context, Journal) error { return nil },
		Now:   fixture.now,
	})
	if err != nil {
		t.Fatal(err)
	}

	journal, err = runner.Apply(t.Context())
	if err == nil {
		t.Fatal("changed staged extension returned no error")
	}
	if journal.State != StateFailedBeforeSwap || drained {
		t.Fatalf("journal=%#v drained=%t", journal, drained)
	}
}

func TestRunnerRechecksTheStagedBinaryAfterBackup(t *testing.T) {
	t.Parallel()

	fixture := newRunnerFixture(t)
	runner, err := NewRunner(RunnerOptions{
		Store: fixture.store, Backups: fixture.backups,
		BackupPlan: func(journal Journal) (BackupPlan, error) {
			writeRunnerFile(t, fixture.staged, "tampered", 0o700)

			return fixture.plan(journal)
		},
		Trial: func(context.Context, Journal) error { return nil },
		Now:   fixture.now,
	})
	if err != nil {
		t.Fatal(err)
	}

	journal, err := runner.Apply(t.Context())
	if err == nil {
		t.Fatal("binary changed during backup returned no error")
	}
	if journal.State != StateRolledBack {
		t.Fatalf("state is %s", journal.State)
	}
	if got := readRunnerFile(t, fixture.current); got != "old" {
		t.Fatalf("current binary is %q", got)
	}
}

type runnerFixture struct {
	store   Store
	backups BackupManager
	current string
	staged  string
	data    string
	clock   time.Time
}

func newRunnerFixture(t *testing.T) *runnerFixture {
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
	digest := sha256.Sum256([]byte("new"))
	journal, err = fixture.store.Transition(journal, StateVerified, fixture.now(), func(next *Journal) {
		next.Verified = &Verified{Name: "core", SHA256: hex.EncodeToString(digest[:]), Size: 3}
	})
	if err != nil {
		t.Fatal(err)
	}
	journal, err = fixture.store.Transition(journal, StatePreflighted, fixture.now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fixture.store.Transition(journal, StateRehearsed, fixture.now(), nil); err != nil {
		t.Fatal(err)
	}

	return fixture
}

func (f *runnerFixture) now() time.Time {
	f.clock = f.clock.Add(time.Second)

	return f.clock
}

func (f *runnerFixture) plan(journal Journal) (BackupPlan, error) {
	return BackupPlan{
		ID: journal.ID, FromVersion: journal.FromVersion, ToVersion: journal.ToVersion,
		Files: []FileSource{{Name: "state", Path: f.data}},
	}, nil
}

func mustPlan(t *testing.T, fixture *runnerFixture, journal Journal) BackupPlan {
	t.Helper()

	plan, err := fixture.plan(journal)
	if err != nil {
		t.Fatal(err)
	}

	return plan
}

func writeRunnerFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func readRunnerFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(content)
}
