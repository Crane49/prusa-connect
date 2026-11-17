package files_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// pageSize is how many entries one List of fakeStorage answers, small so the
// tests read more than one page.
const pageSize = 2

// stored is one file of fakeStorage.
type stored struct {
	data   string
	mtime  int64
	digest bool
}

// fakeStorage is the host's storage wire over files in memory. A path holds a
// file; the folders are the paths above them. It counts the reads, so a test
// sees which files the module hashed.
type fakeStorage struct {
	mu       sync.Mutex
	storages map[string]map[string]stored
	reads    map[string]int
	broken   map[string]bool
	gate     chan struct{}
	entered  chan struct{}
}

func newStorage() *fakeStorage {
	return &fakeStorage{
		storages: map[string]map[string]stored{},
		reads:    map[string]int{},
		broken:   map[string]bool{},
	}
}

// put stores one file. digest says whether the storage keeps its SHA-256.
func (f *fakeStorage) put(storage, file, data string, digest bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.storages[storage] == nil {
		f.storages[storage] = map[string]stored{}
	}

	f.storages[storage][file] = stored{data: data, mtime: 1700000000, digest: digest}
}

// rewrite changes a file in place, as an editor does: a later time of change
// and no digest.
func (f *fakeStorage) rewrite(storage, file, data string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	was := f.storages[storage][file]
	f.storages[storage][file] = stored{data: data, mtime: was.mtime + 1}
}

func (f *fakeStorage) dropStorage(storage string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.storages, storage)
}

// breakFolder makes every List of one folder fail with a reason that is not a
// missing file.
func (f *fakeStorage) breakFolder(storage, folder string, broken bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.broken[storage+"/"+folder] = broken
}

// breakFile makes every Read of one file fail with a reason that is not a
// missing file, as a file the server process may not read does.
func (f *fakeStorage) breakFile(storage, file string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.broken[storage+"/"+file] = true
}

func (f *fakeStorage) readsOf(storage, file string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.reads[storage+"/"+file]
}

// holdLists makes every List wait until release is called, after it told entered
// that a List is waiting.
func (f *fakeStorage) holdLists() (entered <-chan struct{}, release func()) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.gate, f.entered = make(chan struct{}), make(chan struct{}, 1000)

	return f.entered, sync.OnceFunc(func() { close(f.gate) })
}

func (f *fakeStorage) List(_ context.Context, p storageapi.ListParams) (storageapi.Listing, error) {
	f.mu.Lock()
	gate, entered := f.gate, f.entered
	f.mu.Unlock()

	if gate != nil && p.Path != "" {
		entered <- struct{}{}
		<-gate
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.broken[p.Storage+"/"+p.Path] {
		return storageapi.Listing{}, storageapi.ErrUnreadable
	}

	files, ok := f.storages[p.Storage]
	if !ok {
		return storageapi.Listing{}, storageapi.ErrNoFile
	}

	entries := map[string]storageapi.ListEntry{}
	prefix := ""
	if p.Path != "" {
		prefix = p.Path + "/"
	}

	for name, file := range files {
		rest, under := strings.CutPrefix(name, prefix)
		if !under {
			continue
		}

		if first, _, deeper := strings.Cut(rest, "/"); deeper {
			entries[first] = storageapi.ListEntry{Name: first, Dir: true}

			continue
		}

		entry := storageapi.ListEntry{Name: rest, Size: int64(len(file.data)), MTime: file.mtime}
		if file.digest {
			sum := sha256.Sum256([]byte(file.data))
			entry.SHA256 = hex.EncodeToString(sum[:])
		}

		entries[rest] = entry
	}

	if len(entries) == 0 && p.Path != "" {
		return storageapi.Listing{}, storageapi.ErrNoFile
	}

	names := slices.Sorted(func(yield func(string) bool) {
		for name := range entries {
			if !yield(name) {
				return
			}
		}
	})

	start := 0
	if p.Cursor != "" {
		start, _ = strconv.Atoi(p.Cursor)
	}

	end := min(start+pageSize, len(names))
	page := storageapi.Listing{}

	for _, name := range names[start:end] {
		page.Entries = append(page.Entries, entries[name])
	}

	if end < len(names) {
		page.Next = strconv.Itoa(end)
	}

	return page, nil
}

func (f *fakeStorage) Stat(_ context.Context, p storageapi.StatParams) (storageapi.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	file, ok := f.storages[p.Storage][p.Path]
	if !ok {
		return storageapi.FileInfo{}, storageapi.ErrNoFile
	}

	return storageapi.FileInfo{
		Name: path.Base(p.Path), Path: p.Path, Size: int64(len(file.data)), MTime: file.mtime,
	}, nil
}

func (f *fakeStorage) Read(_ context.Context, p storageapi.ReadParams) (storageapi.Chunk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	file, ok := f.storages[p.Storage][p.Path]
	if !ok {
		return storageapi.Chunk{}, storageapi.ErrNoFile
	}

	if f.broken[p.Storage+"/"+p.Path] {
		return storageapi.Chunk{}, storageapi.ErrUnreadable
	}

	if p.Offset == 0 {
		f.reads[p.Storage+"/"+p.Path]++
	}

	if p.Offset > int64(len(file.data)) {
		return storageapi.Chunk{}, fmt.Errorf("offset %d: %w", p.Offset, storageapi.ErrInvalidRange)
	}

	// Two bytes at a time, so a test reads a file in more than one chunk.
	end := min(p.Offset+2, int64(len(file.data)))

	return storageapi.Chunk{
		Data: []byte(file.data[p.Offset:end]),
		Size: int64(len(file.data)),
		EOF:  end == int64(len(file.data)),
	}, nil
}
