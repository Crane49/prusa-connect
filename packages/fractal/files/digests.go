package files

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	fractalfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// digestsFile is the file in the module's data directory with the identities
// the module computed itself.
const digestsFile = "files-digests.json"

// digestKey names one version of one file. A file whose size or time of
// change differs is another version and is hashed again.
type digestKey struct {
	Storage string `json:"storage"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	MTime   int64  `json:"mtime"`
}

// digestRow is one line of digestsFile.
type digestRow struct {
	digestKey

	Identity string `json:"identity"`
}

// digests is the identities the module computed, kept across restarts so a
// restart reads no file again. Only files a scan visited stay, so the file
// does not grow with folders that are no longer shared.
type digests struct {
	file string

	mu    sync.Mutex
	held  map[digestKey]fractalfiles.Identity
	dirty bool
}

// openDigests reads digestsFile from dataDir. A file that is missing or that
// this module cannot read gives an empty set, and the files are hashed again.
func openDigests(dataDir string, log *slog.Logger) (*digests, error) {
	d := &digests{file: filepath.Join(dataDir, digestsFile), held: map[digestKey]fractalfiles.Identity{}}

	data, err := os.ReadFile(d.file)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read the computed digests: %w", err)
	}

	var rows []digestRow
	if err = json.Unmarshal(data, &rows); err != nil {
		log.Warn("the computed digests cannot be read and every file is hashed again",
			"file", d.file, "error", err)

		return d, nil
	}

	for _, row := range rows {
		if id, parseErr := fractalfiles.ParseText(row.Identity); parseErr == nil {
			d.held[row.digestKey] = id
		}
	}

	return d, nil
}

func (d *digests) get(key digestKey) (fractalfiles.Identity, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	id, ok := d.held[key]

	return id, ok
}

func (d *digests) put(key digestKey, id fractalfiles.Identity) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.held[key] = id
	d.dirty = true
}

// keep drops every digest of storage that visited does not name.
func (d *digests) keep(storage string, visited map[digestKey]bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for key := range d.held {
		if key.Storage == storage && !visited[key] {
			delete(d.held, key)
			d.dirty = true
		}
	}
}

// save writes the digests when they changed since the last save.
func (d *digests) save() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.dirty {
		return nil
	}

	rows := make([]digestRow, 0, len(d.held))
	for key, id := range d.held {
		rows = append(rows, digestRow{digestKey: key, Identity: id.Text()})
	}

	if err := writeAtomically(d.file, rows); err != nil {
		return err
	}

	d.dirty = false

	return nil
}
