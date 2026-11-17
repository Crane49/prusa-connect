package storageapitest

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// outOfRootContent fills the file every escape subtest aims at. Reading it back
// through the provider proves the escape.
const outOfRootContent = "OUT-OF-ROOT MARKER"

// escapePayload is what a write attempts to put through a link. Finding it in
// the out-of-root file proves the write escaped.
const escapePayload = "G1 WRITTEN THROUGH THE LINK"

// escapeRoute is one way a link inside a provider's store points out of it.
// plant creates the route under root and returns the relative path a caller
// addresses it by; it skips the subtest when the platform will not create that
// kind of link, because an escape that cannot be planted cannot be proven.
type escapeRoute struct {
	name      string
	linkedDir bool // the link is a directory in the middle of the path, not the leaf
	plant     func(t *testing.T, root, outsideDir, outsideFile string) string
}

// escapeProbe is one operation that must not reach through a route.
type escapeProbe struct {
	name    string
	dirOnly bool // only meaningful when the link is a directory in the path
	needs   func(caps storageapi.Caps) bool
	check   func(t *testing.T, s Subject, rel, outsideFile string)
}

func escapeRoutes() []escapeRoute {
	return []escapeRoute{
		{
			name: "AbsoluteSymlinkAtLeaf",
			plant: func(t *testing.T, root, _, outsideFile string) string {
				symlinkOrSkip(t, outsideFile, filepath.Join(root, "escape.gcode"))
				return "escape.gcode"
			},
		},
		{
			name: "RelativeSymlinkAtLeaf",
			plant: func(t *testing.T, root, _, outsideFile string) string {
				link := filepath.Join(root, "relative.gcode")
				target, err := filepath.Rel(filepath.Dir(link), outsideFile)
				if err != nil {
					t.Fatalf("relative target: %v", err)
				}
				symlinkOrSkip(t, target, link)
				return "relative.gcode"
			},
		},
		{
			name: "SymlinkInSubdirectory",
			plant: func(t *testing.T, root, _, outsideFile string) string {
				if err := os.MkdirAll(filepath.Join(root, "sub"), 0o750); err != nil {
					t.Fatalf("mkdir sub: %v", err)
				}
				symlinkOrSkip(t, outsideFile, filepath.Join(root, "sub", "escape.gcode"))
				return "sub/escape.gcode"
			},
		},
		{
			name:      "SymlinkedDirectoryInPath",
			linkedDir: true,
			plant: func(t *testing.T, root, outsideDir, _ string) string {
				symlinkOrSkip(t, outsideDir, filepath.Join(root, "linked"))
				return "linked/secret.gcode"
			},
		},
		{
			name:      "JunctionInPath",
			linkedDir: true,
			plant: func(t *testing.T, root, outsideDir, _ string) string {
				junctionOrSkip(t, outsideDir, filepath.Join(root, "junction"))
				return "junction/secret.gcode"
			},
		},
	}
}

func escapeProbes() []escapeProbe {
	return []escapeProbe{
		{name: "Open", check: probeOpen},
		{name: "Stat", check: probeStat},
		{name: "Save", check: probeSave},
		{name: "Delete", check: probeDelete},
		{
			name:  "Rename",
			needs: func(caps storageapi.Caps) bool { return caps.Rename },
			check: probeRename,
		},
		{name: "List", dirOnly: true, check: probeList},
		{name: "Mkdir", dirOnly: true, check: probeMkdir},
		{
			name:  "OpenLocal",
			needs: func(caps storageapi.Caps) bool { return caps.LocalFile },
			check: probeOpenLocal,
		},
		{
			name:  "CreateLocal",
			needs: func(caps storageapi.Caps) bool { return caps.LocalFile },
			check: probeCreateLocal,
		},
	}
}

// testLinkEscape plants each kind of out-of-root link inside the provider's
// store and requires every operation to refuse it. Each probe gets a freshly
// planted route on a fresh provider, so a mutating probe cannot mask the next.
//
// Hardlinks and bind mounts are deliberately not modelled: neither is a step in
// path resolution, so no provider can be held to refusing them. Both were
// reproduced against the built-in provider and recorded as limits in ADR-20260825-smx.
//
// A name that points out of the store without any link is PathEscape's job.
func testLinkEscape(t *testing.T, newSubject MakeSubject, caps storageapi.Caps) {
	t.Helper()
	for _, route := range escapeRoutes() {
		t.Run(route.name, func(t *testing.T) {
			for _, probe := range escapeProbes() {
				if probe.dirOnly && !route.linkedDir {
					continue
				}
				if probe.needs != nil && !probe.needs(caps) {
					continue
				}
				t.Run(probe.name, func(t *testing.T) {
					s := newSubject(t)
					if s.NoFilesystemRoot {
						t.Skip("Subject.NoFilesystemRoot is set: no filesystem store to plant a link in")
					}
					outsideDir, outsideFile := plantTarget(t, s.Root)
					rel := route.plant(t, s.Root, outsideDir, outsideFile)
					probe.check(t, s, rel, outsideFile)
				})
			}
		})
	}
}

// plantTarget writes the out-of-root file the escapes aim at and returns its
// directory and its path. It refuses to run if that location is inside the
// store, since the whole test would then prove nothing.
func plantTarget(t *testing.T, root string) (dir, file string) {
	t.Helper()
	dir = t.TempDir()
	rel, err := filepath.Rel(root, dir)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("the out-of-root location %q is inside the store %q, "+
			"so an escape could not be told from a normal read", dir, root)
	}
	file = filepath.Join(dir, "secret.gcode")
	if err = os.WriteFile(file, []byte(outOfRootContent), 0o600); err != nil {
		t.Fatalf("write out-of-root file: %v", err)
	}
	return dir, file
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlink can be created here, so this escape is unproven on this host: %v", err)
	}
}

// junctionOrSkip creates a Windows directory junction, the reparse point an
// unprivileged user can make when symlinks need Developer Mode or elevation.
// The stdlib has no call for it, so mklink does the work.
func junctionOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("directory junctions exist only on Windows")
	}
	cmd := exec.CommandContext(t.Context(), "cmd", "/c", "mklink", "/J", link, target)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("no junction can be created here, so this escape is unproven on this host: %v: %s", err, out)
	}
}

func probeOpen(t *testing.T, s Subject, rel, _ string) {
	rc, err := s.Provider.Open(t.Context(), rel)
	if err != nil {
		requireInvalidPath(t, "Open", rel, err)
		return
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	t.Errorf("Open(%q) followed a link out of the store and returned %q", rel, got)
}

func probeStat(t *testing.T, s Subject, rel, _ string) {
	info, err := s.Provider.Stat(t.Context(), rel)
	if err == nil {
		t.Errorf("Stat(%q) reported an out-of-root object: %+v", rel, info)
		return
	}
	requireInvalidPath(t, "Stat", rel, err)
}

// probeSave drives two writes: one at the link, which would overwrite what it
// points at, and one below it, which makes the provider create the parent
// directory. The second is the one a check on the destination file alone cannot
// see, because the tree lands outside the store whether or not the write does.
func probeSave(t *testing.T, s Subject, rel, outsideFile string) {
	outsideDir := filepath.Dir(outsideFile)
	before := outsideTree(t, outsideDir)

	_, _ = storageapi.Save(t.Context(), s.Provider, rel, strings.NewReader(escapePayload))
	requireTargetIntact(t, "Save", rel, outsideFile)

	nested := path.Dir(rel) + "/planted/new.gcode"
	_, _ = storageapi.Save(t.Context(), s.Provider, nested, strings.NewReader(escapePayload))
	requireTargetIntact(t, "Save", nested, outsideFile)
	requireNothingPlanted(t, "Save", nested, outsideDir, before)
}

func probeDelete(t *testing.T, s Subject, rel, outsideFile string) {
	_ = s.Provider.Delete(t.Context(), rel)
	requireTargetIntact(t, "Delete", rel, outsideFile)
}

// probeRename drives a move in both directions: onto the link, which would
// overwrite whatever it points at, and off it, which would drag an out-of-root
// object into the store and unlink the original.
func probeRename(t *testing.T, s Subject, rel, outsideFile string) {
	outsideDir := filepath.Dir(outsideFile)
	before := outsideTree(t, outsideDir)
	renamer := renamerOf(t, s.Provider)

	save(t, s.Provider, "mover.gcode", "G1 MOVER")
	_ = renamer.Rename(t.Context(), "mover.gcode", rel)
	requireTargetIntact(t, "Rename onto", rel, outsideFile)

	// A destination below the link makes the provider create the parent
	// directory, which lands outside the store even when the move itself fails.
	nested := path.Dir(rel) + "/planted/moved.gcode"
	_ = renamer.Rename(t.Context(), "mover.gcode", nested)
	requireTargetIntact(t, "Rename onto", nested, outsideFile)
	requireNothingPlanted(t, "Rename onto", nested, outsideDir, before)

	_ = renamer.Rename(t.Context(), rel, "captured.gcode")
	requireTargetIntact(t, "Rename off", rel, outsideFile)
	requireNothingPlanted(t, "Rename off", rel, outsideDir, before)
}

// probeMkdir checks the one mutation that creates rather than replaces: a
// directory made through a linked path lands outside the store.
func probeMkdir(t *testing.T, s Subject, rel, outsideFile string) {
	outsideDir := filepath.Dir(outsideFile)
	before := outsideTree(t, outsideDir)
	planted := path.Dir(rel) + "/planted"
	_ = s.Provider.Mkdir(t.Context(), planted)
	requireNothingPlanted(t, "Mkdir", planted, outsideDir, before)
}

func probeList(t *testing.T, s Subject, rel, _ string) {
	dir := path.Dir(rel)
	entries, err := s.Provider.List(t.Context(), dir)
	if err == nil {
		t.Errorf("List(%q) enumerated an out-of-root directory: %+v", dir, entries)
		return
	}
	requireInvalidPath(t, "List", dir, err)
}

// probeOpenLocal checks the read handle a caller serves straight off disk. A
// provider that opens through a link has handed its caller the out-of-root
// object itself, which no check on a returned path could have caught.
func probeOpenLocal(t *testing.T, s Subject, rel, outsideFile string) {
	f, err := localFilesOf(t, s.Provider).OpenLocal(t.Context(), rel)
	if err != nil {
		requireInvalidPath(t, "OpenLocal", rel, err)
		return
	}
	got, _ := io.ReadAll(f)
	_ = f.Close()
	if string(got) == outOfRootContent {
		t.Errorf("OpenLocal(%q) opened the out-of-root file %s and read %q", rel, outsideFile, got)
		return
	}
	t.Errorf("OpenLocal(%q) followed a link out of the store", rel)
}

// probeCreateLocal drives two creations: one at the link, which would truncate
// what it points at, and one below it, which makes the provider create the
// parent directory. The second is the one a check on the destination alone
// cannot see, because the tree lands outside the store either way.
func probeCreateLocal(t *testing.T, s Subject, rel, outsideFile string) {
	outsideDir := filepath.Dir(outsideFile)
	before := outsideTree(t, outsideDir)

	writeThrough(t, s, rel)
	requireTargetIntact(t, "CreateLocal", rel, outsideFile)

	nested := path.Dir(rel) + "/planted/new.gcode"
	writeThrough(t, s, nested)
	requireTargetIntact(t, "CreateLocal", nested, outsideFile)
	requireNothingPlanted(t, "CreateLocal", nested, outsideDir, before)
}

// writeThrough attempts the whole create-write-close cycle, because a provider
// that refuses at Close has still truncated the object it opened.
func writeThrough(t *testing.T, s Subject, rel string) {
	t.Helper()
	w, err := localFilesOf(t, s.Provider).CreateLocal(t.Context(), rel)
	if err != nil {
		requireInvalidPath(t, "CreateLocal", rel, err)
		return
	}
	_, _ = io.WriteString(w, escapePayload)
	_ = w.Close()
}

// requireInvalidPath holds a refusal to the contract's error. A provider that
// reports "escapes the root" apart from "not found" lets a caller walk its
// store asking which names point out of it, and callers that map errors to a
// status turn the difference into a 500 for what is a client mistake.
func requireInvalidPath(t *testing.T, op, rel string, err error) {
	t.Helper()
	if !errors.Is(err, storageapi.ErrInvalidPath) {
		t.Errorf("%s(%q) refused with %v, want ErrInvalidPath", op, rel, err)
	}
}

// outsideTree lists every path under dir, so a probe can require that a refused
// operation created nothing there.
func outsideTree(t *testing.T, dir string) map[string]bool {
	t.Helper()
	tree := map[string]bool{}
	err := filepath.WalkDir(dir, func(p string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		tree[p] = true
		return nil
	})
	if err != nil {
		t.Fatalf("read the out-of-root location %s: %v", dir, err)
	}
	return tree
}

// requireNothingPlanted reports an escape by what it left behind. An operation
// that refuses its caller and still creates a file or a directory outside the
// store has written wherever the server process can write, which no check on
// the operation's own target can see.
func requireNothingPlanted(t *testing.T, op, rel, dir string, before map[string]bool) {
	t.Helper()
	for p := range outsideTree(t, dir) {
		if !before[p] {
			t.Errorf("%s(%q) created %s, outside the store", op, rel, p)
		}
	}
}

// requireTargetIntact reports an escape by its effect: the out-of-root file is
// still there and still holds what it held before the call.
func requireTargetIntact(t *testing.T, op, rel, outsideFile string) {
	t.Helper()
	got, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("%s(%q) reached out of the store and removed %s: %v", op, rel, outsideFile, err)
	}
	if string(got) != outOfRootContent {
		t.Fatalf("%s(%q) reached out of the store and overwrote %s: got %q, want %q",
			op, rel, outsideFile, got, outOfRootContent)
	}
}
