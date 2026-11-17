package upgrade

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrStagedLeft says that a cancel left the staged release on disk, in part
// or whole.
var ErrStagedLeft = errors.New("upgrade: the staged release is left on disk")

// CancelScheduled removes a transaction that waits for its schedule from the
// upgrade folder root, like Cancel. A server that runs no scheduler calls it
// at start, so the transaction does not run the moment a scheduler runs
// again. It returns the transaction it removed, and false when none waits. A
// transaction whose journal it read and could not remove is returned with
// false and the error.
func CancelScheduled(root string) (Journal, bool, error) {
	store := NewStore(filepath.Join(root, "journal.json"))
	journal, err := store.Load()
	if errors.Is(err, ErrNoJournal) {
		return Journal{}, false, nil
	}
	if err != nil {
		return Journal{}, false, err
	}
	if journal.State != StateScheduled {
		return Journal{}, false, nil
	}

	removed, err := cancelTransaction(root, store, journal)

	return journal, removed, err
}

// cancelTransaction removes the staged release of journal and then journal
// from store, and reports whether the journal is gone. Both cancels, the one
// on the Updates tab and the one at start, remove through it.
//
// The staged release is removed first and the journal second, so a stop
// between the two leaves a transaction the next cancel removes again. The
// release is removed only inside root/staged, where the service stages every
// release, and nothing outside root is removed through a link. A journal that
// names any other folder still loses its transaction, and the error wraps
// ErrStagedLeft and names the folder left on disk.
func cancelTransaction(root string, store Store, journal Journal) (bool, error) {
	left := removeStaged(root, journal)
	if left != nil {
		left = fmt.Errorf("%w: %w", ErrStagedLeft, left)
	}
	if err := store.Remove(journal); err != nil {
		return false, errors.Join(err, left)
	}

	return true, left
}

// removeStaged removes the staging folder of journal when it lies below
// root/staged. The staged folder must be a folder and not a link, and the
// release is removed through an os.Root on it, which refuses a link that
// leads out of it, to the backups beside it as well.
func removeStaged(root string, journal Journal) error {
	staged := stagingRoot(journal)
	parent, parentErr := filepath.Abs(filepath.Join(root, "staged"))
	resolved, stagedErr := filepath.Abs(staged)
	if parentErr != nil || stagedErr != nil || resolved == parent || !within(parent, resolved) {
		return fmt.Errorf("%w: transaction %s names the staging folder %q outside %s, which is left as it is",
			ErrJournal, journal.ID, staged, parent)
	}
	relative, err := filepath.Rel(parent, resolved)
	if err != nil {
		return fmt.Errorf("upgrade: resolve the staged release: %w", err)
	}

	folder, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("upgrade: open the upgrade folder: %w", err)
	}
	defer folder.Close()

	info, err := folder.Lstat("staged")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("upgrade: inspect the staging folder: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: the staging folder %s is a link or a file, not a folder, so it is left as it is",
			ErrJournal, parent)
	}
	staging, err := folder.OpenRoot("staged")
	if err != nil {
		return fmt.Errorf("upgrade: open the staging folder: %w", err)
	}
	defer staging.Close()
	// The folder opened is the one inspected, not a link put in its place
	// between the two calls.
	if opened, statErr := staging.Stat("."); statErr != nil || !os.SameFile(info, opened) {
		return fmt.Errorf("%w: the staging folder %s changed while it was opened, so it is left as it is",
			ErrJournal, parent)
	}

	if err = staging.RemoveAll(relative); err != nil {
		return fmt.Errorf("upgrade: remove the staged release %s: %w", resolved, err)
	}

	return nil
}
