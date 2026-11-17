package codegen_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/codegen"
)

// errStale stands in for the sentinel a generator command owns. Every command
// has one of its own, and the point of passing it in is that errors.Is at the
// call site still names the caller's gate.
var errStale = errors.New("committed artefact differs from the generated one")

// catalogue is a stand-in for the artefact struct a command publishes.
type catalogue struct {
	Comment string   `json:"$comment"`
	Entries []string `json:"entries"`
}

// TestWriteFileCreatesTheDirectoryTheArtefactSitsIn covers the first run of a
// new artefact, which is the one run an operator cannot fix by hand: the
// directory the generator was told to write into does not exist yet.
func TestWriteFileCreatesTheDirectoryTheArtefactSitsIn(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "docs", "vocabularies", "thing.json")

	if err := codegen.WriteFile(path, []byte("{}\n")); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back the artefact: %v", err)
	}

	if string(got) != "{}\n" {
		t.Errorf("the artefact holds %q, want %q", got, "{}\n")
	}
}

// TestWriteFileWritesToABareFilename holds the branch that skips MkdirAll. A
// path with no directory part resolves to ".", and creating that is not the
// generator's business.
func TestWriteFileWritesToABareFilename(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := codegen.WriteFile("thing.json", []byte("bare\n")); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := os.ReadFile("thing.json")
	if err != nil {
		t.Fatalf("reading back the artefact: %v", err)
	}

	if string(got) != "bare\n" {
		t.Errorf("the artefact holds %q, want %q", got, "bare\n")
	}
}

// TestWriteFileSaysWhichStepFailed separates the two failures. A directory that
// cannot be created and a file that cannot be written are different problems,
// and an operator reading one message has to be able to tell them apart.
func TestWriteFileSaysWhichStepFailed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory\n"), codegen.FilePerms); err != nil {
		t.Fatalf("writing the blocking file: %v", err)
	}

	err := codegen.WriteFile(filepath.Join(blocker, "nested", "thing.json"), []byte("{}\n"))
	if err == nil {
		t.Fatal("WriteFile() under a plain file returned no error")
	}

	if got := err.Error(); !strings.HasPrefix(got, "create ") {
		t.Errorf("WriteFile() error = %q, want it to start with \"create \"", got)
	}
}

// TestReadCatalogueTellsAbsentFromEmpty is the semantics the four commands that
// call it depend on: a missing file is a first run, and a file holding no rows
// is drift somebody has to answer for.
func TestReadCatalogueTellsAbsentFromEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	absent, err := codegen.ReadCatalogue[catalogue](filepath.Join(dir, "absent.json"))
	if err != nil {
		t.Fatalf("ReadCatalogue() on a missing file error = %v", err)
	}

	if absent != nil {
		t.Errorf("ReadCatalogue() on a missing file = %+v, want nil", absent)
	}

	empty := filepath.Join(dir, "empty.json")
	if err = codegen.WriteFile(empty, []byte(`{"$comment":"x","entries":[]}`)); err != nil {
		t.Fatalf("writing the empty catalogue: %v", err)
	}

	decoded, err := codegen.ReadCatalogue[catalogue](empty)
	if err != nil {
		t.Fatalf("ReadCatalogue() on an empty catalogue error = %v", err)
	}

	if decoded == nil {
		t.Fatal("ReadCatalogue() on an empty catalogue = nil, want a catalogue with no entries")
	}

	if len(decoded.Entries) != 0 {
		t.Errorf("the empty catalogue holds %d entries, want none", len(decoded.Entries))
	}
}

// TestReadCatalogueDecodesIntoTheCallersType checks that the type parameter is
// what the bytes land in, since every command names a different struct.
func TestReadCatalogueDecodesIntoTheCallersType(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "catalogue.json")
	if err := codegen.WriteFile(path, []byte(`{"$comment":"generated","entries":["one","two"]}`)); err != nil {
		t.Fatalf("writing the catalogue: %v", err)
	}

	decoded, err := codegen.ReadCatalogue[catalogue](path)
	if err != nil {
		t.Fatalf("ReadCatalogue() error = %v", err)
	}

	if decoded.Comment != "generated" {
		t.Errorf("the comment is %q, want %q", decoded.Comment, "generated")
	}

	if len(decoded.Entries) != 2 {
		t.Fatalf("the catalogue holds %d entries, want 2", len(decoded.Entries))
	}
}

// TestReadCatalogueNamesTheFileItCouldNotDecode. A generator points at several
// artefacts, so an error that says only "invalid character" leaves the operator
// guessing which one.
func TestReadCatalogueNamesTheFileItCouldNotDecode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "broken.json")
	if err := codegen.WriteFile(path, []byte("{not json")); err != nil {
		t.Fatalf("writing the broken catalogue: %v", err)
	}

	_, err := codegen.ReadCatalogue[catalogue](path)
	if err == nil {
		t.Fatal("ReadCatalogue() on invalid JSON returned no error")
	}

	if !strings.Contains(err.Error(), path) {
		t.Errorf("ReadCatalogue() error = %q, want it to name %q", err, path)
	}
}

// TestCompareFileAcceptsTheBytesTheCodeRenders is the green path: a committed
// artefact equal to what the generator would write passes the gate.
func TestCompareFileAcceptsTheBytesTheCodeRenders(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "artefact.json")

	content := []byte("{\n  \"entries\": []\n}\n")
	if err := codegen.WriteFile(path, content); err != nil {
		t.Fatalf("writing the artefact: %v", err)
	}

	if err := codegen.CompareFile(path, content, errStale, "generate-thing"); err != nil {
		t.Errorf("CompareFile() on a fresh artefact error = %v", err)
	}
}

// TestCompareFileTellsTheOperatorWhichTargetToRun pins both refusals word for
// word. The text is what a contributor sees in CI, and the six commands this
// replaced each printed one of these two sentences.
func TestCompareFileTellsTheOperatorWhichTargetToRun(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	stale := filepath.Join(dir, "stale.json")
	if err := codegen.WriteFile(stale, []byte("stale\n")); err != nil {
		t.Fatalf("writing the stale artefact: %v", err)
	}

	missing := filepath.Join(dir, "missing.json")

	cases := map[string]struct {
		path string
		want string
	}{
		"stale": {
			path: stale,
			want: "committed artefact differs from the generated one: " + stale +
				". Run make generate-thing and commit the result",
		},
		"missing": {
			path: missing,
			want: "committed artefact differs from the generated one: " + missing +
				" does not exist. Run make generate-thing and commit it",
		},
	}

	for name, each := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := codegen.CompareFile(each.path, []byte("fresh\n"), errStale, "generate-thing")
			if !errors.Is(err, errStale) {
				t.Fatalf("CompareFile() error = %v, want it to wrap the caller's sentinel", err)
			}

			if got := err.Error(); got != each.want {
				t.Errorf("CompareFile() error = %q, want %q", got, each.want)
			}
		})
	}
}

// TestCompareFileDoesNotCallAnUnreadableFileStale. A file the process cannot
// read is not a drifted artefact, and reporting it as one sends the operator to
// regenerate something that was already correct.
func TestCompareFileDoesNotCallAnUnreadableFileStale(t *testing.T) {
	t.Parallel()

	// A directory is the one path every platform here refuses to read as a
	// file, and it is not os.ErrNotExist.
	dir := t.TempDir()

	err := codegen.CompareFile(dir, []byte("fresh\n"), errStale, "generate-thing")
	if err == nil {
		t.Fatal("CompareFile() on a directory returned no error")
	}

	if errors.Is(err, errStale) {
		t.Errorf("CompareFile() on a directory reported %v, want a read failure", err)
	}
}
