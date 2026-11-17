//go:build !windows

package atomicfile

import (
	"fmt"
	"os"
)

// syncDir is step 5. Without it a power cut after the rename can leave the old
// directory entry: fsync(2) says "Calling fsync() does not necessarily ensure
// that the entry in the directory containing the file has also reached disk.
// For that an explicit fsync() on a file descriptor for the directory is also
// needed."
func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}

	defer func() {
		// The descriptor is read-only, so its close reports nothing the sync
		// below has not already reported.
		_ = handle.Close()
	}()

	if err = handle.Sync(); err != nil {
		return fmt.Errorf("fsync %s: %w", dir, err)
	}

	return nil
}
