package files_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/prusa3d/fractal-link/files"
)

// openShares opens the shared folders in dir and fails the test on an error.
func openShares(t *testing.T, dir string) *files.Shares {
	t.Helper()

	shares, err := files.OpenShares(dir)
	if err != nil {
		t.Fatalf("open the shared folders: %v", err)
	}

	return shares
}

// held is what a Shares holds, as one value a test compares whole.
type held struct {
	Shares   []files.Share
	Storages []string
}

func heldBy(s *files.Shares) held { return held{Shares: s.List(), Storages: s.Storages()} }

// TestSharesSurviveRestart: what Share and Unshare wrote is there after the
// module opens the directory again.
func TestSharesSurviveRestart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	shares := openShares(t, dir)

	for _, step := range []func() error{
		func() error { return shares.Share("local", "jobs/benchy") },
		func() error { return shares.Share("usb", "/prints/") },
		func() error { return shares.Share("local", "jobs") },
		func() error { return shares.Unshare("local", "jobs/benchy") },
		func() error { return shares.Unshare("usb", "prints") },
	} {
		if err := step(); err != nil {
			t.Fatalf("change the shared folders: %v", err)
		}
	}

	want := held{
		Shares:   []files.Share{{Storage: "local", Path: "jobs"}},
		Storages: []string{"local", "usb"},
	}
	if got := heldBy(openShares(t, dir)); !reflect.DeepEqual(got, want) {
		t.Errorf("after a restart the module holds %+v, want %+v", got, want)
	}
}

// TestRootShareRefused: the root folder of a storage cannot be shared, in any
// spelling.
func TestRootShareRefused(t *testing.T) {
	t.Parallel()

	shares := openShares(t, t.TempDir())

	for _, root := range []string{"", "/", ".", "a/.."} {
		if err := shares.Share("local", root); !errors.Is(err, files.ErrRootShare) {
			t.Errorf("Share of %q returned %v, want ErrRootShare", root, err)
		}
	}

	if err := shares.Share("", "jobs"); !errors.Is(err, files.ErrBadShare) {
		t.Errorf("Share with no storage returned %v, want ErrBadShare", err)
	}
}

// TestOddShareRefused: names the file cannot hold back unchanged are refused, so
// whatever is accepted can be removed again.
func TestOddShareRefused(t *testing.T) {
	t.Parallel()

	shares := openShares(t, t.TempDir())

	for _, odd := range []struct{ storage, path string }{
		{"local", "a\x00b"},
		{"local", "a\x1fb"},
		{"local", "\xff"},
		{"a/b", "jobs"},
		{"lo\x00cal", "jobs"},
		{"\xff", "jobs"},
	} {
		if err := shares.Share(odd.storage, odd.path); !errors.Is(err, files.ErrBadShare) {
			t.Errorf("Share of %q in %q returned %v, want ErrBadShare", odd.path, odd.storage, err)
		}

		if err := shares.Unshare(odd.storage, odd.path); !errors.Is(err, files.ErrBadShare) {
			t.Errorf("Unshare of %q in %q returned %v, want ErrBadShare", odd.path, odd.storage, err)
		}
	}

	if got := heldBy(shares); !reflect.DeepEqual(got, held{}) {
		t.Errorf("the module holds %+v after refusals, want nothing", got)
	}
}

// TestPathCannotLeaveStorage: a path with dot-segments is kept inside the
// storage.
func TestPathCannotLeaveStorage(t *testing.T) {
	t.Parallel()

	shares := openShares(t, t.TempDir())
	if err := shares.Share("local", "..\\..\\../etc"); err != nil {
		t.Fatalf("Share: %v", err)
	}

	want := []files.Share{{Storage: "local", Path: "etc"}}
	if got := shares.List(); !reflect.DeepEqual(got, want) {
		t.Errorf("the shared folders are %+v, want %+v", got, want)
	}
}

// TestChangedSignalsOnce: a change sends one signal, and a call that changes
// nothing sends none.
func TestChangedSignalsOnce(t *testing.T) {
	t.Parallel()

	shares := openShares(t, t.TempDir())
	if err := shares.Share("local", "jobs"); err != nil {
		t.Fatalf("Share: %v", err)
	}

	select {
	case <-shares.Changed():
	default:
		t.Fatal("Share sent no signal")
	}

	if err := shares.Share("local", "jobs"); err != nil {
		t.Fatalf("Share again: %v", err)
	}

	if err := shares.Unshare("local", "other"); err != nil {
		t.Fatalf("Unshare of a folder not shared: %v", err)
	}

	select {
	case <-shares.Changed():
		t.Error("a call that changed nothing sent a signal")
	default:
	}
}

// TestFailedWriteKeepsShares: when the file cannot be written, the set in
// memory and on disk stays as it was.
func TestFailedWriteKeepsShares(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	shares := openShares(t, dir)
	if err := shares.Share("local", "jobs"); err != nil {
		t.Fatalf("Share: %v", err)
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("make the directory read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := shares.Share("local", "more"); err == nil {
		t.Skip("this user writes into a read-only directory, so the failure cannot be made")
	}

	want := []files.Share{{Storage: "local", Path: "jobs"}}
	if got := shares.List(); !reflect.DeepEqual(got, want) {
		t.Errorf("after a failed write the module holds %+v, want %+v", got, want)
	}

	if entries, _ := filepath.Glob(filepath.Join(dir, "files-shares.json.*")); len(entries) != 0 {
		t.Errorf("a failed write left %v behind", entries)
	}
}

// TestAcceptedShareIsRemovable: what Share accepted, Unshare removes after a
// restart.
func TestAcceptedShareIsRemovable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := openShares(t, dir).Share("local", "jobs/é"); err != nil {
		t.Fatalf("Share: %v", err)
	}

	reopened := openShares(t, dir)
	if err := reopened.Unshare("local", "jobs/é"); err != nil {
		t.Fatalf("Unshare: %v", err)
	}

	if got := reopened.List(); len(got) != 0 {
		t.Errorf("the module still shares %+v", got)
	}
}
