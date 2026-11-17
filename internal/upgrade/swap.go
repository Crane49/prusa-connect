package upgrade

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// nextSuffix names the candidate copy placed beside the current binary. A
// rename only replaces a file on the same filesystem, and the private staging
// directory may be on another one, so the swap renames this copy instead.
const nextSuffix = ".next"

// placeBeside links or copies the verified candidate next to the current
// binary and checks the copy against the signed identity before the swap.
func placeBeside(journal Journal, syncParent func(string) error) (string, error) {
	if journal.Verified == nil {
		return "", errors.New("upgrade: staged binary has no verified identity")
	}
	next := journal.CurrentBinary + nextSuffix
	if err := os.Remove(next); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("upgrade: remove stale candidate copy: %w", err)
	}
	if err := os.Link(journal.StagedBinary, next); err != nil {
		if err = copyExecutable(journal.StagedBinary, next); err != nil {
			_ = os.Remove(next)

			return "", fmt.Errorf("upgrade: place candidate beside the current binary: %w", err)
		}
	}
	if err := verifyFileIdentity(next, journal.Verified.Size, journal.Verified.SHA256, "core binary"); err != nil {
		_ = os.Remove(next)

		return "", err
	}
	if err := syncParent(filepath.Dir(next)); err != nil {
		return "", fmt.Errorf("upgrade: sync candidate copy: %w", err)
	}

	return next, nil
}

// platformSwap keeps a durable copy of current as previous, then replaces
// current with next in one step, so the service path always names a binary.
func platformSwap(current, next, previous string, syncParent func(string) error) error {
	if err := os.Remove(previous); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("upgrade: remove previous binary: %w", err)
	}
	if err := os.Link(current, previous); err != nil {
		if err = copyExecutable(current, previous); err != nil {
			return fmt.Errorf("upgrade: preserve current binary: %w", err)
		}
	}
	if err := syncParent(filepath.Dir(current)); err != nil {
		return fmt.Errorf("upgrade: sync preserved binary: %w", err)
	}
	if err := replaceExecutable(next, current); err != nil {
		return fmt.Errorf("upgrade: publish staged binary: %w", err)
	}
	if err := syncParent(filepath.Dir(current)); err != nil {
		return fmt.Errorf("upgrade: sync binary directory: %w", err)
	}

	return nil
}

func copyExecutable(source, destination string) error {
	from, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = from.Close() }()

	info, err := from.Stat()
	if err != nil {
		return err
	}
	to, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(to, from)
	syncErr := to.Sync()
	closeErr := to.Close()

	return errors.Join(copyErr, syncErr, closeErr)
}
