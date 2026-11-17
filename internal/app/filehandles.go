package app

import (
	"crypto/rand"
	"encoding/base64"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// A read handle names one file for one module instance, for as long as one
// call the host makes to that module is open. The module reads the file
// through it without read_storage, and reads nothing else through it.

// fileHandle is what one handle stands for.
type fileHandle struct {
	// principal is the instance the handle was made for, as its own
	// principal. Another instance presenting it is refused.
	principal string
	storage   string
	path      string
	// size and modified are the version of the file the host saw. A file that
	// changed since reads as absent.
	size     int64
	modified int64
}

// fileHandles is every open handle. It holds one entry per call in
// flight, so it grows with the calls the host makes and not with what a
// module sends.
type fileHandles struct {
	mu   sync.Mutex
	held map[string]fileHandle
}

func newFileHandles() *fileHandles {
	return &fileHandles{held: map[string]fileHandle{}}
}

// handleBytes is the randomness in one handle: enough that a module guessing
// another call's handle never finds one.
const handleBytes = 24

// open makes a handle for one file and one instance. The handle ends when
// close is called, which the caller defers to the end of its call.
func (h *fileHandles) open(principal, storage string, info storageapi.FileInfo) (string, func()) {
	raw := make([]byte, handleBytes)
	_, _ = rand.Read(raw) // crypto/rand.Read never fails on a supported platform.
	token := base64.RawURLEncoding.EncodeToString(raw)

	h.mu.Lock()
	h.held[token] = fileHandle{
		principal: principal, storage: storage, path: info.Path, size: info.Size, modified: info.MTime,
	}
	h.mu.Unlock()

	return token, func() {
		h.mu.Lock()
		delete(h.held, token)
		h.mu.Unlock()
	}
}

// resolve answers the file a handle names, when it is open and was made for
// this instance.
func (h *fileHandles) resolve(principal, token string) (fileHandle, bool) {
	if h == nil {
		return fileHandle{}, false
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	found, ok := h.held[token]
	if !ok || found.principal != principal {
		return fileHandle{}, false
	}

	return found, true
}
