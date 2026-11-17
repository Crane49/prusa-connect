package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/printfile"
)

// thumbnailDiskDir is where the pictures printers read out of their own files
// are kept, beside the overlay in the directory this server owns.
const thumbnailDiskDir = "printer-thumbnails"

// diskBlock is what the bound counts for a file smaller than one: a verdict of
// no picture is an empty file and still takes a block on disk.
const diskBlock = 4096

// thumbnailDisk keeps the pictures printers read out of their own files across
// a restart. Asking a printer costs about a second per file, so a server that
// forgot them would ask a whole USB stick again after every restart.
//
// One file holds one version of one printer file: the encoded picture, or
// nothing for a version that carries none. The name is a digest of the key,
// so a path the printer chose never becomes a path on this disk. What is read
// back goes through printfile.Picture again, the same check a picture from the
// printer passes.
//
// It is bounded in bytes by core.device_thumbnail_disk_bytes. Over the bound
// the files written longest ago are removed. The sizes and the write order are
// read from the directory once, when it is opened, and kept in memory from
// then on. A nil thumbnailDisk keeps nothing.
type thumbnailDisk struct {
	dir      string
	maxBytes int64

	mu    sync.Mutex
	bytes int64
	// sizes is what the bound counts for each file, by name, and order is the
	// names from the one written longest ago to the one written last.
	sizes map[string]int64
	order []string
}

// openThumbnailDisk makes the directory and reads what it already holds. A
// write a stopped process left half done is removed.
func openThumbnailDisk(dir string, maxBytes int64) (*thumbnailDisk, error) {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, err //nolint:wrapcheck // the path is in the error already.
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err //nolint:wrapcheck // the path is in the error already.
	}

	type kept struct {
		name    string
		size    int64
		written time.Time
	}

	found := make([]kept, 0, len(entries))

	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}

		if strings.HasPrefix(entry.Name(), writingPrefix) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))

			continue
		}

		found = append(found, kept{name: entry.Name(), size: max(info.Size(), diskBlock), written: info.ModTime()})
	}

	slices.SortFunc(found, func(a, b kept) int { return a.written.Compare(b.written) })

	disk := &thumbnailDisk{dir: dir, maxBytes: maxBytes, sizes: make(map[string]int64, len(found))}

	for _, file := range found {
		disk.sizes[file.name] = file.size
		disk.order = append(disk.order, file.name)
		disk.bytes += file.size
	}

	disk.mu.Lock()
	disk.evictLocked()
	disk.mu.Unlock()

	return disk, nil
}

// thumbnailDiskOf opens the disk copy core.device_thumbnail_disk asks for, in
// the directory beside the overlay. Off, or a directory that cannot be made,
// leaves the pictures in memory only.
func thumbnailDiskOf(ctx context.Context, core config.Core, logger *slog.Logger) *thumbnailDisk {
	if !*core.DeviceThumbnailDisk {
		return nil
	}

	disk, err := openThumbnailDisk(
		filepath.Join(filepath.Dir(core.Overlay), thumbnailDiskDir), int64(core.DeviceThumbnailDiskBytes))
	if err != nil {
		logger.WarnContext(ctx, "printer thumbnails are kept in memory only", "error", err.Error())

		return nil
	}

	return disk
}

// writingPrefix starts the name of a file still being written.
const writingPrefix = ".writing-"

func (d *thumbnailDisk) name(key previewKey) string {
	sum := sha256.Sum256([]byte(key.device + "\x00" + key.path + "\x00" +
		strconv.FormatInt(key.size, 10) + "\x00" + strconv.FormatInt(key.modified, 10)))

	return hex.EncodeToString(sum[:])
}

// get returns what was kept for this version of this file. An empty preview
// is a version that carries no picture.
func (d *thumbnailDisk) get(key previewKey) (printfile.Preview, bool) {
	if d == nil {
		return printfile.Preview{}, false
	}

	raw, err := os.ReadFile(filepath.Join(d.dir, d.name(key)))
	if err != nil {
		return printfile.Preview{}, false
	}

	if len(raw) == 0 {
		return printfile.Preview{}, true
	}

	picture := printfile.Picture(raw)
	if picture == nil {
		return printfile.Preview{}, false
	}

	return printfile.Preview{Thumbnail: picture}, true
}

// has reports whether anything is kept for this version, without reading it.
func (d *thumbnailDisk) has(key previewKey) bool {
	if d == nil {
		return false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	_, ok := d.sizes[d.name(key)]

	return ok
}

// put keeps one version's picture, or the verdict that it has none. A failed
// write keeps nothing: the printer is asked again next time.
func (d *thumbnailDisk) put(key previewKey, preview printfile.Preview) {
	if d == nil {
		return
	}

	var raw []byte
	if preview.Thumbnail != nil {
		raw = preview.Thumbnail.Bytes
	}

	name := d.name(key)

	temp, err := os.CreateTemp(d.dir, writingPrefix+"*")
	if err != nil {
		return
	}

	_, err = temp.Write(raw)
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}

	if err == nil {
		err = os.Rename(temp.Name(), filepath.Join(d.dir, name))
	}

	if err != nil {
		_ = os.Remove(temp.Name())

		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if size, ok := d.sizes[name]; ok {
		d.bytes -= size
		d.order = slices.DeleteFunc(d.order, func(held string) bool { return held == name })
	}

	size := max(int64(len(raw)), diskBlock)
	d.sizes[name] = size
	d.order = append(d.order, name)
	d.bytes += size

	d.evictLocked()
}

// evictLocked removes the files written longest ago until the directory is
// inside its bound.
func (d *thumbnailDisk) evictLocked() {
	for d.bytes > d.maxBytes && len(d.order) > 0 {
		oldest := d.order[0]
		d.order = d.order[1:]

		_ = os.Remove(filepath.Join(d.dir, oldest))

		d.bytes -= d.sizes[oldest]
		delete(d.sizes, oldest)
	}
}
