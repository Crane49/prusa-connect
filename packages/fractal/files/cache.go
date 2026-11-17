package files

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

const (
	// partPattern names a copy still being written.
	partPattern = ".part-*"
	// pathMarker records the Node Path the copies were fetched under.
	// OpenCache adopts no copy without it.
	pathMarker = ".node-path"
	// forgottenList names the copies Follow forgot and could not delete yet,
	// as a JSON array of file names. OpenCache deletes them.
	forgottenList = ".forgotten"
	// namePrefix starts the file name of a copy. The rest is the hex digest:
	// Windows forbids ":" in a file name and base64url is case-sensitive.
	namePrefix = "sha256-"
	// dirMode is the permission of the folders of the cache.
	dirMode = 0o750
)

var _ Copies = (*Cache)(nil)

// Cache is the downward cache, the folder print/ of the module's data
// directory.
type Cache struct {
	dir      string
	maxBytes int64
	log      *slog.Logger

	mu     sync.Mutex
	copies map[sdkfiles.Identity]*held
	clock  uint64
	total  int64
	// dropped closes at the next Drop, which ends every fetch begun before it.
	dropped chan struct{}
	// undeleted is the files of copies a Drop forgot and could not delete.
	// Every later Drop and unpin tries again.
	undeleted []string
}

// held is one kept copy.
type held struct {
	size int64
	used uint64
	pins int
}

// OpenCache opens the cache in dir. It deletes every entry that is not a
// copy that hashes to its name. It deletes every copy when no Node Path is
// recorded, which is the state after a Drop. A nil log discards the lines.
func OpenCache(dir string, maxBytes int64, log *slog.Logger) (*Cache, error) {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("create the cache folder: %w", err)
	}

	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	cache := &Cache{dir: dir, maxBytes: maxBytes, log: log, copies: map[sdkfiles.Identity]*held{},
		dropped: make(chan struct{}),
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read the cache folder: %w", err)
	}

	_, markerErr := os.Stat(filepath.Join(dir, pathMarker))
	adopting := markerErr == nil

	forgotten, err := readForgotten(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if slices.Contains(forgotten, entry.Name()) {
			cache.undeleted = append(cache.undeleted, filepath.Join(dir, entry.Name()))

			continue
		}

		if err = cache.adopt(entry, adopting); err != nil {
			return nil, err
		}
	}

	if err = cache.recordForgotten(); err != nil {
		return nil, err
	}

	cache.evict(sdkfiles.Identity{})

	return cache, nil
}

// adopt keeps one entry of the folder when adopting and it is a copy that
// hashes to its name, and deletes it otherwise.
func (c *Cache) adopt(entry fs.DirEntry, adopting bool) error {
	name := entry.Name()
	if name == pathMarker || name == forgottenList {
		return nil
	}

	path := filepath.Join(c.dir, name)

	id, ok := identityOfName(name)
	if adopting && ok && entry.Type().IsRegular() {
		size, matches, err := hashesTo(path, id)
		if err != nil {
			c.log.Warn("a copy in the cache cannot be read, so it is deleted", "file", name, "error", err.Error())
		}

		if err == nil && matches {
			c.copies[id] = &held{size: size}
			c.total += size

			return nil
		}
	}

	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("delete %s from the cache: %w", name, err)
	}

	return nil
}

// Open opens and pins a copy to serve a child. It counts as a use.
func (c *Cache) Open(id sdkfiles.Identity) (copied *os.File, size int64, unpin func(), ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.copies[id]
	if !ok {
		return nil, 0, nil, false
	}

	opened, err := os.Open(c.place(id)) //nolint:gosec // G703: the name is a hex digest inside the cache folder
	if err != nil {
		return nil, 0, nil, false
	}

	c.clock++
	entry.used = c.clock

	return opened, entry.size, c.pinLocked(entry), true
}

// Put writes a copy with fill and keeps it under its identity. A copy fill
// does not finish is deleted. So is one whose retention is not unlimited, and
// Put then returns ErrRetention.
func (c *Cache) Put(id sdkfiles.Identity, fill func(Sink) (Retention, error)) error {
	_, _, err := c.store(id, fill, false)

	return err
}

// PutHeld is Put, and pins the copy before any other caller can release it.
// It returns the path of the copy as Path does.
func (c *Cache) PutHeld(id sdkfiles.Identity, fill func(Sink) (Retention, error)) (string, func(), error) {
	return c.store(id, fill, true)
}

// Hold pins a copy and returns its path as Path does, in one step, so no
// unpin or eviction falls between the two.
func (c *Cache) Hold(id sdkfiles.Identity) (path string, unpin func(), ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.copies[id]
	if !ok {
		return "", nil, false
	}

	return fileName(id), c.pinLocked(entry), true
}

func (c *Cache) store(id sdkfiles.Identity, fill func(Sink) (Retention, error), pin bool) (string, func(), error) {
	c.mu.Lock()
	dropped := c.dropped
	c.mu.Unlock()

	part, err := os.CreateTemp(c.dir, partPattern)
	if err != nil {
		return "", nil, fmt.Errorf("open a new copy: %w", err)
	}

	kept, err := fill(part)
	if err == nil && !kept.Unlimited {
		err = fmt.Errorf("%w: %s", ErrRetention, kept)
	}

	size, sizeErr := part.Seek(0, io.SeekEnd)
	closeErr := part.Close()

	if err = errors.Join(err, sizeErr, closeErr); err != nil {
		_ = os.Remove(part.Name()) // OpenCache deletes a part file left behind

		return "", nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// A copy fetched from a parent that was dropped meanwhile is not kept.
	select {
	case <-dropped:
		_ = os.Remove(part.Name()) // OpenCache deletes a part file left behind

		return "", nil, ErrDropped
	default:
	}

	placed := c.place(id)
	entry, ok := c.copies[id]
	written := ok || slices.Contains(c.undeleted, placed)

	if err = os.Rename(part.Name(), placed); err != nil {
		_ = os.Remove(part.Name()) // OpenCache deletes a part file left behind

		// On Windows a copy a reader has open cannot be replaced. A copy
		// already under this name has the same bytes: the name is the digest.
		if !written {
			return "", nil, fmt.Errorf("keep the copy: %w", err)
		}
	}

	c.undeleted = slices.DeleteFunc(c.undeleted, func(path string) bool { return path == placed })

	// The entry is updated in place, so an unpin from before this Put still
	// releases its pin.
	if !ok {
		entry = &held{}
		c.copies[id] = entry
	} else {
		c.total -= entry.size
	}

	c.clock++
	entry.size, entry.used = size, c.clock
	c.total += size
	c.evict(id)

	if !pin {
		return "", nil, nil
	}

	return fileName(id), c.pinLocked(entry), nil
}

// Path returns the path of a copy relative to print/, with "/" as the
// separator, and "" when the cache holds none.
func (c *Cache) Path(id sdkfiles.Identity) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.copies[id]; !ok {
		return ""
	}

	return fileName(id)
}

// Pin keeps a copy from eviction until unpin runs.
func (c *Cache) Pin(id sdkfiles.Identity) (unpin func()) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.copies[id]
	if !ok {
		return func() {}
	}

	return c.pinLocked(entry)
}

// pinLocked pins entry. The caller holds c.mu.
func (c *Cache) pinLocked(entry *held) (unpin func()) {
	entry.pins++

	var once sync.Once

	return func() { once.Do(func() { c.unpin(entry) }) }
}

func (c *Cache) unpin(entry *held) {
	c.mu.Lock()
	defer c.mu.Unlock()

	_ = c.deleteForgotten() // what it cannot delete stays in c.undeleted

	entry.pins--
	if entry.pins > 0 {
		return
	}

	c.evict(sdkfiles.Identity{})
}

// Drop forgets every copy, ends the fetches begun before it and deletes the
// Node Path record. It returns an error for the files it could not delete,
// and a later Drop or unpin tries them again. On Windows a copy a reader has
// open cannot be deleted.
func (c *Cache) Drop() error {
	dropped, markerErr, filesErr := c.drop()
	if dropped > 0 {
		c.log.Info("the copies of files from above were dropped", "copies", dropped)
	}

	if err := errors.Join(markerErr, filesErr); err != nil {
		return fmt.Errorf("drop the copies: %w", err)
	}

	return nil
}

// drop is Drop. It returns the number of copies it forgot, and the two
// failures apart: the Node Path record and the forgotten files.
func (c *Cache) drop() (dropped int, markerErr, filesErr error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	close(c.dropped)
	c.dropped = make(chan struct{})

	dropped = len(c.copies)
	for id := range c.copies {
		c.undeleted = append(c.undeleted, c.place(id))
	}

	clear(c.copies)
	c.total = 0

	markerErr = os.Remove(filepath.Join(c.dir, pathMarker))
	if errors.Is(markerErr, fs.ErrNotExist) {
		markerErr = nil
	}

	return dropped, markerErr, c.deleteForgotten()
}

// deleteForgotten deletes the files of forgotten copies, and keeps the ones it
// cannot delete for the next try. The caller holds c.mu.
func (c *Cache) deleteForgotten() error {
	var failed []error

	c.undeleted = slices.DeleteFunc(c.undeleted, func(path string) bool {
		err := os.Remove(path)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return true
		}

		failed = append(failed, err)

		return false
	})

	return errors.Join(failed...)
}

// untilDropped returns ctx, cancelled with ErrDropped at the next Drop.
func (c *Cache) untilDropped(ctx context.Context) (context.Context, context.CancelFunc) {
	c.mu.Lock()
	dropped := c.dropped
	c.mu.Unlock()

	bound, cancel := context.WithCancelCause(ctx)

	go func() {
		select {
		case <-dropped:
			cancel(ErrDropped)
		case <-bound.Done():
		}
	}()

	return bound, func() { cancel(nil) }
}

// Follow drops every copy when path is not the Node Path the copies were
// fetched under, and records path. The process restarts on a new Node Path,
// so only the record on disk tells it that the path changed.
func (c *Cache) Follow(path fractal.NodePath) error {
	marker := filepath.Join(c.dir, pathMarker)

	was, err := os.ReadFile(marker)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read the node path of the copies: %w", err)
	}

	if err == nil && string(was) == string(path) {
		return nil
	}

	dropped, markerErr, filesErr := c.drop()
	if dropped > 0 {
		c.log.Info("the copies of files from above were dropped for a new node path", "path", path, "copies", dropped)
	}

	if markerErr != nil {
		return fmt.Errorf("drop the copies: %w", markerErr)
	}

	// A file a reader has open must not keep the old path. The list on disk
	// stops a restart from adopting it under the new path.
	if filesErr != nil {
		c.log.Warn("some dropped copies could not be deleted yet", "error", filesErr.Error())

		if err = c.recordForgotten(); err != nil {
			return err
		}
	}

	if err = os.WriteFile(marker, []byte(path), fileMode); err != nil {
		return fmt.Errorf("record the node path of the copies: %w", err)
	}

	return nil
}

// recordForgotten writes the names of the copies still to delete to
// forgottenList. shortcut: a name stays listed after a later unpin deletes
// it, so a restart deletes a fresh copy of the same object.
func (c *Cache) recordForgotten() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_ = c.deleteForgotten() // what it cannot delete stays in c.undeleted

	list := filepath.Join(c.dir, forgottenList)

	if len(c.undeleted) == 0 {
		if err := os.Remove(list); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("delete the list of copies to delete: %w", err)
		}

		return nil
	}

	names := make([]string, 0, len(c.undeleted))
	for _, path := range c.undeleted {
		names = append(names, filepath.Base(path))
	}

	return writeAtomically(list, names)
}

// readForgotten reads forgottenList, and no names when there is none.
func readForgotten(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, forgottenList))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	var names []string
	if err == nil {
		err = json.Unmarshal(data, &names)
	}

	if err != nil {
		return nil, fmt.Errorf("read the copies to delete: %w", err)
	}

	return names, nil
}

// evict deletes the copies used least recently until the cache fits maxBytes.
// It keeps a pinned copy and spare. The caller holds c.mu.
func (c *Cache) evict(spare sdkfiles.Identity) {
	stuck := map[sdkfiles.Identity]bool{}

	for c.total > c.maxBytes {
		var (
			victim sdkfiles.Identity
			oldest uint64
			found  bool
		)

		for id, entry := range c.copies {
			if entry.pins > 0 || id == spare || stuck[id] {
				continue
			}

			if !found || entry.used < oldest {
				victim, oldest, found = id, entry.used, true
			}
		}

		if !found {
			return
		}

		if !removed(c.place(victim)) {
			stuck[victim] = true

			continue
		}

		c.total -= c.copies[victim].size
		delete(c.copies, victim)
	}
}

// removed deletes the file at path and reports whether it is gone.
func removed(path string) bool {
	err := os.Remove(path) //nolint:gosec // G703: every caller passes a path inside the cache folder

	return err == nil || errors.Is(err, fs.ErrNotExist)
}

func (c *Cache) place(id sdkfiles.Identity) string {
	return filepath.Join(c.dir, fileName(id))
}

func fileName(id sdkfiles.Identity) string {
	return namePrefix + hex.EncodeToString(id.Bytes()[1:])
}

// identityOfName reads the identity of a copy's file name, and refuses any
// name that fileName would not give.
func identityOfName(name string) (sdkfiles.Identity, bool) {
	digest, ok := strings.CutPrefix(name, namePrefix)
	if !ok {
		return sdkfiles.Identity{}, false
	}

	raw, err := hex.DecodeString(digest)
	if err != nil {
		return sdkfiles.Identity{}, false
	}

	id, err := sdkfiles.ParseBytes(append([]byte{byte(sdkfiles.SHA256)}, raw...))
	if err != nil || fileName(id) != name {
		return sdkfiles.Identity{}, false
	}

	return id, true
}

// hashesTo reports the size of the file at path and whether it hashes to id.
func hashesTo(path string, id sdkfiles.Identity) (int64, bool, error) {
	opened, err := os.Open(path)
	if err != nil {
		return 0, false, fmt.Errorf("open a copy: %w", err)
	}
	defer opened.Close()

	hash := sdkfiles.NewHasher()

	size, err := io.Copy(hash, opened)
	if err != nil {
		return 0, false, fmt.Errorf("read a copy: %w", err)
	}

	return size, hash.Identity() == id, nil
}

// head returns up to n first bytes of a copy.
func (c *Cache) head(id sdkfiles.Identity, n int) ([]byte, error) {
	c.mu.Lock()
	_, ok := c.copies[id]
	c.mu.Unlock()

	if !ok {
		return nil, fs.ErrNotExist
	}

	opened, err := os.Open(c.place(id))
	if err != nil {
		return nil, fmt.Errorf("open a copy: %w", err)
	}
	defer opened.Close()

	first := make([]byte, n)

	read, err := io.ReadFull(opened, first)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read a copy: %w", err)
	}

	return first[:read], nil
}
