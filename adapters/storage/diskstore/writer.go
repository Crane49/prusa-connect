package diskstore

import (
	"context"
	"errors"
	"hash"
	"io/fs"
	"os"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// writer is one object being written: a temporary file beside the
// destination, the digest of the bytes so far, and the confinement handle.
type writer struct {
	root        *os.Root
	file        *os.File
	temp        string
	destination string
	published   string // the "/"-separated name the caller addresses
	digest      hash.Hash
	size        int64
	finished    bool
}

var _ storageapi.Writer = (*writer)(nil)

// Write writes to the temporary file and hashes the bytes that were written.
func (w *writer) Write(p []byte) (int, error) {
	if w.finished {
		return 0, storageapi.ErrWriteClosed
	}

	written, err := w.file.Write(p)
	w.size += int64(written)

	// hash.Hash never reports an error and never writes short.
	_, _ = w.digest.Write(p[:written])

	return written, err
}

// Commit publishes the object under its name and reports what was written.
func (w *writer) Commit(ctx context.Context) (storageapi.WriteResult, error) {
	if w.finished {
		return storageapi.WriteResult{}, storageapi.ErrWriteClosed
	}

	if err := ctx.Err(); err != nil {
		return storageapi.WriteResult{}, err
	}

	// Sync before the rename, so a power cut cannot leave the destination
	// name on bytes that never reached the disk.
	if err := w.file.Sync(); err != nil {
		return storageapi.WriteResult{}, err
	}

	// Check the error of Close. Some filesystems report a full disk only
	// there.
	if err := w.file.Close(); err != nil {
		return storageapi.WriteResult{}, err
	}

	if err := w.root.Rename(w.temp, w.destination); err != nil {
		return storageapi.WriteResult{}, pathError(err)
	}

	// The object is published. Nothing after this point may fail the write.
	w.finished = true

	sum := storageapi.Sum(w.digest)

	writeSidecar(w.root, w.destination, sum)

	_ = w.root.Close()

	return storageapi.WriteResult{Path: w.published, Size: w.size, SHA256: sum}, nil
}

// Abort discards the write. It does nothing after Commit, so a caller can
// defer it. It ignores the context, so a cancelled caller still removes the
// temporary file.
func (w *writer) Abort(_ context.Context) error {
	if w.finished {
		return nil
	}

	w.finished = true

	// The file may already be closed, when Commit failed after its Close.
	_ = w.file.Close()

	err := w.root.Remove(w.temp)

	_ = w.root.Close()

	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return nil
}
