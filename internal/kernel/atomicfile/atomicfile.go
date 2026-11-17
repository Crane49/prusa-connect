// Package atomicfile publishes a file in five steps, so that a crash leaves
// either the previous bytes or the new ones and never a mixture:
//
//  1. create a temporary file in the destination's own directory
//  2. write the data and fsync it
//  3. close the file and check the error from Close
//  4. rename it onto the destination
//  5. fsync the directory that holds it
//
// The write is split into Stage and Commit because the caller that needs it
// most has a check to make in between: a compare-and-swap over a file re-reads
// the inputs immediately before the rename, and the bytes have to be on disk
// already for that window to be as short as it can be.
package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrCommitted reports a second Commit against one staged write.
var ErrCommitted = errors.New("atomicfile: the staged file is already committed")

// tempPattern suffixes the destination's own name, so a temporary left by a
// crash is recognisable as one and sorts beside the file it belongs to.
const tempPattern = ".tmp*"

// Staged is data already written, flushed and closed in the destination's
// directory, waiting for the rename that publishes it.
type Staged struct {
	dest string
	temp string
}

// Write stages data at path and commits it, for a caller with nothing to check
// in between.
func Write(path string, data []byte, perm os.FileMode) error {
	staged, err := Stage(path, data, perm)
	if err != nil {
		return err
	}

	return staged.Commit()
}

// Stage is steps 1 to 3: a temporary file in the destination's own directory,
// because a rename across filesystems fails and a temporary under the system
// temp directory is a different mount in most containers; the data written and
// fsynced; and the file closed with the error from Close checked.
//
// A failed Stage leaves nothing behind.
func Stage(dest string, data []byte, perm os.FileMode) (*Staged, error) {
	dir := filepath.Dir(dest)

	file, err := os.CreateTemp(dir, filepath.Base(dest)+tempPattern)
	if err != nil {
		return nil, fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}

	staged := &Staged{dest: dest, temp: file.Name()}

	if err = writeAndClose(file, data, perm); err != nil {
		_ = staged.Discard()

		return nil, err
	}

	return staged, nil
}

// Path returns the temporary file the data is sitting in.
func (s *Staged) Path() string { return s.temp }

// Commit is steps 4 and 5: the rename, which is atomic, and then an fsync of
// the directory holding it, which makes it durable. fsync(2) says that syncing
// a file does not sync the directory entry naming it.
//
// A failed rename discards the staged file. The destination still holds the
// previous bytes, and temporaries left behind would pile up.
func (s *Staged) Commit() error {
	if s.temp == "" {
		return ErrCommitted
	}

	if err := os.Rename(s.temp, s.dest); err != nil {
		_ = s.Discard()

		return fmt.Errorf("rename %s to %s: %w", s.temp, s.dest, err)
	}

	s.temp = ""

	return syncDir(filepath.Dir(s.dest))
}

// Discard removes the staged file. It is what a caller abandoning a write
// calls; a failed Stage or Commit has already done it.
func (s *Staged) Discard() error {
	if s.temp == "" {
		return nil
	}

	temp := s.temp
	s.temp = ""

	if err := os.Remove(temp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", temp, err)
	}

	return nil
}

// writeAndClose writes, fsyncs and closes, and reports the error from Close.
// On FAT and on USB media, out of space is reported only by close, so
// discarding that error loses data.
func writeAndClose(file *os.File, data []byte, perm os.FileMode) error {
	closed := false

	defer func() {
		if !closed {
			// The failure that got us here is the one worth reporting; this
			// close only releases the descriptor before the file is removed.
			_ = file.Close()
		}
	}()

	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", file.Name(), err)
	}

	// CreateTemp opens at 0600. Widening it before the rename means the file
	// never appears at its destination with the wrong mode.
	if err := file.Chmod(perm); err != nil && !errors.Is(err, os.ErrInvalid) {
		return fmt.Errorf("chmod %s: %w", file.Name(), err)
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("fsync %s: %w", file.Name(), err)
	}

	closed = true

	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", file.Name(), err)
	}

	return nil
}
