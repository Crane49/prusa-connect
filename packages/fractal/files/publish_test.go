package files_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/files"

	fractalfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// waitFor is the longest a test waits for the publisher to announce a change.
const waitFor = 5 * time.Second

// The tree of three files of the extension's examples, and its listings.
const (
	specSub  = "8201818465622e74787401055821015e846c64f2db12266e6b658a8e5b5b42cc225419b3ee1fca88acbb181ddfdb52"
	specRoot = "8201838465612e74787401035821019834876dcfb05cb167a5c24953eba5" +
		"8c4ac89b1adf57f28f2f9d09af107ee8f0846373756202182f58210157d4" +
		"5eeb1d5c893bef51ebf3b9a30c32d41872287096ba699bf356f668a04b8a" +
		"8466c3842e7478740107582101cd4f3afa6982937e1fcc283c451f1cd9d3" +
		"68e998b554ff7b06eabe6e723a39c2"
)

// publishing is a publisher over a fake storage, with the shared folders in
// a directory of the test.
type publishing struct {
	st        *fakeStorage
	shares    *files.Shares
	dir       string
	publisher *files.Publisher
	storages  []string
}

func newPublishing(t *testing.T) *publishing {
	t.Helper()

	p := &publishing{st: newStorage(), dir: t.TempDir()}
	p.shares = openShares(t, p.dir)
	p.publisher = p.restart()

	return p
}

// restart builds a new publisher on the same storage and directory, as a
// restart of the module does.
func (p *publishing) restart() *files.Publisher {
	return files.NewPublisher(p.st, p.shares, p.dir, func(context.Context) ([]string, error) {
		return append(p.shares.Storages(), p.storages...), nil
	}, slog.New(slog.DiscardHandler))
}

func (p *publishing) share(t *testing.T, storage, folder string) {
	t.Helper()

	if err := p.shares.Share(storage, folder); err != nil {
		t.Fatalf("share %s: %v", folder, err)
	}
}

func (p *publishing) scan(t *testing.T) {
	t.Helper()

	if err := p.publisher.Scan(t.Context()); err != nil {
		t.Fatalf("scan: %v", err)
	}
}

// announced drains Updated and returns what it held.
func (p *publishing) announced() []string {
	var out []string

	for {
		select {
		case name := <-p.publisher.Updated():
			out = append(out, name)
		default:
			return out
		}
	}
}

func (p *publishing) root(t *testing.T, storage string) fractalfiles.Identity {
	t.Helper()

	id, ok := p.publisher.RootHash(t.Context(), storage)
	if !ok {
		t.Fatalf("the storage %s has no root hash", storage)
	}

	return id
}

func unhex(t *testing.T, text string) []byte {
	t.Helper()

	data, err := hex.DecodeString(text)
	if err != nil {
		t.Fatalf("decode %s: %v", text, err)
	}

	return data
}

// listing encodes entries and returns the bytes and the identity.
func listing(t *testing.T, entries ...fractalfiles.Entry) ([]byte, fractalfiles.Identity) {
	t.Helper()

	data, err := fractalfiles.Listing{Entries: entries}.Encode()
	if err != nil {
		t.Fatalf("encode a listing: %v", err)
	}

	return data, fractalfiles.Sum(data)
}

func fileEntry(name, data string) fractalfiles.Entry {
	return fractalfiles.Entry{Name: name, Kind: fractalfiles.KindFile, Size: uint64(len(data)),
		Hash: fractalfiles.Sum([]byte(data))}
}

func folderEntry(name string, data []byte) fractalfiles.Entry {
	return fractalfiles.Entry{Name: name, Kind: fractalfiles.KindDirectory, Size: uint64(len(data)),
		Hash: fractalfiles.Sum(data)}
}

// TestPublishesSpecTree: a shared folder holding the tree of the extension's
// examples publishes the listings of the examples, byte for byte, and the
// root holds only the path to it.
func TestPublishesSpecTree(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/a.txt", "aaa", true)
	p.st.put("local", "lib/sub/b.txt", "bbbbb", false)
	p.st.put("local", "lib/Ä.txt", "ccccccc", false)
	p.st.put("local", "lib/.pcl-meta/a.txt.sha256", "x", false)
	p.st.put("local", "private/secret.gcode", "secret", true)
	p.share(t, "local", "lib")
	p.scan(t)

	lib := unhex(t, specRoot)
	got, ok := p.publisher.Lookup(t.Context(), fractalfiles.Sum(lib))
	want := files.Published{Listing: lib, Storage: "local", Path: "lib", Size: int64(len(lib))}

	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("Lookup of the shared folder is %+v %t, want %+v", got, ok, want)
	}

	sub := unhex(t, specSub)
	if got, ok = p.publisher.Lookup(t.Context(), fractalfiles.Sum(sub)); !ok || got.Path != "lib/sub" {
		t.Errorf("Lookup of lib/sub is %+v %t", got, ok)
	}

	_, rootID := listing(t, folderEntry("lib", lib))
	if rootHash := p.root(t, "local"); rootHash != rootID {
		t.Errorf("the root hash is %s, want %s", rootHash.Text(), rootID.Text())
	}

	if _, ok = p.publisher.Lookup(t.Context(), fractalfiles.Sum([]byte("secret"))); ok {
		t.Error("Lookup answered a file that is not shared")
	}
}

// TestRootHoldsPathOnly: the folders above a shared one list only the way
// to it, and a folder shared inside another adds nothing.
func TestRootHoldsPathOnly(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "jobs/benchy/benchy.gcode", "G1", true)
	p.st.put("local", "jobs/benchy/plate/plate.gcode", "G2", true)
	p.st.put("local", "jobs/other/other.gcode", "G3", true)
	p.share(t, "local", "jobs/benchy")
	p.share(t, "local", "jobs/benchy/plate")
	p.scan(t)

	plate, _ := listing(t, fileEntry("plate.gcode", "G2"))
	benchy, _ := listing(t, fileEntry("benchy.gcode", "G1"), folderEntry("plate", plate))
	jobs, _ := listing(t, folderEntry("benchy", benchy))
	_, root := listing(t, folderEntry("jobs", jobs))

	if got := p.root(t, "local"); got != root {
		t.Errorf("the root hash is %s, want %s", got.Text(), root.Text())
	}
}

// TestNamesInNFC: a name not in Normalization Form C is converted, and one
// that converts to a name the folder has gets a suffix before its extension.
func TestNamesInNFC(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/Ä.txt", "nfc", true)
	p.st.put("local", "lib/Ä.txt", "nfd", true)
	p.st.put("local", "lib/Ö", "nfd folder", true)
	p.share(t, "local", "lib")
	p.scan(t)

	lib, _ := listing(t,
		fileEntry("Ä.txt", "nfc"),
		fileEntry("Ä (2).txt", "nfd"),
		fileEntry("Ö", "nfd folder"))
	_, root := listing(t, folderEntry("lib", lib))

	if got := p.root(t, "local"); got != root {
		t.Errorf("the root hash is %s, want %s", got.Text(), root.Text())
	}
}

// TestHashesOnlyWithoutDigest: a file the storage keeps a digest for is never
// read, one without is read once, and a restart reads it no more.
func TestHashesOnlyWithoutDigest(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/kept.gcode", "kept", true)
	p.st.put("local", "lib/plain.gcode", "plain", false)
	p.share(t, "local", "lib")
	p.scan(t)
	p.scan(t)
	p.publisher = p.restart()
	p.scan(t)

	got := [2]int{p.st.readsOf("local", "lib/kept.gcode"), p.st.readsOf("local", "lib/plain.gcode")}
	if want := [2]int{0, 1}; got != want {
		t.Errorf("the reads of kept and plain are %v, want %v", got, want)
	}

	if _, ok := p.publisher.Lookup(t.Context(), fractalfiles.Sum([]byte("plain"))); !ok {
		t.Error("a file the module hashed is not published")
	}
}

// TestUnshareAtOnce: after unsharing, the folder's files are not answered,
// before the next scan, and the scan publishes a new root hash.
func TestUnshareAtOnce(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "a", true)
	p.st.put("local", "jobs/b.gcode", "b", true)
	p.share(t, "local", "lib")
	p.share(t, "local", "jobs")
	p.scan(t)
	before := p.root(t, "local")
	p.announced()

	if err := p.shares.Unshare("local", "lib"); err != nil {
		t.Fatalf("unshare: %v", err)
	}

	type reached struct{ a, b bool }

	lookup := func() reached {
		_, a := p.publisher.Lookup(t.Context(), fractalfiles.Sum([]byte("a")))
		_, b := p.publisher.Lookup(t.Context(), fractalfiles.Sum([]byte("b")))

		return reached{a, b}
	}

	if got := lookup(); got != (reached{a: false, b: true}) {
		t.Errorf("before the scan the files reached are %+v, want only b", got)
	}

	p.scan(t)

	type outcome struct {
		changed   bool
		announced []string
	}

	got := outcome{changed: p.root(t, "local") != before, announced: p.announced()}
	if want := (outcome{changed: true, announced: []string{"local"}}); !reflect.DeepEqual(got, want) {
		t.Errorf("after the scan the publisher holds %+v, want %+v", got, want)
	}
}

// TestFailedScanKeepsRoot: a scan that cannot read a shared folder keeps the
// last root hash and what it reaches.
func TestFailedScanKeepsRoot(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "a", true)
	p.share(t, "local", "lib")
	p.scan(t)
	before := p.root(t, "local")

	p.st.put("local", "lib/new.gcode", "new", true)
	p.st.breakFolder("local", "lib", true)

	if err := p.publisher.Scan(t.Context()); err == nil {
		t.Fatal("a scan of a folder the storage cannot list returned no error")
	}

	type outcome struct {
		root    fractalfiles.Identity
		reached bool
	}

	_, reached := p.publisher.Lookup(t.Context(), fractalfiles.Sum([]byte("a")))
	if got, want := (outcome{p.root(t, "local"), reached}), (outcome{before, true}); got != want {
		t.Errorf("after a failed scan the publisher holds %+v, want %+v", got, want)
	}
}

// TestLeavesOutAnUnreadableFile: a file the storage cannot read is left out
// of its listing with a warning, and the rest of the storage is published.
func TestLeavesOutAnUnreadableFile(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	p := newPublishing(t)
	p.publisher = files.NewPublisher(p.st, p.shares, p.dir, func(context.Context) ([]string, error) {
		return p.shares.Storages(), nil
	}, slog.New(slog.NewTextHandler(&out, nil)))
	p.st.put("local", "lib/a.gcode", "a", true)
	p.st.put("local", "lib/locked.gcode", "locked", false)
	p.st.breakFile("local", "lib/locked.gcode")
	p.share(t, "local", "lib")
	p.scan(t)

	lib, _ := listing(t, fileEntry("a.gcode", "a"))
	_, root := listing(t, folderEntry("lib", lib))

	type outcome struct {
		root   fractalfiles.Identity
		warned bool
	}

	got := outcome{p.root(t, "local"), strings.Contains(out.String(), "level=WARN") &&
		strings.Contains(out.String(), "lib/locked.gcode")}
	if want := (outcome{root, true}); got != want {
		t.Errorf("a scan with an unreadable file gave %+v, want %+v; the log is %q", got, want, out.String())
	}
}

// TestStorageStates: a known storage with no shared folder, as after its last
// folder was unshared, publishes the empty listing, and a storage the operator
// removed has no root hash.
func TestStorageStates(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.storages = []string{"usb"}
	p.st.put("usb", "x.gcode", "x", true)
	p.scan(t)

	empty, ok := p.publisher.Lookup(t.Context(), fractalfiles.EmptyListing)
	want := files.Published{Listing: []byte{0x82, 0x01, 0x80}, Storage: "usb", Size: 3}

	if !ok || !reflect.DeepEqual(empty, want) || p.root(t, "usb") != fractalfiles.EmptyListing {
		t.Fatalf("a storage with no share holds %+v %t, want the empty listing", empty, ok)
	}

	p.announced()
	p.st.dropStorage("usb")
	p.scan(t)

	if _, held := p.publisher.RootHash(t.Context(), "usb"); held {
		t.Error("a removed storage still has a root hash")
	}

	if got := p.announced(); !reflect.DeepEqual(got, []string{"usb"}) {
		t.Errorf("the removal announced %v, want [usb]", got)
	}
}

// TestAnnouncesNewStorage: a storage is announced before its first scan ends,
// and again when its root hash appears. A scan that changes nothing announces
// nothing.
func TestAnnouncesNewStorage(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "a", true)
	p.share(t, "local", "lib")
	p.scan(t)

	if got := p.announced(); !reflect.DeepEqual(got, []string{"local", "local"}) {
		t.Errorf("the first scan announced %v, want local twice", got)
	}

	p.scan(t)

	if got := p.announced(); got != nil {
		t.Errorf("a scan that changed nothing announced %v", got)
	}
}

// TestRunRescansOnShare: Run scans again when a folder is shared, without
// waiting for the interval.
func TestRunRescansOnShare(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "a", true)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- p.publisher.Run(ctx, time.Hour) }()

	p.share(t, "local", "lib")

	deadline := time.After(waitFor)

	for {
		if _, ok := p.publisher.Lookup(t.Context(), fractalfiles.Sum([]byte("a"))); ok {
			break
		}

		select {
		case <-p.publisher.Updated():
		case <-deadline:
			t.Fatal("Run did not publish the shared folder")
		}
	}

	cancel()

	if err := <-done; err != nil {
		t.Errorf("Run returned %v after its context ended", err)
	}
}

// TestUniqueSuffixWithoutExtension: a converted name with no extension, or
// one that starts with a dot, gets the suffix at its end.
func TestUniqueSuffixWithoutExtension(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/Ä", "nfc", true)
	p.st.put("local", "lib/Ä", "nfd", true)
	p.st.put("local", "lib/.Ä", "dot nfc", true)
	p.st.put("local", "lib/.Ä", "dot nfd", true)
	p.share(t, "local", "lib")
	p.scan(t)

	lib, _ := listing(t,
		fileEntry("Ä", "nfc"),
		fileEntry("Ä (2)", "nfd"),
		fileEntry(".Ä", "dot nfc"),
		fileEntry(".Ä (2)", "dot nfd"))

	if got, ok := p.publisher.Lookup(t.Context(), fractalfiles.Sum(lib)); !ok || got.Path != "lib" {
		t.Errorf("Lookup of the expected listing of lib is %+v %t", got, ok)
	}
}

// TestUnshareRebuildsRootAtOnce: after unsharing one of two folders, with no
// scan, the root hash and the answers are those of the folder that stays.
func TestUnshareRebuildsRootAtOnce(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "a", true)
	p.st.put("local", "jobs/b.gcode", "b", true)
	p.share(t, "local", "lib")
	p.share(t, "local", "jobs")
	p.scan(t)

	libList, _ := listing(t, fileEntry("a.gcode", "a"))
	jobsList, _ := listing(t, fileEntry("b.gcode", "b"))
	_, oldRoot := listing(t, folderEntry("jobs", jobsList), folderEntry("lib", libList))
	_, newRoot := listing(t, folderEntry("jobs", jobsList))

	if got := p.root(t, "local"); got != oldRoot {
		t.Fatalf("the root hash is %s, want %s", got.Text(), oldRoot.Text())
	}

	p.announced()

	if err := p.shares.Unshare("local", "lib"); err != nil {
		t.Fatalf("unshare: %v", err)
	}

	type outcome struct {
		root                                 fractalfiles.Identity
		oldRoot, lib, fileA, fileB, jobsList bool
		announced                            []string
	}

	reaches := func(id fractalfiles.Identity) bool { _, ok := p.publisher.Lookup(t.Context(), id); return ok }
	got := outcome{
		root:      p.root(t, "local"),
		oldRoot:   reaches(oldRoot),
		lib:       reaches(fractalfiles.Sum(libList)),
		fileA:     reaches(fractalfiles.Sum([]byte("a"))),
		fileB:     reaches(fractalfiles.Sum([]byte("b"))),
		jobsList:  reaches(fractalfiles.Sum(jobsList)),
		announced: p.announced(),
	}
	want := outcome{root: newRoot, fileB: true, jobsList: true, announced: []string{"local"}}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the unshare the publisher holds %+v, want %+v", got, want)
	}
}

// TestUnshareDuringScan: a scan that began before an unshare never publishes
// the folder again.
func TestUnshareDuringScan(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "a", true)
	p.st.put("local", "jobs/b.gcode", "b", true)
	p.share(t, "local", "lib")
	p.share(t, "local", "jobs")
	p.scan(t)

	entered, release := p.st.holdLists()
	defer release()

	done := make(chan error, 1)

	go func() { done <- p.publisher.Scan(t.Context()) }()

	select {
	case <-entered:
	case <-time.After(waitFor):
		t.Fatal("the scan did not reach the storage")
	}

	if err := p.shares.Unshare("local", "lib"); err != nil {
		t.Fatalf("unshare: %v", err)
	}

	release()

	if err := <-done; err != nil {
		t.Fatalf("scan: %v", err)
	}

	jobsList, _ := listing(t, fileEntry("b.gcode", "b"))
	_, want := listing(t, folderEntry("jobs", jobsList))

	if got := p.root(t, "local"); got != want {
		t.Errorf("the root hash is %s, want %s", got.Text(), want.Text())
	}

	if _, ok := p.publisher.Lookup(t.Context(), fractalfiles.Sum([]byte("a"))); ok {
		t.Error("a file of the unshared folder is answered")
	}
}

// TestSameFileInTwoFolders: a file with the same bytes in two shared folders
// is still answered when one of them is unshared.
func TestSameFileInTwoFolders(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "aaa/a.gcode", "same", true)
	p.st.put("local", "zzz/b.gcode", "same", true)
	p.share(t, "local", "zzz")
	p.share(t, "local", "aaa")
	p.scan(t)

	if err := p.shares.Unshare("local", "aaa"); err != nil {
		t.Fatalf("unshare: %v", err)
	}

	got, ok := p.publisher.Lookup(t.Context(), fractalfiles.Sum([]byte("same")))
	want := files.Published{Storage: "local", Path: "zzz/b.gcode", Size: 4, MTime: 1700000000}

	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("Lookup of the shared file is %+v %t, want %+v", got, ok, want)
	}
}

// TestScanRetriesDigestFile: a digest file that cannot be opened fails one
// scan and not the next.
func TestScanRetriesDigestFile(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "a", false)
	p.share(t, "local", "lib")

	blocker := filepath.Join(p.dir, "files-digests.json")
	if err := os.Mkdir(blocker, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := p.publisher.Scan(t.Context()); err == nil {
		t.Fatal("a scan with an unreadable digest file returned no error")
	}

	if err := os.Remove(blocker); err != nil {
		t.Fatalf("remove: %v", err)
	}

	p.scan(t)
	p.root(t, "local")
}

// TestWarnsOfUnreadableDigests: a digest file the module cannot decode is
// hashed again, and the module says so.
func TestWarnsOfUnreadableDigests(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "files-digests.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var out bytes.Buffer

	publisher := files.NewPublisher(newStorage(), openShares(t, dir), dir,
		func(context.Context) ([]string, error) { return nil, nil }, slog.New(slog.NewTextHandler(&out, nil)))

	if err := publisher.Scan(t.Context()); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !strings.Contains(out.String(), "level=WARN") {
		t.Errorf("no warning in the log: %q", out.String())
	}
}

// TestUnreadUpdatesDoNotBlockScan: a scan finishes when nobody reads Updated,
// and the reader still learns of the storage.
func TestUnreadUpdatesDoNotBlockScan(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.share(t, "local", "lib")

	done := make(chan struct{})

	go func() {
		defer close(done)

		ctx, cancel := context.WithTimeout(t.Context(), 2*waitFor)
		defer cancel()

		for i := range 200 {
			p.st.put("local", "lib/f", strconv.Itoa(i), true)
			_ = p.publisher.Scan(ctx)
		}
	}()

	select {
	case <-done:
	case <-time.After(waitFor):
		t.Fatal("a scan waits for a reader of Updated")
	}

	if got := p.announced(); !slices.Contains(got, "local") {
		t.Errorf("Updated holds %v, want local", got)
	}
}

// TestRunAnnouncesUnshareDuringScan: while a scan is reading, an unshare is
// announced at once, with no one asking for the root hash.
func TestRunAnnouncesUnshareDuringScan(t *testing.T) {
	t.Parallel()

	p := newPublishing(t)
	p.st.put("local", "lib/a.gcode", "a", true)
	p.st.put("local", "jobs/b.gcode", "b", true)
	p.st.put("local", "other/c.gcode", "c", true)
	p.share(t, "local", "lib")
	p.share(t, "local", "jobs")

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- p.publisher.Run(ctx, time.Hour) }()

	for {
		if _, ok := p.publisher.RootHash(t.Context(), "local"); ok {
			break
		}

		select {
		case <-p.publisher.Updated():
		case <-time.After(waitFor):
			t.Fatal("Run did not publish the shared folders")
		}
	}

	p.announced()
	entered, release := p.st.holdLists()

	defer release()

	p.share(t, "local", "other")

	select {
	case <-entered:
	case <-time.After(waitFor):
		t.Fatal("the rescan did not reach the storage")
	}

	if err := p.shares.Unshare("local", "lib"); err != nil {
		t.Fatalf("unshare: %v", err)
	}

	select {
	case name := <-p.publisher.Updated():
		if name != "local" {
			t.Errorf("Updated announced %q, want local", name)
		}
	case <-time.After(waitFor):
		t.Fatal("the unshare was not announced while the scan was reading")
	}

	release()
	cancel()

	if err := <-done; err != nil {
		t.Errorf("Run returned %v after its context ended", err)
	}
}
