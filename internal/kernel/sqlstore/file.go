package sqlstore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// FileMode is the mode of a SQLite file this server keeps: the process that
// runs the server alone reads and writes it. SQLite gives the write-ahead log
// and its index the mode of the database file when it creates them.
const FileMode = 0o600

// OwnFile makes the SQLite file at path its owner's alone, creating it empty
// when it is not there yet, so it never exists with a mode other users may
// read. The driver would otherwise create it through the process umask, which
// is 0644 on a usual system, and the directory mode protects it only when this
// server created the directory.
//
// A log or index an earlier process left beside the file is narrowed too,
// because SQLite reuses a file it finds with the mode the file already has.
func OwnFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, FileMode)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}

	if err = file.Close(); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}

	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		if err = os.Chmod(name, FileMode); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("make %s its owner's alone: %w", name, err)
		}
	}

	return nil
}
