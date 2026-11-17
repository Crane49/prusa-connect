package storageapitest

// The local-handle subtests. A provider that serves storageapi.LocalFiles hands
// the host an open file on its own disk, so the host serves a download and
// stages an upload without resolving the name a second time. A store that keeps
// its objects anywhere else does not implement it, and none of this runs.

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// localFileContent is what the LocalFile subtests store and read back.
const localFileContent = "G28\nG1 X0 Y0\n"

// A LocalFiles provider must open a stored object into a handle the caller can
// read and seek, and must describe that handle from the handle itself. Serving
// a file over HTTP needs all three: a length, a modification time, and a seek
// for range requests.
func testLocalFileOpens(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	save(t, p, "a.gcode", localFileContent)

	f, err := localFilesOf(t, p).OpenLocal(ctx, "a.gcode")
	if err != nil {
		t.Fatalf("OpenLocal: %v; a storageapi.LocalFiles must open a stored object", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("LocalFile.Stat: %v", err)
	}
	if info.IsDir() {
		t.Error("LocalFile.Stat reported a directory for a stored file")
	}
	if info.Size() != int64(len(localFileContent)) {
		t.Errorf("LocalFile.Stat reported %d bytes, want %d", info.Size(), len(localFileContent))
	}

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read the handle: %v", err)
	}
	if string(got) != localFileContent {
		t.Fatalf("handle read %q, want %q", got, localFileContent)
	}

	// A range request rewinds and reads again off the same handle.
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("LocalFile.Seek: %v", err)
	}
	again, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read after Seek: %v", err)
	}
	if string(again) != localFileContent {
		t.Errorf("read after Seek(0) = %q, want %q", again, localFileContent)
	}
}

// A LocalFiles provider must create an object that does not exist yet, name it
// absolutely, and let the caller see the bytes through its own surface. Staging
// an upload for a printer is exactly this and nothing else works for it.
func testLocalFileCreates(t *testing.T, p storageapi.Provider) {
	const rel = "staged/upload.gcode"
	ctx := t.Context()

	w, err := localFilesOf(t, p).CreateLocal(ctx, rel)
	if err != nil {
		t.Fatalf("CreateLocal(%q): %v; a storageapi.LocalFiles must create a "+
			"path that does not exist yet, parent directories included", rel, err)
	}
	if _, err = io.WriteString(w, localFileContent); err != nil {
		t.Fatalf("write the handle: %v", err)
	}
	// Close is where a full disk reports itself, so the contract requires it to
	// be reportable rather than swallowed by the provider.
	if err = w.Close(); err != nil {
		t.Fatalf("LocalWriter.Close: %v", err)
	}

	name := w.Name()
	if !filepath.IsAbs(name) {
		t.Errorf("LocalWriter.Name must be an absolute path, got %q", name)
	}

	// The object must be readable back through the provider's own surface,
	// which is what proves CreateLocal wrote into the store and not beside it.
	info, err := p.Stat(ctx, rel)
	if err != nil {
		t.Fatalf("Stat(%q) after CreateLocal: %v", rel, err)
	}
	if info.Size != int64(len(localFileContent)) {
		t.Errorf("Stat(%q) reports %d bytes, want %d", rel, info.Size, len(localFileContent))
	}

	// Name and rel must be the same object: deleting through the provider must
	// take the named file with it. A provider whose Name points somewhere else
	// hands its caller a cleanup path that deletes the wrong file.
	if _, err = os.Stat(name); err != nil {
		t.Fatalf("LocalWriter.Name = %q, which does not exist: %v", name, err)
	}
	if err = p.Delete(ctx, rel); err != nil {
		t.Fatalf("Delete(%q): %v", rel, err)
	}
	if _, err = os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Delete(%q) left %q behind, so LocalWriter.Name named a different object", rel, name)
	}
}

// The store root names no object to open or create. A provider that resolves it
// hands a caller a directory handle where a file was promised, and a create
// that reaches it is a create aimed at the whole store.
func testLocalFileRejectsRoot(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	local := localFilesOf(t, p)
	for _, rel := range []string{"", ".", "/"} {
		if f, err := local.OpenLocal(ctx, rel); err == nil {
			_ = f.Close()
			t.Errorf("OpenLocal(%q) resolved the store root", rel)
		} else if !errors.Is(err, storageapi.ErrInvalidPath) {
			t.Errorf("OpenLocal(%q) = %v, want ErrInvalidPath", rel, err)
		}
		if w, err := local.CreateLocal(ctx, rel); err == nil {
			_ = w.Close()
			t.Errorf("CreateLocal(%q) resolved the store root", rel)
		} else if !errors.Is(err, storageapi.ErrInvalidPath) {
			t.Errorf("CreateLocal(%q) = %v, want ErrInvalidPath", rel, err)
		}
	}
}

// A name that is not in the store reports ErrInvalidPath, the same error a name
// that only resolves by leaving the store reports. A provider that tells them
// apart hands a caller an oracle for which names point out of it, and it must
// not report ErrNoLocalFile either: that would read as "not filesystem-backed"
// from a provider that is.
func testLocalFileRejectsMissing(t *testing.T, p storageapi.Provider) {
	f, err := localFilesOf(t, p).OpenLocal(t.Context(), "nope.gcode")
	if err == nil {
		_ = f.Close()
		t.Fatal("OpenLocal of a missing path returned a handle")
	}
	if !errors.Is(err, storageapi.ErrInvalidPath) {
		t.Errorf("OpenLocal of a missing path = %v, want ErrInvalidPath", err)
	}
	if errors.Is(err, storageapi.ErrNoLocalFile) {
		t.Error("OpenLocal reported ErrNoLocalFile from a provider implementing LocalFiles")
	}
}

// rangeContent is what the range subtests store: long enough that a provider
// ignoring the offset returns something visibly different.
