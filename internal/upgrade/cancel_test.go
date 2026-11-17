package upgrade

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestCancelScheduledRemovesTheTransactionAndWhatItStaged(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	staged := filepath.Join(root, "staged", "upgrade-1")
	saveScheduled(t, root, filepath.Join(staged, "core"), "")

	journal, cancelled, err := CancelScheduled(root)
	if err != nil {
		t.Fatal(err)
	}
	if !cancelled || journal.ID != "upgrade-1" || journal.ToVersion != "1.1.0" {
		t.Fatalf("CancelScheduled returned %+v, %v", journal, cancelled)
	}
	if _, err = NewStore(filepath.Join(root, "journal.json")).Load(); !errors.Is(err, ErrNoJournal) {
		t.Fatalf("the journal stayed: %v", err)
	}
	if _, err = os.Stat(staged); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the staged release stayed: %v", err)
	}
}

func TestCancelScheduledLeavesEveryOtherTransaction(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if _, cancelled, err := CancelScheduled(root); err != nil || cancelled {
		t.Fatalf("no journal: cancelled %v, error %v", cancelled, err)
	}

	staged := filepath.Join(root, "staged", "upgrade-1")
	journal := saveScheduled(t, root, filepath.Join(staged, "core"), "")
	journal.State, journal.Revision = StateRehearsed, journal.Revision+1
	if err := NewStore(filepath.Join(root, "journal.json")).Save(journal); err != nil {
		t.Fatal(err)
	}

	if _, cancelled, err := CancelScheduled(root); err != nil || cancelled {
		t.Fatalf("a rehearsed transaction: cancelled %v, error %v", cancelled, err)
	}
	if _, err := os.Stat(filepath.Join(staged, "core")); err != nil {
		t.Fatalf("the staged release of a rehearsed transaction is gone: %v", err)
	}
}

// TestCancelRemovesARelativeStagingFolder is the systemd service, whose
// overlay and so its upgrade folder are relative to the working directory.
// The service records the staging folder the same way.
func TestCancelRemovesARelativeStagingFolder(t *testing.T) {
	for way, cancel := range cancels() {
		t.Run(way, func(t *testing.T) {
			work := t.TempDir()
			t.Chdir(work)

			root := filepath.Join("config", "config.d", "upgrade")
			staged := filepath.Join(root, "staged", "upgrade-1")
			placeFile(t, filepath.Join(staged, "core"))
			saveScheduled(t, root, filepath.Join(staged, "core"), "")

			if cancelled, err := cancel(root); !cancelled || err != nil {
				t.Fatalf("cancelled %v, error %v", cancelled, err)
			}
			if _, err := os.Stat(staged); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the staged release stayed: %v", err)
			}
		})
	}
}

// TestCancelRemovesNothingOutsideTheStagingFolder is a journal that names a
// folder the service never stages in: the staging folder itself, a path that
// climbs out, a sibling whose name starts like the staging folder, and a
// folder elsewhere.
func TestCancelRemovesNothingOutsideTheStagingFolder(t *testing.T) {
	t.Parallel()

	for name, journalNames := range map[string]func(root, outside string) (binary, stagedRoot string){
		"the staging folder": func(root, _ string) (string, string) { return "core", filepath.Join(root, "staged") },
		"a climb out":        func(root, _ string) (string, string) { return "core", filepath.Join(root, "staged", "..", "..") },
		"a sibling":          func(root, _ string) (string, string) { return "core", filepath.Join(root, "staged-x") },
		"a folder elsewhere": func(_, outside string) (string, string) { return filepath.Join(outside, "core"), "" },
		"a root elsewhere":   func(_, outside string) (string, string) { return "core", outside },
		"the upgrade folder": func(root, _ string) (string, string) { return filepath.Join(root, "core"), "" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for way, cancel := range cancels() {
				t.Run(way, func(t *testing.T) {
					t.Parallel()

					root := filepath.Join(t.TempDir(), "upgrade")
					outside := t.TempDir()
					kept := []string{
						filepath.Join(outside, "core"),
						filepath.Join(root, "staged", "other", "core"),
						filepath.Join(root, "staged-x", "core"),
						filepath.Join(root, "core"),
					}
					for _, path := range kept {
						placeFile(t, path)
					}

					binary, stagedRoot := journalNames(root, outside)
					saveScheduled(t, root, binary, stagedRoot)

					cancelled, err := cancel(root)
					if !cancelled || !errors.Is(err, ErrJournal) || !errors.Is(err, ErrStagedLeft) {
						t.Fatalf("cancelled %v, error %v; want the transaction removed and ErrJournal", cancelled, err)
					}
					for _, path := range kept {
						if _, statErr := os.Stat(path); statErr != nil {
							t.Fatalf("%s is gone: %v", path, statErr)
						}
					}
				})
			}
		})
	}
}

// TestCancelRemovesNothingARelativePathNames is a journal whose staging
// folder is relative: a bare binary name, whose folder is the working
// directory, and a climb above it. The test runs in a folder of its own, so
// a removal it misses cannot reach the source tree.
func TestCancelRemovesNothingARelativePathNames(t *testing.T) {
	for name, stagedRoot := range map[string]string{
		"a bare binary name": "",
		"a climb above":      filepath.Join("..", ".."),
	} {
		t.Run(name, func(t *testing.T) {
			for way, cancel := range cancels() {
				t.Run(way, func(t *testing.T) {
					work := filepath.Join(t.TempDir(), "a", "b", "work")
					placeFile(t, filepath.Join(work, "kept"))
					t.Chdir(work)

					root := filepath.Join(work, "upgrade")
					saveScheduled(t, root, "core", stagedRoot)

					if cancelled, err := cancel(root); !cancelled || !errors.Is(err, ErrJournal) {
						t.Fatalf("cancelled %v, error %v; want the transaction removed and ErrJournal", cancelled, err)
					}
					if _, err := os.Stat(filepath.Join(work, "kept")); err != nil {
						t.Fatalf("the working directory lost a file: %v", err)
					}
				})
			}
		})
	}
}

// TestCancelRefusesAJournalWithNoStagedBinary is a journal written by hand
// with an empty path, which the store refuses to read.
func TestCancelRefusesAJournalWithNoStagedBinary(t *testing.T) {
	t.Parallel()

	for way, cancel := range cancels() {
		t.Run(way, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			placeFile(t, filepath.Join(root, "staged", "core"))
			content := `{"schema":1,"id":"upgrade-1","state":"scheduled","from_version":"1.0.0",` +
				`"to_version":"1.1.0","staged_binary":"","current_binary":"/c","revision":1,` +
				`"updated_at":"2026-10-26T12:00:00Z"}`
			if err := os.WriteFile(filepath.Join(root, "journal.json"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}

			if cancelled, err := cancel(root); cancelled || !errors.Is(err, ErrJournal) {
				t.Fatalf("cancelled %v, error %v; want nothing removed and ErrJournal", cancelled, err)
			}
			if _, err := os.Stat(filepath.Join(root, "staged", "core")); err != nil {
				t.Fatalf("the staging folder is gone: %v", err)
			}
		})
	}
}

func saveScheduled(t *testing.T, root, binary, stagedRoot string) Journal {
	t.Helper()

	due := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	journal := Journal{
		Schema: 1, ID: "upgrade-1", State: StateScheduled,
		FromVersion: "1.0.0", ToVersion: "1.1.0",
		StagedBinary: binary, StagedRoot: stagedRoot, CurrentBinary: "/c",
		Schedule: "at", ScheduledFor: &due, Revision: 1, UpdatedAt: due,
	}
	if filepath.IsAbs(binary) {
		placeFile(t, binary)
	}
	if err := NewStore(filepath.Join(root, "journal.json")).Save(journal); err != nil {
		t.Fatal(err)
	}

	return journal
}

// placeFile writes a small private file at path, with its folders.
func placeFile(t *testing.T, path string) {
	t.Helper()

	writeFile(t, path, []byte("x"), 0o600)
}

// cancels are the two ways a transaction is cancelled: Cancel on the Updates
// tab, and CancelScheduled at a start with upgrades off. Both remove through
// cancelTransaction, and the tests that run each keep them from drifting
// apart again. Each reports whether the journal is gone.
func cancels() map[string]func(root string) (bool, error) {
	return map[string]func(root string) (bool, error){
		"CancelScheduled": func(root string) (bool, error) {
			_, cancelled, err := CancelScheduled(root)

			return cancelled, err
		},
		"Service.Cancel": func(root string) (bool, error) {
			service, err := NewService(ServiceOptions{Root: root, CurrentVersion: "1.0.0"})
			if err != nil {
				return false, err
			}

			return service.Cancel()
		},
	}
}

// TestCancelRemovesNothingThroughALink is a staging folder that holds a link
// to a folder outside it, and a journal that names a folder behind the link.
func TestCancelRemovesNothingThroughALink(t *testing.T) {
	t.Parallel()

	for way, cancel := range cancels() {
		t.Run(way, func(t *testing.T) {
			t.Parallel()

			root := filepath.Join(t.TempDir(), "upgrade")
			outside := t.TempDir()
			placeFile(t, filepath.Join(outside, "victim", "keep"))
			placeFile(t, filepath.Join(root, "staged", "upgrade-0", "core"))
			if err := os.Symlink(outside, filepath.Join(root, "staged", "link")); err != nil {
				t.Skipf("no symbolic link here: %v", err)
			}
			saveScheduled(t, root, "core", filepath.Join(root, "staged", "link", "victim"))

			if cancelled, err := cancel(root); !cancelled || !errors.Is(err, ErrStagedLeft) {
				t.Fatalf("cancelled %v, error %v; want the transaction removed and ErrStagedLeft", cancelled, err)
			}
			if _, err := os.Stat(filepath.Join(outside, "victim", "keep")); err != nil {
				t.Fatalf("a file behind the link is gone: %v", err)
			}
		})
	}
}

// TestCancelCancelsATransactionWhoseReleaseIsGone is the state a stop between
// the two steps leaves: the journal waits, and its staged release is already
// removed.
func TestCancelCancelsATransactionWhoseReleaseIsGone(t *testing.T) {
	t.Parallel()

	for way, cancel := range cancels() {
		t.Run(way, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			saveScheduled(t, root, filepath.Join(root, "staged", "upgrade-1", "core"), "")
			if err := os.RemoveAll(filepath.Join(root, "staged")); err != nil {
				t.Fatal(err)
			}

			if cancelled, err := cancel(root); !cancelled || err != nil {
				t.Fatalf("cancelled %v, error %v", cancelled, err)
			}
			if _, err := NewStore(filepath.Join(root, "journal.json")).Load(); !errors.Is(err, ErrNoJournal) {
				t.Fatalf("the journal stayed: %v", err)
			}
		})
	}
}

// TestCancelRemovesNothingThroughALinkedStagingFolder is a staging folder
// that is itself a link to a folder outside the upgrade folder.
func TestCancelRemovesNothingThroughALinkedStagingFolder(t *testing.T) {
	t.Parallel()

	for way, cancel := range cancels() {
		t.Run(way, func(t *testing.T) {
			t.Parallel()

			root := filepath.Join(t.TempDir(), "upgrade")
			outside := t.TempDir()
			placeFile(t, filepath.Join(outside, "victim", "keep"))
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, "staged")); err != nil {
				t.Skipf("no symbolic link here: %v", err)
			}
			saveScheduled(t, root, "core", filepath.Join(root, "staged", "victim"))

			if cancelled, err := cancel(root); !cancelled || !errors.Is(err, ErrStagedLeft) {
				t.Fatalf("cancelled %v, error %v; want the transaction removed and ErrStagedLeft", cancelled, err)
			}
			if _, err := os.Stat(filepath.Join(outside, "victim", "keep")); err != nil {
				t.Fatalf("a file behind the linked staging folder is gone: %v", err)
			}
		})
	}
}

// TestCancelRemovesNothingThroughARelativeLink is a link inside the staging
// folder, or the staging folder itself, that leads to the backups beside it
// in the upgrade folder.
func TestCancelRemovesNothingThroughARelativeLink(t *testing.T) {
	t.Parallel()

	for name, link := range map[string]func(root string) (linkAt, target, stagedRoot string){
		"a link inside staged": func(root string) (string, string, string) {
			return filepath.Join(root, "staged", "up"), "..", filepath.Join(root, "staged", "up", "backups")
		},
		"staged itself": func(root string) (string, string, string) {
			return filepath.Join(root, "staged"), "backups", filepath.Join(root, "staged", "b1")
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for way, cancel := range cancels() {
				t.Run(way, func(t *testing.T) {
					t.Parallel()

					root := filepath.Join(t.TempDir(), "upgrade")
					kept := filepath.Join(root, "backups", "b1", "keep")
					placeFile(t, kept)
					linkAt, target, stagedRoot := link(root)
					if err := os.MkdirAll(filepath.Dir(linkAt), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, linkAt); err != nil {
						t.Skipf("no symbolic link here: %v", err)
					}
					saveScheduled(t, root, "core", stagedRoot)

					if cancelled, err := cancel(root); !cancelled || !errors.Is(err, ErrStagedLeft) {
						t.Fatalf(
							"cancelled %v, error %v; want the transaction removed and ErrStagedLeft",
							cancelled,
							err,
						)
					}
					if _, err := os.Stat(kept); err != nil {
						t.Fatalf("a backup behind the link is gone: %v", err)
					}
				})
			}
		})
	}
}

// TestCancelReportsBothRemovalsThatFail is an upgrade folder and a staging
// folder the server cannot write. Neither the release nor the journal can be
// removed, and the error says both.
func TestCancelReportsBothRemovalsThatFail(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a folder mode does not stop this process here")
	}

	for way, cancel := range cancels() {
		t.Run(way, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			staged := filepath.Join(root, "staged", "upgrade-1")
			saveScheduled(t, root, filepath.Join(staged, "core"), "")
			for _, folder := range []string{filepath.Join(root, "staged"), root} {
				if err := os.Chmod(folder, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(folder, 0o700) })
			}

			cancelled, err := cancel(root)
			if cancelled {
				t.Fatal("the transaction is reported cancelled, and its journal stays")
			}
			if !errors.Is(err, ErrStagedLeft) || !errors.Is(err, os.ErrPermission) {
				t.Fatalf("error %v; want the release left and the journal not removed", err)
			}
			if _, statErr := os.Stat(staged); statErr != nil {
				t.Fatalf("the staged release is gone: %v", statErr)
			}
		})
	}
}

// TestStageRemovesTheFinishedReleaseOnlyInTheStagingFolder is a finished
// transaction that a new stage replaces. Its staged release is removed the
// way a cancel removes it: below the staging folder, and nowhere else its
// journal names.
func TestStageRemovesTheFinishedReleaseOnlyInTheStagingFolder(t *testing.T) {
	t.Parallel()

	for name, elsewhere := range map[string]bool{"the staging folder": false, "a folder elsewhere": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := filepath.Join(t.TempDir(), "upgrade")
			release := filepath.Join(root, "staged", "upgrade-0")
			if elsewhere {
				release = t.TempDir()
			}
			placeFile(t, filepath.Join(release, "core"))
			finished := saveScheduled(t, root, filepath.Join(release, "core"), "")
			finished.State, finished.Revision = StateCommitted, finished.Revision+1
			if err := NewStore(filepath.Join(root, "journal.json")).Save(finished); err != nil {
				t.Fatal(err)
			}

			service, err := NewService(ServiceOptions{Root: root, CurrentVersion: "1.0.0"})
			if err != nil {
				t.Fatal(err)
			}
			staged, err := service.Stage(t.Context(), StageRequest{
				Name: "core", Version: "1.1.0", Binary: bytes.NewReader([]byte("candidate")),
			})
			if err != nil {
				t.Fatal(err)
			}

			_, err = os.Stat(filepath.Join(release, "core"))
			if elsewhere && err != nil {
				t.Fatalf("a file the finished journal names outside the staging folder is gone: %v", err)
			}
			if !elsewhere && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the finished release stayed: %v", err)
			}
			if _, err = os.Stat(staged.StagedBinary); err != nil {
				t.Fatalf("the new release is gone: %v", err)
			}
		})
	}
}
