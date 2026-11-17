// Package storageapitest provides a reusable conformance ("contract") test suite
// for storageapi.Provider implementations, so every provider behaves
// consistently against the contract.
//
// The suite reads storageapi.Probe, not Capabilities. A provider declares only
// whether its browse half and its write half return real data; the four
// optional abilities come from the interfaces it implements, so the shape the
// suite runs is the shape the compiler already agreed to.
//
// Usage from a provider's _test.go:
//
//	func TestContract(t *testing.T) {
//	    storageapitest.RunProviderContract(t, func(t *testing.T) storageapitest.Subject {
//	        p, err := storageapi.BuildProvider(storageapi.ProviderSpec{ID: "x", Type: "..."})
//	        if err != nil { t.Fatal(err) }
//	        return storageapitest.Subject{Provider: p}
//	    })
//	}
//
// RunStreaming is a second entry point, run separately:
//
//	func TestNothingScalesWithFileSize(t *testing.T) {
//	    storageapitest.RunStreaming(t, ...)
//	}
//
// It proves that nothing on the provider's path holds a share of the object.
// It is not folded into RunProviderContract because it moves half
// a gigabyte through the provider twice and forks a process to bound the heap
// while it does, which is not a cost every run of the contract should carry.
//
// # What a subject has to declare
//
// A provider declaring Browse and Write is returned fresh and empty, with the
// default upload allowlist, so the extension check runs.
//
// Such a provider sets either Subject.Root, the directory it keeps its objects
// in, or Subject.NoFilesystemRoot. Never both and never neither. Root is what
// lets the suite plant a link inside the store and prove it opens no way out.
//
// A storageapi.Presigner sets Subject.ResolvablePath, the one path the suite
// requires it to turn into a URL.
//
// A storageapi.LocalFiles also declares Write, because the suite writes a file
// before opening a handle to it, and sets Subject.Root.
package storageapitest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// Subject is a provider instance plus what the suite needs to exercise it.
type Subject struct {
	Provider storageapi.Provider
	// ResolvablePath is a relative path the provider can turn into a download
	// URL. It is required from a storageapi.Presigner and read from no other
	// provider: a store that serves no URLs says so by leaving the interface
	// out, so there is no under-declaring left for the suite to probe for.
	ResolvablePath string
	// Root is the directory a filesystem-backed provider keeps its objects in.
	// The suite plants symlinks and Windows junctions inside it that point out
	// of it, then requires every operation to refuse them. Links arrive in a
	// store from an operator's own "ln -s", a bind mount, an rsync'd tree or a
	// USB stick, and a provider that follows one reads and writes anywhere the
	// server process can.
	//
	// Root is also what tells the escaping-name cases where the store ends, so
	// they can require that a name pointing past it reached nothing.
	Root string
	// NoFilesystemRoot declares that the provider keeps its objects somewhere
	// the suite cannot plant a link, so the escape subtests do not apply and are
	// skipped. A provider declaring Browse and Write sets this or Root, never
	// both and never neither: an omission that silently skipped the security
	// cases would let a provider certify green with no confinement at all.
	NoFilesystemRoot bool
}

// MakeSubject builds a fresh subject for a single subtest.
type MakeSubject func(t *testing.T) Subject

// shapeError reports why the suite cannot exercise a provider with these caps,
// or nil when it can. Browse and Write are asserted together: most browse
// subtests build their fixtures by writing, so a read-only provider would
// require a pre-populated factory the suite does not have.
func shapeError(caps storageapi.Caps) error {
	if caps.Browse != caps.Write {
		return fmt.Errorf("storageapitest covers full (Browse+Write) and URL-only "+
			"(neither) providers; got Browse=%v Write=%v. A read-only provider "+
			"needs the suite extended with a pre-populated factory",
			caps.Browse, caps.Write)
	}
	if caps.LocalFile && !caps.Write {
		return errors.New("storageapitest cannot exercise LocalFiles without Write; " +
			"testLocalFileOpens writes a file before opening a handle to it, so a " +
			"provider implementing LocalFiles must also declare Write")
	}
	return nil
}

// storeError reports why the suite cannot exercise the escape cases against a
// subject, or nil when it can. The declaration is required rather than inferred
// so that a provider which simply never wrote one is told, instead of being
// certified on a suite run that skipped every security case.
func storeError(caps storageapi.Caps, s Subject) error {
	if caps.LocalFile && s.NoFilesystemRoot {
		return errors.New("Subject.NoFilesystemRoot is set while the provider implements " +
			"storageapi.LocalFiles; a provider that hands out open file handles keeps its objects " +
			"on a filesystem, so the suite must be told where, to plant a link there and " +
			"require the handle it gets back is not the object the link points at")
	}
	if !caps.Browse || !caps.Write {
		return nil
	}
	switch {
	case s.Root == "" && !s.NoFilesystemRoot:
		return errors.New("a provider declaring Browse and Write must set Subject.Root, " +
			"the directory it keeps its objects in, so the suite can plant a link inside " +
			"the store and require every operation to refuse it; set " +
			"Subject.NoFilesystemRoot to declare that there is no such directory")
	case s.Root != "" && s.NoFilesystemRoot:
		return errors.New("Subject.Root and Subject.NoFilesystemRoot are both set; " +
			"one says where the store is and the other says there is none")
	}
	return nil
}

// RunProviderContract runs the conformance suite against the provider produced
// by newSubject, asserting the surface it declares and the abilities it
// implements. Each subtest gets its own provider instance.
func RunProviderContract(t *testing.T, newSubject MakeSubject) {
	t.Helper()
	subject := newSubject(t)
	caps := storageapi.Probe(subject.Provider)

	if err := shapeError(caps); err != nil {
		t.Fatal(err)
	}
	if err := storeError(caps, subject); err != nil {
		t.Fatal(err)
	}

	t.Run("Identity", func(t *testing.T) { testIdentity(t, newSubject(t).Provider) })

	if caps.Browse {
		t.Run("EmptyRoot", func(t *testing.T) { testEmptyRoot(t, newSubject(t).Provider) })
		t.Run("SaveStatOpen", func(t *testing.T) { testSaveStatOpen(t, newSubject(t).Provider) })
		t.Run("ListImmediateChildren", func(t *testing.T) { testListImmediate(t, newSubject(t).Provider) })
		t.Run("Mkdir", func(t *testing.T) { testMkdir(t, newSubject(t).Provider) })
		t.Run("DeleteFile", func(t *testing.T) { testDeleteFile(t, newSubject(t).Provider) })
		t.Run("DeleteDirRecursive", func(t *testing.T) { testDeleteDir(t, newSubject(t).Provider) })
		t.Run("DeleteMany", func(t *testing.T) { testDeleteMany(t, newSubject(t).Provider) })
		t.Run("DeleteManyWithMissing", func(t *testing.T) { testDeleteManyMissing(t, newSubject(t).Provider) })
		t.Run("StatMissing", func(t *testing.T) { testStatMissing(t, newSubject(t).Provider) })
		t.Run("Traversal", func(t *testing.T) { testTraversal(t, newSubject(t).Provider) })
		t.Run("LinkEscape", func(t *testing.T) { testLinkEscape(t, newSubject, caps) })
		t.Run("PathEscape", func(t *testing.T) { testPathEscape(t, newSubject, caps) })
		t.Run("ExtensionRejected", func(t *testing.T) { testExtensionRejected(t, newSubject(t).Provider) })
		t.Run("SaveIsAtomic", func(t *testing.T) { testSaveIsAtomic(t, newSubject(t).Provider) })
		t.Run("NothingIsVisibleBeforeCommit", func(t *testing.T) {
			testNothingVisibleBeforeCommit(t, newSubject(t).Provider)
		})
		t.Run("AbortLeavesTheStoreAsItWas", func(t *testing.T) { testAbortLeavesTheStore(t, newSubject(t).Provider) })
		t.Run("CommitTwiceIsRefused", func(t *testing.T) { testCommitTwice(t, newSubject(t).Provider) })
		t.Run("WriteResultCarriesTheDigest", func(t *testing.T) { testWriteDigest(t, newSubject(t).Provider) })
	} else {
		t.Run("BrowseUnsupported", func(t *testing.T) { testBrowseUnsupported(t, newSubject(t).Provider) })
		t.Run("WriteUnsupported", func(t *testing.T) { testWriteUnsupported(t, newSubject(t).Provider) })
	}

	if caps.Browse && caps.Rename {
		t.Run("Rename", func(t *testing.T) { testRename(t, newSubject(t).Provider) })
		t.Run("RenameDir", func(t *testing.T) { testRenameDir(t, newSubject(t).Provider) })
		t.Run("RenameMissing", func(t *testing.T) { testRenameMissing(t, newSubject(t).Provider) })
		t.Run("RenameExtensionRejected", func(t *testing.T) {
			testRenameExtensionRejected(t, newSubject(t).Provider)
		})
	}

	if caps.DownloadURL {
		t.Run("DownloadURLResolves", func(t *testing.T) { testDownloadURLResolves(t, newSubject(t)) })
	}

	if caps.LocalFile {
		t.Run("LocalFileOpens", func(t *testing.T) { testLocalFileOpens(t, newSubject(t).Provider) })
		t.Run("LocalFileCreates", func(t *testing.T) { testLocalFileCreates(t, newSubject(t).Provider) })
		t.Run("LocalFileRejectsRoot", func(t *testing.T) { testLocalFileRejectsRoot(t, newSubject(t).Provider) })
		t.Run("LocalFileRejectsMissing", func(t *testing.T) {
			testLocalFileRejectsMissing(t, newSubject(t).Provider)
		})
	}

	if caps.Range {
		t.Run("RangeInTheMiddle", func(t *testing.T) { testRangeMiddle(t, newSubject(t).Provider) })
		t.Run("RangeToTheEnd", func(t *testing.T) { testRangeToTheEnd(t, newSubject(t).Provider) })
		t.Run("RangeAtTheLength", func(t *testing.T) { testRangeAtTheLength(t, newSubject(t).Provider) })
		t.Run("RangePastTheLength", func(t *testing.T) { testRangePastTheLength(t, newSubject(t).Provider) })
	}
}

// renamerOf, presignerOf, localFilesOf and rangeReaderOf take the interface
// Probe already found. A failure here is the suite calling a subtest it should
// not have, not a provider defect.
func renamerOf(t *testing.T, p storageapi.Provider) storageapi.Renamer {
	t.Helper()
	r, ok := p.(storageapi.Renamer)
	if !ok {
		t.Fatalf("%T is not a storageapi.Renamer", p)
	}
	return r
}

func presignerOf(t *testing.T, p storageapi.Provider) storageapi.Presigner {
	t.Helper()
	s, ok := p.(storageapi.Presigner)
	if !ok {
		t.Fatalf("%T is not a storageapi.Presigner", p)
	}
	return s
}

func localFilesOf(t *testing.T, p storageapi.Provider) storageapi.LocalFiles {
	t.Helper()
	l, ok := p.(storageapi.LocalFiles)
	if !ok {
		t.Fatalf("%T is not a storageapi.LocalFiles", p)
	}
	return l
}

func rangeReaderOf(t *testing.T, p storageapi.Provider) storageapi.RangeReader {
	t.Helper()
	r, ok := p.(storageapi.RangeReader)
	if !ok {
		t.Fatalf("%T is not a storageapi.RangeReader", p)
	}
	return r
}

// save drives the whole of a streaming upload the way a host does, through
// storageapi.Save, and reports what the provider said it wrote.
func save(t *testing.T, p storageapi.Provider, path, content string) storageapi.WriteResult {
	t.Helper()
	result, err := storageapi.Save(t.Context(), p, path, strings.NewReader(content))
	if err != nil {
		t.Fatalf("Save(%q): %v", path, err)
	}
	if result.Size != int64(len(content)) {
		t.Fatalf("Save(%q) wrote %d bytes, want %d", path, result.Size, len(content))
	}
	return result
}

func find(entries []storageapi.FileInfo, name string) (storageapi.FileInfo, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return storageapi.FileInfo{}, false
}

func testEmptyRoot(t *testing.T, p storageapi.Provider) {
	entries, err := p.List(t.Context(), "")
	if err != nil {
		t.Fatalf("List(\"\") on empty: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty provider should list nothing, got %+v", entries)
	}
}

func testSaveStatOpen(t *testing.T, p storageapi.Provider) {
	save(t, p, "a.gcode", "hello")

	info, err := p.Stat(t.Context(), "a.gcode")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.IsDir || info.Size != 5 || info.Name != "a.gcode" {
		t.Fatalf("unexpected Stat: %+v", info)
	}

	rc, err := p.Open(t.Context(), "a.gcode")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "hello" {
		t.Fatalf("Open content = %q, want %q", data, "hello")
	}
}

// A name holds what it held before until Commit returns. A provider that
// publishes as it writes hands a printer a half-written object, which is the
// failure Create and Commit exist to make impossible.
func testNothingVisibleBeforeCommit(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()

	w, err := p.Create(ctx, "pending.gcode")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err = io.WriteString(w, "G28 PARTIAL"); err != nil {
		t.Fatalf("write: %v", err)
	}

	if info, statErr := p.Stat(ctx, "pending.gcode"); statErr == nil {
		t.Errorf("Stat reported %+v before Commit", info)
	}
	entries, err := p.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, found := find(entries, "pending.gcode"); found {
		t.Error("the listing showed the object before Commit")
	}

	result, err := w.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if result.Size != int64(len("G28 PARTIAL")) {
		t.Errorf("Commit reported %d bytes, want %d", result.Size, len("G28 PARTIAL"))
	}
	if _, err = p.Stat(ctx, "pending.gcode"); err != nil {
		t.Errorf("Stat after Commit: %v", err)
	}
}

// Abort is what a failed upload runs, so the store has to look exactly as it
// did: no new name, and no damage to a name that was already there.
func testAbortLeavesTheStore(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	save(t, p, "keeper.gcode", atomicOriginal)

	w, err := p.Create(ctx, "keeper.gcode")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err = io.WriteString(w, "G1 NEVER COMMITTED"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err = w.Abort(ctx); err != nil {
		t.Fatalf("Abort: %v", err)
	}

	rc, err := p.Open(ctx, "keeper.gcode")
	if err != nil {
		t.Fatalf("the existing object is gone after an Abort: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(got) != atomicOriginal {
		t.Errorf("Abort damaged the existing object: got %q, want %q", got, atomicOriginal)
	}

	fresh, err := p.Create(ctx, "aborted.gcode")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err = io.WriteString(fresh, "G1 NEVER COMMITTED"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err = fresh.Abort(ctx); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if info, statErr := p.Stat(ctx, "aborted.gcode"); statErr == nil {
		t.Errorf("an aborted write published %+v", info)
	}
}

// A caller that commits twice has lost track of its own write. Reporting
// ErrWriteClosed separates it from a store that failed, which is what lets a
// caller defer Abort and let the successful path fall through.
func testCommitTwice(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()

	w, err := p.Create(ctx, "once.gcode")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err = io.WriteString(w, "G28\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err = w.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if _, err = w.Commit(ctx); !errors.Is(err, storageapi.ErrWriteClosed) {
		t.Errorf("second Commit = %v, want ErrWriteClosed", err)
	}
	if err = w.Abort(ctx); err != nil {
		t.Errorf("Abort after a successful Commit = %v, want nil", err)
	}
	if _, err = p.Stat(ctx, "once.gcode"); err != nil {
		t.Errorf("Abort after Commit removed the committed object: %v", err)
	}
}

// digestContent is what the digest subtest writes. It is long enough that a
// provider hashing something other than the object's bytes cannot match by
// accident.
const digestContent = "G28\nG1 X10 Y10 Z0.2 F1200\nM104 S215\n"

// The digest is the provider's own reading of the bytes it wrote, so a caller
// comparing it against a digest of its source is comparing two independent
// readings. A later Stat has to repeat it, or the digest is not metadata the
// store kept.
func testWriteDigest(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	result := save(t, p, "digest.gcode", digestContent)

	if result.SHA256 == "" {
		t.Skip("this provider computes no digest, which the contract allows; " +
			"an empty WriteResult.SHA256 is how it says so")
	}

	sum := sha256.Sum256([]byte(digestContent))
	want := hex.EncodeToString(sum[:])
	if result.SHA256 != want {
		t.Fatalf("WriteResult.SHA256 = %q, want %q", result.SHA256, want)
	}

	info, err := p.Stat(ctx, "digest.gcode")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.SHA256 != want {
		t.Errorf("Stat reported SHA256 %q, want the digest the write returned, %q", info.SHA256, want)
	}

	entries, err := p.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	listed, found := find(entries, "digest.gcode")
	if !found {
		t.Fatal("the listing does not hold the object that was just written")
	}
	if listed.SHA256 != want {
		t.Errorf("List reported SHA256 %q, want %q", listed.SHA256, want)
	}
}

func testListImmediate(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	save(t, p, "a.gcode", "x")
	save(t, p, "sub/b.gcode", "y")
	if err := p.Mkdir(ctx, "emptydir"); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	entries, err := p.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if f, ok := find(entries, "a.gcode"); !ok || f.IsDir {
		t.Errorf("root should contain file a.gcode, got %+v", entries)
	}
	if d, ok := find(entries, "sub"); !ok || !d.IsDir {
		t.Errorf("root should contain dir sub, got %+v", entries)
	}
	if d, ok := find(entries, "emptydir"); !ok || !d.IsDir {
		t.Errorf("root should contain dir emptydir, got %+v", entries)
	}
	if _, ok := find(entries, "b.gcode"); ok {
		t.Error("root listing must not include nested b.gcode")
	}

	// Listing the subfolder shows the nested file.
	sub, err := p.List(ctx, "sub")
	if err != nil {
		t.Fatalf("List(sub): %v", err)
	}
	if f, ok := find(sub, "b.gcode"); !ok || f.IsDir {
		t.Errorf("sub should contain b.gcode, got %+v", sub)
	}
}

func testMkdir(t *testing.T, p storageapi.Provider) {
	if err := p.Mkdir(t.Context(), "models"); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	entries, _ := p.List(t.Context(), "")
	if d, ok := find(entries, "models"); !ok || !d.IsDir {
		t.Fatalf("Mkdir did not create a listable dir, got %+v", entries)
	}
}

func testRename(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	save(t, p, "a.gcode", "x")
	if err := renamerOf(t, p).Rename(ctx, "a.gcode", "b.gcode"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := p.Stat(ctx, "a.gcode"); err == nil {
		t.Error("source should be gone after Rename")
	}
	if _, err := p.Stat(ctx, "b.gcode"); err != nil {
		t.Errorf("destination missing after Rename: %v", err)
	}
}

func testRenameDir(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	save(t, p, "a/f.gcode", "x")
	if err := p.Mkdir(ctx, "a/empty"); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := renamerOf(t, p).Rename(ctx, "a", "b"); err != nil {
		t.Fatalf("Rename dir: %v", err)
	}
	if _, err := p.Stat(ctx, "b/f.gcode"); err != nil {
		t.Errorf("file should move with its dir: %v", err)
	}
	if _, err := p.Stat(ctx, "a/f.gcode"); err == nil {
		t.Error("old dir contents should be gone after dir Rename")
	}
	// The nested (empty) sub-dir must move too, not be orphaned.
	entries, _ := p.List(ctx, "b")
	if d, ok := find(entries, "empty"); !ok || !d.IsDir {
		t.Errorf("nested empty dir should move with its parent, got %+v", entries)
	}
}

func testRenameMissing(t *testing.T, p storageapi.Provider) {
	if err := renamerOf(t, p).Rename(t.Context(), "does-not-exist.gcode", "x.gcode"); err == nil {
		t.Error("Rename of a nonexistent source should return an error")
	}
}

func testDeleteFile(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	save(t, p, "x.gcode", "x")
	if err := p.Delete(ctx, "x.gcode"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := p.Stat(ctx, "x.gcode"); err == nil {
		t.Error("file should be gone after Delete")
	}
}

func testDeleteDir(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	save(t, p, "d/1.gcode", "x")
	save(t, p, "d/2.gcode", "y")
	if err := p.Delete(ctx, "d"); err != nil {
		t.Fatalf("Delete dir: %v", err)
	}
	if _, err := p.Stat(ctx, "d/1.gcode"); err == nil {
		t.Error("dir contents should be gone after recursive Delete")
	}
}

func testDeleteMany(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	save(t, p, "a.gcode", "x")
	save(t, p, "b.gcode", "y")
	save(t, p, "c.gcode", "z")
	if err := p.DeleteMany(ctx, []string{"a.gcode", "b.gcode"}); err != nil {
		t.Fatalf("DeleteMany: %v", err)
	}
	if _, err := p.Stat(ctx, "a.gcode"); err == nil {
		t.Error("a.gcode should be deleted")
	}
	if _, err := p.Stat(ctx, "c.gcode"); err != nil {
		t.Error("c.gcode should remain")
	}
}

// Deleting a batch that includes a missing path must not fail the whole call.
// The entries that exist are still removed.
func testDeleteManyMissing(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	save(t, p, "a.gcode", "x")
	if err := p.DeleteMany(ctx, []string{"a.gcode", "missing.gcode"}); err != nil {
		t.Fatalf("DeleteMany with a missing path should not error, got %v", err)
	}
	if _, err := p.Stat(ctx, "a.gcode"); err == nil {
		t.Error("existing entry should still be deleted")
	}
}

// A missing path reports ErrInvalidPath, the same error a path that only
// resolves by leaving the root reports. A provider that distinguishes them
// hands a caller an oracle for which names in its store point out of it.
func testStatMissing(t *testing.T, p storageapi.Provider) {
	_, err := p.Stat(t.Context(), "nope.gcode")
	if err == nil {
		t.Fatal("Stat of a missing path should return an error")
	}
	if !errors.Is(err, storageapi.ErrInvalidPath) {
		t.Errorf("Stat of a missing path = %v, want ErrInvalidPath", err)
	}
}

// Path traversal must be neutralised: escape attempts resolve inside the
// provider (never read host files), so "../x" addresses the in-root "x".
func testTraversal(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	if rc, err := p.Open(ctx, "../../../../etc/hosts"); err == nil {
		_ = rc.Close()
		t.Error("traversal Open must not read files outside the provider root")
	}
	save(t, p, "x.gcode", "data")
	rc, err := p.Open(ctx, "../x.gcode") // resolves to in-root x.gcode, not an escape
	if err != nil {
		t.Fatalf("traversal path should resolve inside root: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "data" {
		t.Errorf("../x.gcode should resolve to in-root x.gcode, got %q", data)
	}
}

func testExtensionRejected(t *testing.T, p storageapi.Provider) {
	if _, err := storageapi.Save(t.Context(), p, "evil.sh", strings.NewReader("rm -rf /")); err == nil {
		t.Error("provider with the default allowlist must reject a .sh upload")
	}
}

// The allowlist decides what names the store ends up holding, and a rename puts
// a name there as surely as an upload does. A provider that checks only the
// write path lets a caller upload payload.gcode and rename it to payload.sh.
func testRenameExtensionRejected(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	save(t, p, "a.gcode", "x")
	err := renamerOf(t, p).Rename(ctx, "a.gcode", "payload.sh")
	if !errors.Is(err, storageapi.ErrExtensionNotAllowed) {
		t.Errorf(`Rename("a.gcode", "payload.sh") = %v, want ErrExtensionNotAllowed`, err)
	}
	if _, err = p.Stat(ctx, "payload.sh"); err == nil {
		t.Error("Rename put payload.sh in the store, past the allowlist")
	}
}

func testIdentity(t *testing.T, p storageapi.Provider) {
	if p.Name() == "" {
		t.Error("Name() must not be empty: it is the storage id callers address")
	}
	if p.DisplayName() == "" {
		t.Error("DisplayName() must not be empty: the UI renders it as a label")
	}
}

// errAfterN yields n bytes then fails, standing in for an upload whose
// connection drops partway.
type errAfterN struct {
	data []byte
	n    int
	off  int
}

var errShortRead = errors.New("storageapitest: reader failed")

// bytesBeforeFailure is how much the reader yields before failing: enough
// that a provider streaming straight to the destination has already written.
const bytesBeforeFailure = 8

func (r *errAfterN) Read(p []byte) (int, error) {
	if r.off >= r.n {
		return 0, errShortRead
	}
	c := copy(p, r.data[r.off:r.n])
	r.off += c
	return c, nil
}

// atomicOriginal is written before the failing overwrite and must survive it.
const atomicOriginal = `G28
ORIGINAL CONTENT
`

// testSaveIsAtomic asserts that a write which fails partway neither destroys an
// existing object nor publishes a partial one under the destination name.
//
// A provider that opens the destination directly and streams into it fails
// here, which is the point: that shape loses a customer's file every time an
// upload is interrupted.
func testSaveIsAtomic(t *testing.T, p storageapi.Provider) {
	t.Helper()
	ctx := t.Context()

	if _, err := storageapi.Save(ctx, p, "atomic.gcode", strings.NewReader(atomicOriginal)); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	src := &errAfterN{data: []byte("G1 REPLACEMENT NEVER COMPLETES"), n: bytesBeforeFailure}
	if _, err := storageapi.Save(ctx, p, "atomic.gcode", src); err == nil {
		t.Fatal("Save reported success for a reader that failed")
	}

	f, err := p.Open(ctx, "atomic.gcode")
	if err != nil {
		t.Fatalf("existing object gone after a failed overwrite: %v", err)
	}
	defer func() { _ = f.Close() }()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != atomicOriginal {
		t.Fatalf("failed overwrite damaged the existing object: got %q, want %q", got, atomicOriginal)
	}

	src2 := &errAfterN{data: []byte("G1 PARTIAL"), n: bytesBeforeFailure}
	if _, saveErr := storageapi.Save(ctx, p, "atomic-new.gcode", src2); saveErr == nil {
		t.Fatal("Save reported success for a reader that failed")
	}
	if _, statErr := p.Stat(ctx, "atomic-new.gcode"); statErr == nil {
		t.Fatal("a failed Save published a partial object at the destination")
	}
}
