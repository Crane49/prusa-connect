package storageapitest

import (
	"context"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// unconfinedEnv makes the test binary run the suite against the unconfined
// provider instead of driving the child. A suite that reports a provider broken
// fails the process it runs in, so the only way to assert that it does is to
// watch a second process fail.
const unconfinedEnv = "STORAGEAPITEST_RUN_UNCONFINED"

// mustFail names the escape and probe pairs an unconfined provider has to be
// caught by. Each is a platform-independent climb: filepath.Join folds the
// ".." segments and hands back a path above the store on every operating
// system, so
// a suite that passes any of these is not checking confinement at all.
var mustFail = []string{
	"PathEscape/ClimbingRelative/Open",
	"PathEscape/ClimbingRelative/Save",
	"PathEscape/ClimbingRelative/Delete",
	"PathEscape/ClimbingRelative/RenameOff",
	"PathEscape/RootedClimb/Open",
	"PathEscape/DotDotInTheMiddle/Open",
	"PathEscape/DeepClimbToTheFilesystemRoot/Open",
}

// TestPathEscapeCatchesAnUnconfinedProvider runs the published suite against a
// provider written the way the bug is written, filepath.Join onto a root, and
// requires the suite to report it. Without this the escape cases could be
// asserting nothing and every provider would certify green.
func TestPathEscapeCatchesAnUnconfinedProvider(t *testing.T) {
	if os.Getenv(unconfinedEnv) == "1" {
		RunProviderContract(t, func(t *testing.T) Subject {
			root := t.TempDir()

			return Subject{Provider: &unconfined{root: root}, Root: root}
		})

		return
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0],
		"-test.run=TestPathEscapeCatchesAnUnconfinedProvider/PathEscape", "-test.v")
	cmd.Env = append(os.Environ(), unconfinedEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the suite passed a provider that resolves with filepath.Join:\n%s", out)
	}

	for _, want := range mustFail {
		if !strings.Contains(string(out), "--- FAIL: TestPathEscapeCatchesAnUnconfinedProvider/"+want) {
			t.Errorf("the suite did not report %s against an unconfined provider:\n%s", want, out)
		}
	}
}

// unconfined resolves every name with filepath.Join, which is the defect the
// escape cases exist to catch. It is not a reference implementation.
type unconfined struct{ root string }

func (p *unconfined) join(rel string) string {
	return filepath.Join(p.root, filepath.FromSlash(rel))
}

func (p *unconfined) Name() string        { return "unconfined" }
func (p *unconfined) DisplayName() string { return "Unconfined" }

func (p *unconfined) Capabilities() storageapi.Caps {
	return storageapi.Caps{Browse: true, Write: true}
}

func (p *unconfined) List(_ context.Context, relDir string) ([]storageapi.FileInfo, error) {
	entries, err := os.ReadDir(p.join(relDir))
	if err != nil {
		return nil, storageapi.ErrInvalidPath
	}
	out := make([]storageapi.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		out = append(out, storageapi.FileInfo{
			Name:  entry.Name(),
			Path:  filepath.ToSlash(filepath.Join(relDir, entry.Name())),
			IsDir: entry.IsDir(),
			Size:  info.Size(),
			MTime: info.ModTime().Unix(),
		})
	}

	return out, nil
}

func (p *unconfined) Stat(_ context.Context, relPath string) (storageapi.FileInfo, error) {
	info, err := os.Stat(p.join(relPath))
	if err != nil {
		return storageapi.FileInfo{}, storageapi.ErrInvalidPath
	}

	return storageapi.FileInfo{
		Name:  info.Name(),
		Path:  relPath,
		IsDir: info.IsDir(),
		Size:  info.Size(),
		MTime: info.ModTime().Unix(),
	}, nil
}

func (p *unconfined) Open(_ context.Context, relPath string) (io.ReadCloser, error) {
	f, err := os.Open(p.join(relPath))
	if err != nil {
		return nil, storageapi.ErrInvalidPath
	}

	return f, nil
}

func (p *unconfined) Create(_ context.Context, relPath string) (storageapi.Writer, error) {
	name := p.join(relPath)
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		return nil, err
	}
	f, err := os.Create(name)
	if err != nil {
		return nil, storageapi.ErrInvalidPath
	}

	return &unconfinedWrite{file: f, path: relPath, digest: storageapi.NewDigest()}, nil
}

// unconfinedWrite streams straight at the destination, which is the other half
// of what this provider is for: the escape cases need a write that actually
// lands, and the atomicity case is not among the ones it is run against.
type unconfinedWrite struct {
	file   *os.File
	path   string
	digest hash.Hash
	size   int64
	done   bool
}

func (w *unconfinedWrite) Write(p []byte) (int, error) {
	if w.done {
		return 0, storageapi.ErrWriteClosed
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	_, _ = w.digest.Write(p[:n])

	return n, err
}

func (w *unconfinedWrite) Commit(_ context.Context) (storageapi.WriteResult, error) {
	if w.done {
		return storageapi.WriteResult{}, storageapi.ErrWriteClosed
	}
	w.done = true
	if err := w.file.Close(); err != nil {
		return storageapi.WriteResult{}, err
	}

	return storageapi.WriteResult{Path: w.path, Size: w.size, SHA256: storageapi.Sum(w.digest)}, nil
}

func (w *unconfinedWrite) Abort(_ context.Context) error {
	if w.done {
		return nil
	}
	w.done = true
	_ = w.file.Close()

	return os.Remove(w.file.Name())
}

func (p *unconfined) Delete(_ context.Context, relPath string) error {
	if err := os.RemoveAll(p.join(relPath)); err != nil {
		return storageapi.ErrInvalidPath
	}

	return nil
}

func (p *unconfined) DeleteMany(ctx context.Context, relPaths []string) error {
	for _, rel := range relPaths {
		if err := p.Delete(ctx, rel); err != nil {
			return err
		}
	}

	return nil
}

func (p *unconfined) Rename(_ context.Context, from, to string) error {
	if err := os.MkdirAll(filepath.Dir(p.join(to)), 0o750); err != nil {
		return err
	}
	if err := os.Rename(p.join(from), p.join(to)); err != nil {
		return storageapi.ErrInvalidPath
	}

	return nil
}

func (p *unconfined) Mkdir(_ context.Context, relPath string) error {
	if err := os.MkdirAll(p.join(relPath), 0o750); err != nil {
		return storageapi.ErrInvalidPath
	}

	return nil
}

func (p *unconfined) OpenLocal(_ context.Context, relPath string) (storageapi.LocalFile, error) {
	f, err := os.Open(p.join(relPath))
	if err != nil {
		return nil, storageapi.ErrInvalidPath
	}
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		_ = f.Close()

		return nil, storageapi.ErrInvalidPath
	}

	return f, nil
}

func (p *unconfined) CreateLocal(_ context.Context, relPath string) (storageapi.LocalWriter, error) {
	name := p.join(relPath)
	if name == filepath.Clean(p.root) {
		return nil, storageapi.ErrInvalidPath
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, storageapi.ErrInvalidPath
	}

	return unconfinedWriter{File: f}, nil
}

// unconfinedWriter satisfies storageapi.LocalWriter, whose Name is the absolute
// path of the object the handle holds open.
type unconfinedWriter struct{ *os.File }

var (
	_ storageapi.Provider    = (*unconfined)(nil)
	_ storageapi.LocalFiles  = (*unconfined)(nil)
	_ storageapi.Renamer     = (*unconfined)(nil)
	_ storageapi.LocalWriter = unconfinedWriter{}
)
