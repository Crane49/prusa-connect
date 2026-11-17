package storageapi

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// stagedContent is what the object under test holds, so a delete that landed on
// something else can be told from one that landed on nothing.
const stagedContent = "G28\n"

func stage(t *testing.T, dir, name string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(stagedContent), 0o600); err != nil {
		t.Fatalf("stage %s: %v", full, err)
	}

	return full
}

func TestRemoveStagedDeletesTheNamedObject(t *testing.T) {
	t.Parallel()
	staged := stage(t, t.TempDir(), "upload.gcode")

	if err := RemoveStaged(staged); err != nil {
		t.Fatalf("RemoveStaged(%q): %v", staged, err)
	}
	if _, err := os.Lstat(staged); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("RemoveStaged left %q behind: %v", staged, err)
	}
}

func TestRemoveStagedRefusesANameThatIsNotAnObject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cases := map[string]string{
		"relative":   "upload.gcode",
		"climbing":   filepath.Join("..", "upload.gcode"),
		"volumeOnly": filepath.VolumeName(dir) + string(filepath.Separator),
	}

	for name, arg := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := RemoveStaged(arg); !errors.Is(err, ErrInvalidPath) {
				t.Errorf("RemoveStaged(%q) = %v, want ErrInvalidPath", arg, err)
			}
		})
	}
}

// A name that is already gone reports the filesystem's error rather than
// success, so a caller that is cleaning up after itself can tell "someone else
// removed it" from "the parent directory is not there any more".
func TestRemoveStagedReportsAMissingObject(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "never-written.gcode")

	err := RemoveStaged(missing)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("RemoveStaged(%q) = %v, want a not-exist error", missing, err)
	}
	if errors.Is(err, ErrInvalidPath) {
		t.Error("a missing object must not read as an invalid path")
	}
}

// A link planted at the staged name is unlinked itself. Following it would let
// whoever can write into the staging directory choose the file the host deletes
// on the next cleanup tick.
func TestRemoveStagedUnlinksALinkRatherThanItsTarget(t *testing.T) {
	t.Parallel()
	dir, outside := t.TempDir(), t.TempDir()
	target := stage(t, outside, "secret.gcode")
	link := filepath.Join(dir, "upload.gcode")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlink can be created here, so this escape is unproven on this host: %v", err)
	}

	if err := RemoveStaged(link); err != nil {
		t.Fatalf("RemoveStaged(%q): %v", link, err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the link at %q survived: %v", link, err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("RemoveStaged followed the link and removed %q: %v", target, err)
	}
}
