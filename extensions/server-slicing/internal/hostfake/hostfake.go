// Package hostfake is the part of the server this module calls, held in
// memory for the tests: one storage, and the answer to whether a person may
// read it, write it and configure modules.
package hostfake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// Host is one storage named Name and the grants on it. The module itself is
// the empty subject.
type Host struct {
	Name string

	mu    sync.Mutex
	files map[string][]byte
	may   map[string][]string
}

// New returns a host with one storage and no grants.
func New(name string) *Host {
	return &Host{Name: name, files: map[string][]byte{}, may: map[string][]string{}}
}

// Grant lets subject take each action: read_storage, write_storage, or
// configure_resource for an administrator.
func (h *Host) Grant(subject string, actions ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.may[subject] = append(h.may[subject], actions...)
}

// Put stores one file.
func (h *Host) Put(path string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.files[path] = data
}

// File answers one stored file.
func (h *Host) File(path string) ([]byte, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	data, ok := h.files[path]

	return data, ok
}

func (h *Host) allowed(subject, action string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Contains(h.may[subject], action)
}

// Stat describes one file.
func (h *Host) Stat(_ context.Context, p storageapi.StatParams) (storageapi.FileInfo, error) {
	if !h.allowed(p.Principal, "read_storage") {
		return storageapi.FileInfo{}, storageapi.ErrDenied
	}

	data, ok := h.File(p.Path)
	if !ok || p.Storage != h.Name {
		return storageapi.FileInfo{}, storageapi.ErrNoFile
	}

	return storageapi.FileInfo{
		Name: p.Path[strings.LastIndex(p.Path, "/")+1:],
		Path: p.Path,
		Size: int64(len(data)),
	}, nil
}

// Read answers one range of one file.
func (h *Host) Read(_ context.Context, p storageapi.ReadParams) (storageapi.Chunk, error) {
	if !h.allowed(p.Principal, "read_storage") {
		return storageapi.Chunk{}, storageapi.ErrDenied
	}

	data, ok := h.File(p.Path)
	if !ok || p.Storage != h.Name {
		return storageapi.Chunk{}, storageapi.ErrNoFile
	}

	start := min(p.Offset, int64(len(data)))
	end := min(int64(len(data)), start+int64(p.Length))

	return storageapi.Chunk{Data: data[start:end], Size: int64(len(data)), EOF: end == int64(len(data))}, nil
}

// Write stores one file.
func (h *Host) Write(_ context.Context, p storageapi.WriteParams, body io.Reader) (storageapi.Written, error) {
	if !h.allowed(p.Principal, "write_storage") {
		return storageapi.Written{}, storageapi.ErrDenied
	}

	if p.Storage != h.Name {
		return storageapi.Written{}, storageapi.ErrNoFile
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return storageapi.Written{}, err
	}

	h.Put(p.Path, data)

	sum := sha256.Sum256(data)

	return storageapi.Written{Path: p.Path, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}, nil
}

// Check answers from the grants.
func (h *Host) Check(_ context.Context, p accessapi.CheckParams) (accessapi.Decision, error) {
	if p.ObjectType != "resource_kind" {
		return accessapi.Decision{}, accessapi.ErrInvalidQuestion
	}

	return accessapi.Decision{Allowed: h.allowed(p.Subject, p.Action)}, nil
}
