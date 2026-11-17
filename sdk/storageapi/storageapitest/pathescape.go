package storageapitest

import (
	"io"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// climbDepth is how far a deep-climbing escape reaches above the store. It is
// larger than any plausible nesting of a store root, so the path leaves the
// store however deep the root is and lands at the filesystem root.
const climbDepth = 16

// pathEscape is one caller-supplied string that names something outside the
// store without a link being involved. Nothing is planted on disk for these:
// the escape is in the name, so a provider that assembles a path with
// filepath.Join is reached by them and a provider that resolves under a root is
// not.
type pathEscape struct {
	name  string
	build func(t *testing.T, root, outsideFile string) string
}

func pathEscapes() []pathEscape {
	return []pathEscape{
		{
			name: "Absolute",
			build: func(_ *testing.T, _, outsideFile string) string {
				return filepath.ToSlash(outsideFile)
			},
		},
		{
			name: "AbsoluteBackslash",
			build: func(_ *testing.T, _, outsideFile string) string {
				return strings.ReplaceAll(filepath.ToSlash(outsideFile), "/", `\`)
			},
		},
		{
			name:  "ClimbingRelative",
			build: climbTo,
		},
		{
			name: "ClimbingBackslash",
			build: func(t *testing.T, root, outsideFile string) string {
				return strings.ReplaceAll(climbTo(t, root, outsideFile), "/", `\`)
			},
		},
		{
			name: "RootedClimb",
			build: func(t *testing.T, root, outsideFile string) string {
				return "/" + climbTo(t, root, outsideFile)
			},
		},
		{
			name: "DotDotInTheMiddle",
			build: func(t *testing.T, root, outsideFile string) string {
				return "models/nested/../../" + climbTo(t, root, outsideFile)
			},
		},
		{
			name: "DeepClimbToTheFilesystemRoot",
			build: func(_ *testing.T, _, outsideFile string) string {
				tail := filepath.ToSlash(strings.TrimPrefix(outsideFile, filepath.VolumeName(outsideFile)))

				return strings.Repeat("../", climbDepth) + strings.TrimPrefix(tail, "/")
			},
		},
	}
}

// climbTo is the shortest "/"-separated route from the store root to the
// out-of-root file, which is what a caller writes when it knows where the store
// sits and wants the file next to it.
func climbTo(t *testing.T, root, outsideFile string) string {
	t.Helper()
	rel, err := filepath.Rel(root, outsideFile)
	if err != nil {
		t.Skipf("no relative route from %s to %s on this host: %v", root, outsideFile, err)
	}

	return filepath.ToSlash(rel)
}

// pathProbe is one operation driven with an escaping name.
type pathProbe struct {
	name  string
	needs func(caps storageapi.Caps) bool
	check func(t *testing.T, s Subject, rel, outsideFile string)
}

func pathProbes() []pathProbe {
	return []pathProbe{
		{name: "Open", check: probePathOpen},
		{name: "Stat", check: probePathStat},
		{name: "List", check: probePathList},
		{name: "Save", check: probePathSave},
		{name: "Delete", check: probePathDelete},
		{name: "DeleteMany", check: probePathDeleteMany},
		{
			name:  "RenameOnto",
			needs: func(caps storageapi.Caps) bool { return caps.Rename },
			check: probePathRenameOnto,
		},
		{
			name:  "RenameOff",
			needs: func(caps storageapi.Caps) bool { return caps.Rename },
			check: probePathRenameOff,
		},
		{name: "Mkdir", check: probePathMkdir},
		{
			name:  "OpenLocal",
			needs: func(caps storageapi.Caps) bool { return caps.LocalFile },
			check: probePathOpenLocal,
		},
		{
			name:  "CreateLocal",
			needs: func(caps storageapi.Caps) bool { return caps.LocalFile },
			check: probePathCreateLocal,
		},
	}
}

// testPathEscape drives every operation with a name that points out of the
// store on its own. The contract folds a climbing name back inside the store, so
// these do not require a refusal: they require that nothing outside the store is
// read, written, created or removed, whatever the call returns.
//
// Each probe gets a fresh provider and a freshly planted out-of-root file, so a
// mutating probe cannot mask the next one.
func testPathEscape(t *testing.T, newSubject MakeSubject, caps storageapi.Caps) {
	t.Helper()
	for _, escape := range pathEscapes() {
		t.Run(escape.name, func(t *testing.T) {
			for _, probe := range pathProbes() {
				if probe.needs != nil && !probe.needs(caps) {
					continue
				}
				t.Run(probe.name, func(t *testing.T) {
					s := newSubject(t)
					if s.NoFilesystemRoot {
						t.Skip("Subject.NoFilesystemRoot is set: nothing outside the store to aim at")
					}
					_, outsideFile := plantTarget(t, s.Root)
					probe.check(t, s, escape.build(t, s.Root, outsideFile), outsideFile)
				})
			}
		})
	}
}

func probePathOpen(t *testing.T, s Subject, rel, _ string) {
	rc, err := s.Provider.Open(t.Context(), rel)
	if err != nil {
		return
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	requireNotTheOutOfRootFile(t, "Open", rel, got)
}

func probePathStat(t *testing.T, s Subject, rel, _ string) {
	info, err := s.Provider.Stat(t.Context(), rel)
	if err != nil {
		return
	}
	requireContainedPath(t, "Stat", rel, info)
}

// probePathList checks both what the listing enumerates and what it calls the
// entries. A provider that lists the out-of-root directory has published its
// contents; one that lists correctly but reports an escaping Path has handed the
// caller a name that walks out of the store on the next call.
func probePathList(t *testing.T, s Subject, rel, outsideFile string) {
	entries, err := s.Provider.List(t.Context(), path.Dir(rel))
	if err != nil {
		return
	}
	if _, found := find(entries, filepath.Base(outsideFile)); found {
		t.Errorf("List(%q) enumerated the out-of-root directory holding %s", path.Dir(rel), outsideFile)
	}
	for _, entry := range entries {
		requireContainedPath(t, "List", rel, entry)
	}
}

func probePathSave(t *testing.T, s Subject, rel, outsideFile string) {
	outsideDir := filepath.Dir(outsideFile)
	before := outsideTree(t, outsideDir)

	_, _ = storageapi.Save(t.Context(), s.Provider, rel, strings.NewReader(escapePayload))
	requireTargetIntact(t, "Save", rel, outsideFile)
	requireNothingPlanted(t, "Save", rel, outsideDir, before)
}

func probePathDelete(t *testing.T, s Subject, rel, outsideFile string) {
	_ = s.Provider.Delete(t.Context(), rel)
	requireTargetIntact(t, "Delete", rel, outsideFile)
}

// probePathDeleteMany drives the escape in a batch beside a real entry, because
// a provider that abandons the batch on the first refusal would pass a probe
// that only checked the escape.
func probePathDeleteMany(t *testing.T, s Subject, rel, outsideFile string) {
	save(t, s.Provider, "keeper.gcode", "G1 KEEPER")

	_ = s.Provider.DeleteMany(t.Context(), []string{rel, "keeper.gcode"})
	requireTargetIntact(t, "DeleteMany", rel, outsideFile)
	if _, err := s.Provider.Stat(t.Context(), "keeper.gcode"); err == nil {
		t.Errorf("DeleteMany([%q, keeper.gcode]) left the in-store entry behind", rel)
	}
}

func probePathRenameOnto(t *testing.T, s Subject, rel, outsideFile string) {
	outsideDir := filepath.Dir(outsideFile)
	before := outsideTree(t, outsideDir)

	save(t, s.Provider, "mover.gcode", "G1 MOVER")
	_ = renamerOf(t, s.Provider).Rename(t.Context(), "mover.gcode", rel)
	requireTargetIntact(t, "Rename onto", rel, outsideFile)
	requireNothingPlanted(t, "Rename onto", rel, outsideDir, before)
}

// probePathRenameOff moves in the other direction, which is the one that drags
// an object the caller never had into the store and unlinks it where it stood.
func probePathRenameOff(t *testing.T, s Subject, rel, outsideFile string) {
	outsideDir := filepath.Dir(outsideFile)
	before := outsideTree(t, outsideDir)

	_ = renamerOf(t, s.Provider).Rename(t.Context(), rel, "captured.gcode")
	requireTargetIntact(t, "Rename off", rel, outsideFile)
	requireNothingPlanted(t, "Rename off", rel, outsideDir, before)

	rc, err := s.Provider.Open(t.Context(), "captured.gcode")
	if err != nil {
		return
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	requireNotTheOutOfRootFile(t, "Rename off", rel, got)
}

func probePathMkdir(t *testing.T, s Subject, rel, outsideFile string) {
	outsideDir := filepath.Dir(outsideFile)
	before := outsideTree(t, outsideDir)

	planted := path.Dir(filepath.ToSlash(rel)) + "/planted"
	_ = s.Provider.Mkdir(t.Context(), planted)
	requireNothingPlanted(t, "Mkdir", planted, outsideDir, before)
}

func probePathOpenLocal(t *testing.T, s Subject, rel, _ string) {
	f, err := localFilesOf(t, s.Provider).OpenLocal(t.Context(), rel)
	if err != nil {
		return
	}
	got, _ := io.ReadAll(f)
	_ = f.Close()
	requireNotTheOutOfRootFile(t, "OpenLocal", rel, got)
}

func probePathCreateLocal(t *testing.T, s Subject, rel, outsideFile string) {
	outsideDir := filepath.Dir(outsideFile)
	before := outsideTree(t, outsideDir)

	writeThrough(t, s, rel)
	requireTargetIntact(t, "CreateLocal", rel, outsideFile)
	requireNothingPlanted(t, "CreateLocal", rel, outsideDir, before)
}

// requireNotTheOutOfRootFile reports a read that came back with the bytes only
// the out-of-root file holds. A name folded back into the store may legitimately
// resolve to something, so what is checked is where the bytes came from.
func requireNotTheOutOfRootFile(t *testing.T, op, rel string, got []byte) {
	t.Helper()
	if string(got) == outOfRootContent {
		t.Errorf("%s(%q) read the file outside the store", op, rel)
	}
}

// requireContainedPath holds a returned entry to the contract's own vocabulary:
// a "/"-separated path relative to the store root. A provider that echoes an
// absolute or climbing name back has handed its caller a path that leaves the
// store the next time it is used, and a UI that renders it has published where
// the store sits on disk.
func requireContainedPath(t *testing.T, op, rel string, info storageapi.FileInfo) {
	t.Helper()
	if filepath.VolumeName(filepath.FromSlash(info.Path)) != "" {
		t.Errorf("%s(%q) reported Path %q, which names a volume", op, rel, info.Path)
		return
	}
	if info.Path != storageapi.CleanRelPath(info.Path) {
		t.Errorf("%s(%q) reported Path %q, which is not relative to the store root", op, rel, info.Path)
	}
}
