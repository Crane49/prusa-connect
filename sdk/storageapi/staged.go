package storageapi

import (
	"fmt"
	"os"
	"path/filepath"
)

// RemoveStaged deletes the object at a LocalWriter.Name, for a caller that no
// longer has the provider. Prefer a delete through the provider: this call
// does not confine the directories above the file to a store.
//
// It reports ErrInvalidPath for a name that is not absolute or names no
// object, and the filesystem's not-exist error for a name that is gone.
func RemoveStaged(name string) error {
	if !filepath.IsAbs(name) {
		return fmt.Errorf("%w: %q is not an absolute path", ErrInvalidPath, name)
	}

	// After Clean, only a volume or a filesystem root has no last element.
	dir, leaf := filepath.Split(filepath.Clean(name))
	if leaf == "" {
		return fmt.Errorf("%w: %q names no object to remove", ErrInvalidPath, name)
	}

	root, err := os.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	return root.Remove(leaf)
}
