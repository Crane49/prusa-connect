package app

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printfile"
)

func diskPicture(t *testing.T) printfile.Preview {
	t.Helper()

	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, image.NewGray(image.Rect(0, 0, 16, 16))))

	picture := printfile.Picture(encoded.Bytes())
	require.NotNil(t, picture)

	return printfile.Preview{Thumbnail: picture}
}

// A picture and a verdict of no picture both come back after the directory is
// opened again, which is what a restart does.
func TestAPrinterThumbnailOnDiskOutlivesTheProcess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	picture := diskPicture(t)
	shown := previewKey{device: "p1", path: "usb/a.bgcode", size: 10, modified: 1}
	blank := previewKey{device: "p1", path: "usb/b.bgcode", size: 20, modified: 2}

	first, err := openThumbnailDisk(dir, 1<<20)
	require.NoError(t, err)
	first.put(shown, picture)
	first.put(blank, printfile.Preview{})

	again, err := openThumbnailDisk(dir, 1<<20)
	require.NoError(t, err)

	held, ok := again.get(shown)
	require.True(t, ok)
	require.Equal(t, picture.Thumbnail.Bytes, held.Thumbnail.Bytes)

	held, ok = again.get(blank)
	require.True(t, ok, "the verdict of no picture was lost")
	require.Nil(t, held.Thumbnail)

	_, ok = again.get(previewKey{device: "p1", path: "usb/a.bgcode", size: 10, modified: 2})
	require.False(t, ok, "another version of the file was served the old picture")
}

// Over its bound the directory drops the files written longest ago, both
// while it runs and when it is opened again with a smaller bound.
func TestPrinterThumbnailsOnDiskDropTheOldestOverTheBound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	disk, err := openThumbnailDisk(dir, 3*diskBlock)
	require.NoError(t, err)

	keys := []previewKey{
		{device: "p1", path: "usb/1.bgcode", size: 1},
		{device: "p1", path: "usb/2.bgcode", size: 2},
		{device: "p1", path: "usb/3.bgcode", size: 3},
		{device: "p1", path: "usb/4.bgcode", size: 4},
	}

	for _, key := range keys {
		disk.put(key, printfile.Preview{})
	}

	require.False(t, disk.has(keys[0]), "the oldest file was kept over the bound")
	require.FileExists(t, filepath.Join(dir, disk.name(keys[1])))

	_, err = os.Stat(filepath.Join(dir, disk.name(keys[0])))
	require.ErrorIs(t, err, os.ErrNotExist, "the dropped file is still on disk")

	// Modification times are not finer than a second on every filesystem, so
	// the order a reopen reads is set by hand.
	written := time.Now().Add(-time.Hour)
	for _, key := range []previewKey{keys[3], keys[1], keys[2]} {
		require.NoError(t, os.Chtimes(filepath.Join(dir, disk.name(key)), written, written))
		written = written.Add(time.Minute)
	}

	again, err := openThumbnailDisk(dir, 2*diskBlock)
	require.NoError(t, err)

	require.False(t, again.has(keys[3]), "the file dated oldest was kept over the smaller bound")
	require.True(t, again.has(keys[1]))
	require.True(t, again.has(keys[2]))
}
