package wirehost_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/extension/wirehost"
)

// TestTheArtefactIsNamedForThePlatformTheHostRunsOn covers the file naming that
// lets one install directory hold every platform's build.
func TestTheArtefactIsNamedForThePlatformTheHostRunsOn(t *testing.T) {
	t.Parallel()

	got := wirehost.Artefact(filepath.Join("plugins", "bambu"), "bambu")

	want := "bambu_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		want += ".exe"
	}

	if filepath.Base(got) != want {
		t.Errorf("the artefact is %q, and this host launches %q", filepath.Base(got), want)
	}

	if dir := filepath.Dir(got); dir != filepath.Join("plugins", "bambu") {
		t.Errorf("the artefact is in %q, and the operator named plugins/bambu", dir)
	}
}

// TestADirectoryHoldingAnotherPlatformsBuildIsRefused covers an operator who
// installed the wrong release. The refusal names the file this host wanted,
// because the operator already knows the directory.
func TestADirectoryHoldingAnotherPlatformsBuildIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// A build for another platform, so the case is about the file name and not
	// about an empty directory.
	elsewhere := filepath.Join(dir, "bambu_plan9_mips")
	if err := os.WriteFile(elsewhere, []byte("not for this host"), 0o600); err != nil {
		t.Fatalf("write %s: %v", elsewhere, err)
	}

	_, err := wirehost.Find(dir, "bambu")
	if !errors.Is(err, wirehost.ErrArtefactMissing) {
		t.Fatalf("the refusal is %v, and a missing artefact is %v", err, wirehost.ErrArtefactMissing)
	}

	if want := filepath.Base(wirehost.Artefact(dir, "bambu")); !strings.Contains(err.Error(), want) {
		t.Errorf("the refusal is %q, and it names the file this host wanted, %q", err, want)
	}
}

// TestADirectoryWhereTheArtefactShouldBeIsRefused covers an operator who
// installed an extension one directory too deep. A host should not discover
// that it is about to launch a directory from exec.
func TestADirectoryWhereTheArtefactShouldBeIsRefused(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	if err := os.MkdirAll(wirehost.Artefact(dir, "bambu"), 0o750); err != nil {
		t.Fatalf("make the directory: %v", err)
	}

	_, err := wirehost.Find(dir, "bambu")
	if !errors.Is(err, wirehost.ErrArtefactMissing) {
		t.Fatalf("the refusal is %v, and a missing artefact is %v", err, wirehost.ErrArtefactMissing)
	}
}

// TestTheInstalledArtefactIsFound is the other direction, so the refusals above
// cannot pass because Find refuses everything.
func TestTheInstalledArtefactIsFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	path := wirehost.Artefact(dir, "bambu")
	if err := os.WriteFile(path, []byte("an installed build"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	found, err := wirehost.Find(dir, "bambu")
	if err != nil {
		t.Fatalf("the installed artefact was not found: %v", err)
	}

	if found != path {
		t.Errorf("Find returned %q, and the artefact is at %q", found, path)
	}
}
