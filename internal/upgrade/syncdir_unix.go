//go:build !windows

package upgrade

import (
	"fmt"
	"os"
)

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory %s for sync: %w", path, err)
	}

	defer func() { _ = directory.Close() }()

	if err = directory.Sync(); err != nil {
		return fmt.Errorf("sync directory %s: %w", path, err)
	}

	return nil
}
