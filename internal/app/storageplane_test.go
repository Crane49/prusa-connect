package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// planeOver stands up the storage plane over a real supervised filesystem
// storage and a real authorization graph.
//
// Both are real rather than faked: what is under test is whether a file reaches
// disk through the supervised call path and whether the graph's answer is
// consulted first, and a fake of either would answer for itself.
func planeOver(t *testing.T, root string) storagePlane {
	t.Helper()

	return storagePlane{graph: newGraph(t), stores: stores{host: stageOne(t, root).host}}
}

func storageSample(size int) []byte {
	source := rand.NewChaCha8([32]byte{19})
	data := make([]byte, size)
	_, _ = source.Read(data)

	return data
}

func storageDigest(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

// requireDenied asserts the refusal is the graph's own and not some other
// failure that happens to be an error. The code is what a client branches on,
// so the code is what is asserted.
func requireDenied(t *testing.T, err error) {
	t.Helper()

	var carried *fault.Fault
	require.ErrorAs(t, err, &carried)
	require.Equal(t, "core.auth/ACCESS_DENIED", carried.Code().String())
}

// The whole loop a person needs: put a file in, see it, read it back, move it
// and take it out again, all through the plane a transport calls.
func TestAFileGoesIntoAStorageAndComesBackOut(t *testing.T) {
	t.Parallel()

	// In a bubble: the supervisor scatters an instance's first start over two
	// seconds, which the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane := planeOver(t, t.TempDir())
		who := subjectOf(config.Default().Principal)

		data := storageSample(300_000)

		written, err := plane.Save(t.Context(), who, "bench", "models/part.gcode", bytes.NewReader(data))
		require.NoError(t, err)
		require.Equal(t, int64(len(data)), written.Size)

		// The digest is the storage's own, taken as it wrote, so comparing it
		// against one taken here compares two readings of the same bytes rather
		// than one number copied twice.
		digest := sha256.Sum256(data)
		require.Equal(t, hex.EncodeToString(digest[:]), written.SHA256)

		// And it is still there afterwards, beside the object, so a caller can
		// check a file it uploaded without downloading it again.
		stat, err := plane.Stat(t.Context(), who, "bench", "models/part.gcode")
		require.NoError(t, err)
		require.Equal(t, written.SHA256, stat.SHA256)

		// The parent directory was made on the way, which is what a caller uploading
		// into a tree it has not created expects.
		entries, err := plane.Files(t.Context(), who, "bench", "models")
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.Equal(t, "models/part.gcode", entries[0].Path)
		require.Equal(t, int64(len(data)), entries[0].Size)
		require.False(t, entries[0].Folder)

		object, err := plane.Open(t.Context(), who, "bench", "models/part.gcode")
		require.NoError(t, err)

		read, err := io.ReadAll(object.Content)
		require.NoError(t, err)
		require.NoError(t, object.Content.Close())
		require.Equal(t, storageDigest(data), storageDigest(read))

		require.NoError(t, plane.CreateFolder(t.Context(), who, "bench", "archive"))
		require.NoError(t, plane.Move(t.Context(), who, "bench", "models/part.gcode", "archive/part.gcode"))

		moved, err := plane.Files(t.Context(), who, "bench", "archive")
		require.NoError(t, err)
		require.Len(t, moved, 1)

		require.NoError(t, plane.Delete(t.Context(), who, "bench", "archive"))

		gone, err := plane.Files(t.Context(), who, "bench", "")
		require.NoError(t, err)

		for _, entry := range gone {
			require.NotEqual(t, "archive", entry.Path)
		}

		t.Logf("observed: %d bytes in with digest %s, listed, read back identical, moved and removed",
			written.Size, written.SHA256)
	})
}

// A folder row says how much is inside it, so a listing can state the count
// without a second round trip per row.
func TestAFolderRowCountsWhatItHolds(t *testing.T) {
	t.Parallel()

	// In a bubble: the supervisor scatters an instance's first start over two
	// seconds, which the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane := planeOver(t, t.TempDir())
		who := subjectOf(config.Default().Principal)

		for _, name := range []string{"models/one.gcode", "models/two.gcode", "loose.gcode"} {
			_, err := plane.Save(t.Context(), who, "bench", name, bytes.NewReader(storageSample(16)))
			require.NoError(t, err)
		}

		entries, err := plane.Files(t.Context(), who, "bench", "")
		require.NoError(t, err)

		counted := map[string]*int{}
		for _, entry := range entries {
			counted[entry.Path] = entry.Entries
		}

		require.NotNil(t, counted["models"])
		require.Equal(t, 2, *counted["models"])
		// A file holds nothing, so it says nothing.
		require.Nil(t, counted["loose.gcode"])
	})
}

// A storage's contents are a separate permission from its declaration, in both
// directions. A subject holding nothing gets neither.
func TestAStoragesContentsTakeTheirOwnPermission(t *testing.T) {
	t.Parallel()

	// In a bubble: the supervisor scatters an instance's first start over two
	// seconds, which the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane := planeOver(t, t.TempDir())

		granted := subjectOf(config.Default().Principal)
		stranger := "user:nobody"

		// The bootstrap subject holds both, which is what makes a fresh deployment
		// usable at all.
		_, err := plane.List(t.Context(), granted)
		require.NoError(t, err)

		_, err = plane.Save(t.Context(), granted, "bench", "ok.gcode", strings.NewReader("G28"))
		require.NoError(t, err)

		// The refusal half. A subject with no grant is refused for reading and for
		// writing, and the refusal is the graph's own.
		_, err = plane.List(t.Context(), stranger)
		require.Error(t, err)
		requireDenied(t, err)
		t.Logf("observed, read refused: %v", err)

		_, err = plane.Save(t.Context(), stranger, "bench", "sneak.gcode", strings.NewReader("x"))
		require.Error(t, err)
		requireDenied(t, err)
		t.Logf("observed, write refused: %v", err)

		require.Error(t, plane.Delete(t.Context(), stranger, "bench", "ok.gcode"))
		require.Error(t, plane.CreateFolder(t.Context(), stranger, "bench", "sneak"))
		require.Error(t, plane.Move(t.Context(), stranger, "bench", "ok.gcode", "sneak.gcode"))

		// Nothing the stranger asked for happened.
		entries, err := plane.Files(t.Context(), granted, "bench", "")
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.Equal(t, "ok.gcode", entries[0].Path)
	})
}

// The thumbnail route answers a 304 from a file's version alone, and a browser
// asks it before every use of a kept picture. So the version read is where a
// person without read_storage is refused, and the file's version and length
// are not answered to them.
func TestAFilesVersionTakesReadStorage(t *testing.T) {
	t.Parallel()

	// In a bubble: the supervisor scatters an instance's first start over two
	// seconds, which the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane := planeOver(t, t.TempDir())
		granted := subjectOf(config.Default().Principal)

		_, err := plane.Save(t.Context(), granted, "bench", "part.gcode", strings.NewReader("G28"))
		require.NoError(t, err)

		version, err := plane.FileVersion(t.Context(), granted, "bench", "part.gcode")
		require.NoError(t, err)
		require.NotEmpty(t, version.Version)

		_, err = plane.FileVersion(t.Context(), "user:nobody", "bench", "part.gcode")
		requireDenied(t, err)

		_, err = plane.FileExtent(t.Context(), "user:nobody", "bench", "part.gcode")
		requireDenied(t, err)
	})
}

// A path that climbs is refused rather than resolved.
//
// The provider would not refuse it: storageapi.CleanRelPath turns
// "../../etc/passwd" into "etc/passwd" and writes it inside the store, so
// nothing escapes and the caller silently gets a different path from the one it
// asked for. This is the refusal that stops that, and it matches what the
// printer side does with the same shape.
func TestAClimbingPathIsRefusedRatherThanResolved(t *testing.T) {
	t.Parallel()

	// In a bubble: the supervisor scatters an instance's first start over two
	// seconds, which the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		root := t.TempDir()
		plane := planeOver(t, root)
		who := subjectOf(config.Default().Principal)

		// The backslash spellings are here because the provider normalises them
		// before it resolves: a refusal that read "/" alone let "..\..\x" through to
		// be silently rewritten, which is the exact behaviour it exists to stop.
		climbing := []string{
			"../escape.gcode", "../../etc/passwd", "models/../../out.gcode", "..",
			`..\escape.gcode`, `..\..\etc\passwd`, `models\..\..\out.gcode`, `models\..`,
		}

		for _, name := range climbing {
			_, err := plane.Save(t.Context(), who, "bench", name, strings.NewReader("x"))
			require.Error(t, err, "saving %q was accepted", name)

			_, err = plane.Files(t.Context(), who, "bench", name)
			require.Error(t, err, "listing %q was accepted", name)

			require.Error(t, plane.Delete(t.Context(), who, "bench", name), "deleting %q was accepted", name)
		}

		// The storage is still empty: not one of those became a file under a name
		// the caller did not ask for.
		entries, err := plane.Files(t.Context(), who, "bench", "")
		require.NoError(t, err)
		require.Empty(t, entries)

		t.Logf("observed: %d climbing paths refused and the storage left empty", len(climbing))
	})
}

// A body over the transport's bound is the published 413 and not a server
// failure.
//
// The breach surfaces inside the provider's copy, which is a supervised call,
// so it reaches the caller through the same mapping every other storage failure
// does. It read as core.storage/UNAVAILABLE, retryable, which told the caller
// to send the oversized file again.
func TestABodyOverTheUploadBoundIsRefusedAsTooLarge(t *testing.T) {
	t.Parallel()

	// In a bubble: the supervisor scatters an instance's first start over two
	// seconds, which the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		const bound = 8

		plane := planeOver(t, t.TempDir())
		who := subjectOf(config.Default().Principal)

		body := http.MaxBytesReader(nil, io.NopCloser(strings.NewReader(strings.Repeat("G", 4096))), bound)

		_, err := plane.Save(t.Context(), who, "bench", "big.gcode", body)
		require.Error(t, err)

		var carried *fault.Fault
		require.ErrorAs(t, err, &carried)
		require.Equal(t, httpfault.CodeBodyTooLarge(), carried.Code())
		require.Equal(t, http.StatusRequestEntityTooLarge, carried.Status())

		t.Logf("observed: %v", err)
	})
}

// An extension the storage does not accept is refused, and the refusal says so
// rather than reporting a missing file.
func TestAnExtensionTheStorageRefusesSaysSo(t *testing.T) {
	t.Parallel()

	// In a bubble: the supervisor scatters an instance's first start over two
	// seconds, which the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane := planeOver(t, t.TempDir())
		who := subjectOf(config.Default().Principal)

		_, err := plane.Save(t.Context(), who, "bench", "payload.sh", strings.NewReader("echo hi"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "extension")
		t.Logf("observed: %v", err)
	})
}

// A storage nobody declared is refused after the permission check and not
// before, so the answer does not depend on what exists.
func TestAnUndeclaredStorageIsRefused(t *testing.T) {
	t.Parallel()

	// In a bubble: the supervisor scatters an instance's first start over two
	// seconds, which the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane := planeOver(t, t.TempDir())
		who := subjectOf(config.Default().Principal)

		_, err := plane.Files(t.Context(), who, "nowhere", "")
		require.ErrorIs(t, err, ErrNoSuchStorage)

		listed, err := plane.List(t.Context(), who)
		require.NoError(t, err)
		require.Len(t, listed, 1)
		require.Equal(t, "bench", listed[0].Name)
		require.True(t, listed[0].Browse)
		require.True(t, listed[0].Write)
		require.ElementsMatch(t, storageapi.DefaultAllowedExtensions, listed[0].AllowedExtensions)
	})
}

// A batch delete removes every entry it names, a folder with what is under it,
// and leaves the rest of the storage alone.
func TestABatchDeleteRemovesEveryEntryItNames(t *testing.T) {
	t.Parallel()

	// In a bubble: the supervisor scatters an instance's first start over two
	// seconds, which the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane := planeOver(t, t.TempDir())
		who := subjectOf(config.Default().Principal)

		for _, name := range []string{"keep.gcode", "a.gcode", "old/one.gcode", "old/two.gcode"} {
			_, err := plane.Save(t.Context(), who, "bench", name, strings.NewReader("G28"))
			require.NoError(t, err)
		}

		require.NoError(t, plane.DeleteMany(t.Context(), who, "bench", []string{"a.gcode", "old"}))

		entries, err := plane.Files(t.Context(), who, "bench", "")
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.Equal(t, "keep.gcode", entries[0].Path)
	})
}

// A batch is checked whole before anything is removed. One path that climbs,
// one that names the root and a caller without write_storage each remove
// nothing, including the entries in the same batch that were fine.
func TestABatchDeleteWithOneBadPathRemovesNothing(t *testing.T) {
	t.Parallel()

	// In a bubble: the supervisor scatters an instance's first start over two
	// seconds, which the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane := planeOver(t, t.TempDir())
		who := subjectOf(config.Default().Principal)

		_, err := plane.Save(t.Context(), who, "bench", "a.gcode", strings.NewReader("G28"))
		require.NoError(t, err)

		for _, bad := range []string{"../a.gcode", `..\a.gcode`, "", "/", "."} {
			err = plane.DeleteMany(t.Context(), who, "bench", []string{"a.gcode", bad})

			var carried *fault.Fault
			require.ErrorAs(t, err, &carried, "deleting %q was accepted", bad)
			require.Equal(t, fault.StorageInvalidPath.Code(), carried.Code())
		}

		requireDenied(t, plane.DeleteMany(t.Context(), "user:nobody", "bench", []string{"a.gcode"}))

		entries, err := plane.Files(t.Context(), who, "bench", "")
		require.NoError(t, err)
		require.Len(t, entries, 1, "a refused batch removed an entry")
	})
}

// The disk storage hands out no URL, so a download from it is served by this
// server. A caller without read_storage learns nothing either way.
func TestADiskStorageHandsOutNoDownloadURL(t *testing.T) {
	t.Parallel()

	// In a bubble: the supervisor scatters an instance's first start over two
	// seconds, which the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane := planeOver(t, t.TempDir())
		who := subjectOf(config.Default().Principal)

		_, err := plane.Save(t.Context(), who, "bench", "a.gcode", strings.NewReader("G28"))
		require.NoError(t, err)

		location, ok, err := plane.DownloadURL(t.Context(), who, "bench", "a.gcode")
		require.NoError(t, err)
		require.False(t, ok)
		require.Empty(t, location)

		_, _, err = plane.DownloadURL(t.Context(), "user:nobody", "bench", "a.gcode")
		requireDenied(t, err)
	})
}

// Only an absolute http or https URL is one a client is redirected to. The URL
// is written by the storage's type, and a redirect to any other scheme sends
// the caller's browser wherever that type chose.
func TestOnlyAWebURLIsFetchable(t *testing.T) {
	t.Parallel()

	for location, want := range map[string]bool{
		"https://objects.example.com/a.gcode?sig=1": true,
		"http://10.0.0.5:9000/bucket/a.gcode":       true,
		"javascript:alert(1)":                       false,
		"file:///etc/passwd":                        false,
		"/api/v1/storages/bench/content":            false,
		"//objects.example.com/a.gcode":             false,
		"https:///a.gcode":                          false,
		"":                                          false,
		"https://bad host/a.gcode":                  false,
	} {
		require.Equal(t, want, fetchable(location), location)
	}
}

var _ httpapi.Storages = storagePlane{}
