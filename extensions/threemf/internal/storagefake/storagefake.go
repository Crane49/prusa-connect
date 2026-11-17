// Package storagefake is a storage in memory for the tests of this module. It
// answers the storage wire the way the host does: a file a person may not
// read is ErrDenied, a file that is not there is ErrNoFile.
package storagefake

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"sync"
	"sync/atomic"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"

	"github.com/prusa3d/threemf/internal/projects"
)

// Files is a set of files of named storages.
type Files struct {
	mu    sync.Mutex
	files map[string]file
	// denied is the people who may read nothing.
	denied map[string]bool
	// Reads counts the ReadAt calls on opened files.
	Reads atomic.Int64
	// Opens counts the files opened.
	Opens atomic.Int64
}

type file struct {
	data     []byte
	modified int64
}

// Put stores data at storage:name, written at modified Unix seconds.
func (f *Files) Put(storage, name string, data []byte, modified int64) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.files == nil {
		f.files = map[string]file{}
	}

	f.files[storage+":"+name] = file{data: data, modified: modified}
}

// Deny makes every read by principal a refusal.
func (f *Files) Deny(principal string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.denied == nil {
		f.denied = map[string]bool{}
	}

	f.denied[principal] = true
}

func (f *Files) find(principal, storage, name string) (file, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.denied[principal] {
		return file{}, fmt.Errorf("storagefake: %s: %w", principal, storageapi.ErrDenied)
	}

	found, ok := f.files[storage+":"+name]
	if !ok {
		return file{}, fmt.Errorf("storagefake: %s:%s: %w", storage, name, storageapi.ErrNoFile)
	}

	return found, nil
}

// Stat answers what the host answers for a file.
func (f *Files) Stat(_ context.Context, params storageapi.StatParams) (storageapi.FileInfo, error) {
	found, err := f.find(params.Principal, params.Storage, params.Path)
	if err != nil {
		return storageapi.FileInfo{}, err
	}

	return storageapi.FileInfo{
		Name: path.Base(params.Path), Path: params.Path, Size: int64(len(found.data)), MTime: found.modified,
	}, nil
}

// Open answers a reader over the file.
func (f *Files) Open(_ context.Context, target storageapi.Target) (projects.File, error) {
	found, err := f.find(target.Principal, target.Storage, target.Path)
	if err != nil {
		return nil, err
	}

	f.Opens.Add(1)

	return &counted{Reader: bytes.NewReader(found.data), reads: &f.Reads}, nil
}

type counted struct {
	*bytes.Reader

	reads *atomic.Int64
}

func (c *counted) ReadAt(p []byte, off int64) (int, error) {
	c.reads.Add(1)

	n, err := c.Reader.ReadAt(p, off)
	if err != nil && err != io.EOF {
		return n, fmt.Errorf("storagefake: %w", err)
	}

	return n, err //nolint:wrapcheck // io.EOF is compared by identity
}
