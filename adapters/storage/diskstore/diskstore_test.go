package diskstore_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/storage/diskstore"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi/storageapitest"
)

// The implementation carries no registered name, so a suite over it names its
// own: what is under test is the store and not what an adapter calls it.
const (
	testType  = "diskstore"
	testLabel = "Disk store"
)

func open(t *testing.T, root string) *diskstore.Provider {
	t.Helper()

	p, err := diskstore.Open(storageapi.ProviderSpec{
		ID:                "local",
		Type:              testType,
		Path:              root,
		AllowedExtensions: storageapi.DefaultAllowedExtensions,
	}, testLabel)
	require.NoError(t, err)

	return p
}

// The published suite every storage provider is held to, ours included.
func TestProviderContract(t *testing.T) {
	storageapitest.RunProviderContract(t, func(t *testing.T) storageapitest.Subject {
		root := t.TempDir()

		return storageapitest.Subject{Provider: open(t, root), Root: root}
	})
}

// An earlier design decision: nothing on the path holds a share of the object. The suite
// streams half a gigabyte through Save and back out through Open in a second
// process whose live heap is bounded well under it, so a provider that buffers
// is killed rather than merely slow.
func TestNothingScalesWithFileSize(t *testing.T) {
	storageapitest.RunStreaming(t, func(t *testing.T) storageapitest.Subject {
		root := t.TempDir()

		return storageapitest.Subject{Provider: open(t, root), Root: root}
	})
}

// A spec with no path names no directory to keep objects in, and a provider
// that defaulted it would put an operator's uploads in the working directory.
func TestASpecWithNoPathIsRejected(t *testing.T) {
	t.Parallel()

	_, err := diskstore.Open(storageapi.ProviderSpec{ID: "local", Type: testType}, testLabel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec.Path")
}

// The store is created when it is not there, parents included, because an
// operator names a directory under a volume rather than one that exists.
func TestTheStoreIsCreatedWithItsParents(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "a", "b", "c")
	p := open(t, root)

	assert.Equal(t, root, p.Root())

	info, err := os.Stat(root)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

// The spec's own label wins over the adapter's default, so two disk-backed
// stores are told apart in the UI.
func TestTheSpecNamesTheLabel(t *testing.T) {
	t.Parallel()

	p, err := diskstore.Open(storageapi.ProviderSpec{
		ID: "usb", Type: testType, Name: "USB stick", Path: t.TempDir(),
	}, testLabel)
	require.NoError(t, err)

	assert.Equal(t, "usb", p.Name())
	assert.Equal(t, "USB stick", p.DisplayName())
}

// A store whose directory is replaced under the running server leads somewhere
// the provider was not built on, and every operation refuses rather than
// serving the new one. An operator remounting a volume is the case.
//
// The original is moved aside rather than deleted, so the replacement cannot be
// handed the inode the original had. Deleting it first is not a test of this:
// ADR-20260825-smx's escape table records identity as device and inode, and on the
// container filesystem `make test-race` runs in, a directory removed and
// remade at the same path came back with the inode it had before (2552450 both
// times, measured), so os.SameFile reported the replacement as the original and
// List returned the new directory's empty contents. Renaming leaves the
// original inode in use and the replacement gets the next one.
func TestAReplacedStoreIsRefused(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	root := filepath.Join(parent, "store")
	p := open(t, root)

	_, err := storageapi.Save(t.Context(), p, "a.gcode", strings.NewReader("x"))
	require.NoError(t, err)

	require.NoError(t, os.Rename(root, filepath.Join(parent, "unmounted")))
	require.NoError(t, os.Mkdir(root, 0o750))

	_, err = p.Stat(t.Context(), "a.gcode")
	require.Error(t, err)

	_, err = p.List(t.Context(), "")
	require.Error(t, err)
}

// An empty allowlist permits anything, which is what an operator gets by
// leaving the key out. The default list is applied by the host, not invented
// here.
func TestAnEmptyAllowlistPermitsAnything(t *testing.T) {
	t.Parallel()

	p, err := diskstore.Open(storageapi.ProviderSpec{ID: "local", Type: testType, Path: t.TempDir()},
		testLabel)
	require.NoError(t, err)

	_, err = storageapi.Save(t.Context(), p, "script.sh", strings.NewReader("echo hi"))
	require.NoError(t, err)
}

// A file already sitting where a component of the store path goes is an
// operator's mistake and has to be reported at construction, not on the first
// upload.
func TestAFileInThePathIsReportedAtConstruction(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	blocked := filepath.Join(parent, "occupied")
	require.NoError(t, os.WriteFile(blocked, []byte("not a directory"), 0o600))

	_, err := diskstore.Open(storageapi.ProviderSpec{
		ID: "local", Type: testType, Path: filepath.Join(blocked, "store"),
	}, testLabel)
	require.Error(t, err)
}

// A partial upload leaves no temporary file behind, so a store does not fill
// with the debris of interrupted transfers.
func TestAFailedSaveLeavesNothingBehind(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	p := open(t, root)

	_, err := storageapi.Save(t.Context(), p, "a.gcode", failingReader{})
	require.Error(t, err)

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, entries, "a failed upload left debris in the store")
}

type failingReader struct{}

var errReaderFailed = errors.New("reader failed")

func (failingReader) Read([]byte) (int, error) { return 0, errReaderFailed }

// The digest a write reports is kept beside the object, in one reserved
// directory at the store root, so a later Stat answers with the same number
// after a restart. An operator browsing the store must never be shown it.
func TestTheDigestIsKeptBesideTheObject(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	p := open(t, root)

	written, err := storageapi.Save(t.Context(), p, "sub/a.gcode", strings.NewReader("G28\n"))
	require.NoError(t, err)

	sum := sha256.Sum256([]byte("G28\n"))
	assert.Equal(t, hex.EncodeToString(sum[:]), written.SHA256)

	sidecar := filepath.Join(root, ".pcl-meta", "sub", "a.gcode.sha256")
	recorded, err := os.ReadFile(sidecar)
	require.NoError(t, err)
	assert.Equal(t, written.SHA256, string(recorded))

	// A fresh provider on the same directory reads the digest back, which is
	// what makes it metadata rather than a value held in this process.
	described, err := open(t, root).Stat(t.Context(), "sub/a.gcode")
	require.NoError(t, err)
	assert.Equal(t, written.SHA256, described.SHA256)

	listed, err := p.List(t.Context(), "")
	require.NoError(t, err)
	for _, entry := range listed {
		assert.NotEqual(t, ".pcl-meta", entry.Name, "the store's own bookkeeping was listed to an operator")
	}
}

// The reserved directory is not an operator's to address. Every entry point
// that takes a name refuses it, so a caller cannot delete the store's digests
// or write a name into the place they are kept.
func TestTheReservedDirectoryIsRefusedByName(t *testing.T) {
	t.Parallel()

	p := open(t, t.TempDir())
	ctx := t.Context()

	_, err := p.Create(ctx, ".pcl-meta/planted.gcode")
	require.ErrorIs(t, err, storageapi.ErrInvalidPath)

	require.ErrorIs(t, p.Delete(ctx, ".pcl-meta"), storageapi.ErrInvalidPath)
	require.ErrorIs(t, p.Mkdir(ctx, ".pcl-meta/sub"), storageapi.ErrInvalidPath)
	require.ErrorIs(t, p.Rename(ctx, ".pcl-meta", "loot"), storageapi.ErrInvalidPath)

	_, err = p.Stat(ctx, ".pcl-meta")
	require.ErrorIs(t, err, storageapi.ErrInvalidPath)

	_, err = p.OpenLocal(ctx, ".pcl-meta/x")
	require.ErrorIs(t, err, storageapi.ErrInvalidPath)

	_, err = p.CreateLocal(ctx, ".pcl-meta/x")
	require.ErrorIs(t, err, storageapi.ErrInvalidPath)
}

// A digest follows its object. A delete takes it and a rename moves it, so a
// name never reports the digest of bytes that are no longer under it.
func TestTheDigestFollowsTheObject(t *testing.T) {
	t.Parallel()

	p := open(t, t.TempDir())
	ctx := t.Context()

	written, err := storageapi.Save(ctx, p, "a.gcode", strings.NewReader("G28\n"))
	require.NoError(t, err)

	require.NoError(t, p.Rename(ctx, "a.gcode", "b.gcode"))

	moved, err := p.Stat(ctx, "b.gcode")
	require.NoError(t, err)
	assert.Equal(t, written.SHA256, moved.SHA256)

	require.NoError(t, p.Delete(ctx, "b.gcode"))

	// The same name written again by a caller that keeps its own bytes reports
	// no digest, rather than the one the deleted object had.
	handle, err := p.CreateLocal(ctx, "b.gcode")
	require.NoError(t, err)
	_, err = handle.Write([]byte("G1 X0\n"))
	require.NoError(t, err)
	require.NoError(t, handle.Close())

	staged, err := p.Stat(ctx, "b.gcode")
	require.NoError(t, err)
	assert.Empty(t, staged.SHA256)
}

// A name the store has nothing under carries both errors a caller branches on.
// ErrInvalidPath is what the provider contract publishes for a name that does
// not resolve, and every way a name fails to resolve stays that one answer.
// fs.ErrNotExist is what lets a host say "there is no such file" and answer
// 404, rather than telling a person their path is wrong.
func TestAMissingNameCarriesBothTheContractsErrorAndNotExist(t *testing.T) {
	t.Parallel()

	p := open(t, t.TempDir())

	for _, call := range []struct {
		name string
		run  func() error
	}{
		{"stat", func() error { _, err := p.Stat(t.Context(), "nope.gcode"); return err }},
		{"open", func() error { _, err := p.Open(t.Context(), "nope.gcode"); return err }},
	} {
		t.Run(call.name, func(t *testing.T) {
			t.Parallel()

			err := call.run()
			require.Error(t, err)
			require.ErrorIs(t, err, storageapi.ErrInvalidPath,
				"the error the provider contract publishes is gone")
			require.ErrorIs(t, err, fs.ErrNotExist,
				"a host cannot tell a missing name from a name it will not take")
		})
	}
}

// A listing flags a link, whether it points at a file or a folder, and keeps
// it with the link's own size. Only the module wire leaves it out (ADR-20261023-sns
// rule 1), so the HTTP listing people see does not change.
func TestAListingFlagsLinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	p := open(t, root)

	written, err := storageapi.Save(t.Context(), p, "part.gcode", strings.NewReader("G28\n"))
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(root, "models"), 0o700))

	if os.Symlink("part.gcode", filepath.Join(root, "link.gcode")) != nil ||
		os.Symlink("models", filepath.Join(root, "link-models")) != nil {
		t.Skip("this file system makes no links")
	}

	listed, err := p.List(t.Context(), "")
	require.NoError(t, err)

	// The times and a folder's own size are the file system's, not the store's.
	for i := range listed {
		listed[i].MTime = 0
		if listed[i].IsDir {
			listed[i].Size = 0
		}
	}

	slices.SortFunc(listed, func(a, b storageapi.FileInfo) int { return strings.Compare(a.Name, b.Name) })

	require.Equal(t, []storageapi.FileInfo{
		{Name: "link-models", Path: "link-models", Size: int64(len("models")), Irregular: true},
		{Name: "link.gcode", Path: "link.gcode", Size: int64(len("part.gcode")), Irregular: true},
		{Name: "models", Path: "models", IsDir: true},
		{Name: "part.gcode", Path: "part.gcode", Size: 4, SHA256: written.SHA256},
	}, listed)
}

// A digest older than its object belongs to content the object no longer
// holds, because a copy or an editor changed it in place. Stat and List leave
// it out. A digest as old as its object is kept: a store whose clock counts
// whole seconds writes both in one second (ADR-20261023-sns rule 3).
func TestADigestOlderThanItsObjectIsNotReported(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	p := open(t, root)

	written, err := storageapi.Save(t.Context(), p, "part.gcode", strings.NewReader("G28\n"))
	require.NoError(t, err)

	object := filepath.Join(root, "part.gcode")
	sidecar := filepath.Join(root, ".pcl-meta", "part.gcode.sha256")
	recorded := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(sidecar, recorded, recorded))

	cases := []struct {
		name    string
		changed time.Time
		sha256  string
	}{
		{"the digest is older", recorded.Add(time.Second), ""},
		{"the digest is as old", recorded, written.SHA256},
	}

	for _, held := range cases {
		require.NoError(t, os.Chtimes(object, held.changed, held.changed))

		want := storageapi.FileInfo{
			Name: "part.gcode", Path: "part.gcode", Size: 4,
			MTime: held.changed.Unix(), SHA256: held.sha256,
		}

		described, statErr := p.Stat(t.Context(), "part.gcode")
		require.NoError(t, statErr)

		listed, listErr := p.List(t.Context(), "")
		require.NoError(t, listErr)

		require.Equal(t, []storageapi.FileInfo{want, want},
			append([]storageapi.FileInfo{described}, listed...), held.name)
	}
}
