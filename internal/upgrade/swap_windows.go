//go:build windows

package upgrade

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const replaceFlags = windows.MOVEFILE_REPLACE_EXISTING | windows.MOVEFILE_WRITE_THROUGH

// replaceExecutable moves source over destination in one MoveFileEx call
// that reaches the disk before it returns. Windows refuses to replace the
// image of a running process, but lets it be renamed. In that case the
// running image is moved aside first, and destination is empty between the
// two calls. platformSwap first makes a full copy at .previous, and
// restorePrevious puts that copy back when it finds destination missing.
func replaceExecutable(source, destination string) error {
	removeAsideCopies(destination)

	err := move(source, destination, replaceFlags)
	if err == nil || !inUse(err) {
		return err
	}

	aside := destination + ".old"
	for index := 1; ; index++ {
		if _, statErr := os.Lstat(aside); errors.Is(statErr, os.ErrNotExist) {
			break
		}
		aside = fmt.Sprintf("%s.old%d", destination, index)
	}
	if err = move(destination, aside, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return fmt.Errorf("move the running binary aside: %w", err)
	}
	if err = move(source, destination, replaceFlags); err != nil {
		return errors.Join(err, move(aside, destination, windows.MOVEFILE_WRITE_THROUGH))
	}

	return nil
}

func move(source, destination string, flags uint32) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	if err = windows.MoveFileEx(from, to, flags); err != nil {
		return &os.LinkError{Op: "movefileex", Old: source, New: destination, Err: err}
	}

	return nil
}

func inUse(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

// removeAsideCopies deletes binaries moved aside by an earlier swap. One that
// still runs cannot be deleted and stays for a later swap.
func removeAsideCopies(destination string) {
	matches, err := filepath.Glob(destination + ".old*")
	if err != nil {
		return
	}
	for _, match := range matches {
		_ = os.Remove(match)
	}
}
