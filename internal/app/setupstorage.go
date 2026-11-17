package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// probePattern names the file a write probe creates and removes at once.
const probePattern = ".prusa-connect-write-probe-*"

// The reasons a folder cannot hold the uploads, as the page reads them.
const (
	storageNotAFolder  = "not_a_folder"
	storageNotWritable = "not_writable"
	storageNoParent    = "no_parent"
)

// CheckStorage reports whether this process could keep uploads in root,
// behind the setup code. It creates nothing: a folder that is missing is
// judged by the nearest folder above it that exists, which is where the
// confirm would create it.
func (f *firstRun) CheckStorage(
	ctx context.Context, client httpapi.SetupClient, code, root string,
) (httpapi.SetupStorageCheck, error) {
	if !f.Pending() {
		return httpapi.SetupStorageCheck{}, fault.AuthSetupClosed.New(
			"this server has already recorded what it does about identity")
	}

	if err := f.code.admit(ctx, client, code); err != nil {
		return httpapi.SetupStorageCheck{}, err
	}

	return checkStorage(root), nil
}

// checkStorage is CheckStorage without the gate, for the confirm and a test.
func checkStorage(root string) httpapi.SetupStorageCheck {
	absolute, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return httpapi.SetupStorageCheck{Root: root, Problem: storageNoParent, Detail: err.Error()}
	}

	check := httpapi.SetupStorageCheck{Root: absolute}

	info, err := os.Stat(absolute)

	switch {
	case err == nil && !info.IsDir():
		check.Problem, check.Detail = storageNotAFolder, absolute+" is a file"

		return check
	case err == nil:
		check.Exists = true
	case !errors.Is(err, fs.ErrNotExist):
		check.Problem, check.Detail = storageNotWritable, err.Error()

		return check
	}

	// A folder that does not exist yet is created inside the nearest one that
	// does, so that is the folder the write is tried in.
	probeIn := absolute
	if !check.Exists {
		parent, found := existingParent(absolute)
		if !found {
			check.Problem, check.Detail = storageNoParent, "no folder above "+absolute+" exists"

			return check
		}

		probeIn = parent
	}

	if err = probeWrite(probeIn); err != nil {
		check.Problem, check.Detail = storageNotWritable, err.Error()

		return check
	}

	check.Writable = true

	return check
}

// existingParent is the nearest folder above path that exists.
func existingParent(path string) (string, bool) {
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if info, err := os.Stat(dir); err == nil {
			return dir, info.IsDir()
		}

		if dir == filepath.Dir(dir) {
			return "", false
		}
	}
}

// probeWrite creates a file in dir and removes it, which is the one way to
// learn whether this process may write there: the mode bits do not say what a
// read-only mount, an ACL or a sandbox allows.
func probeWrite(dir string) error {
	probe, err := os.CreateTemp(dir, probePattern)
	if err != nil {
		return fmt.Errorf("write in %s: %w", dir, err)
	}

	name := probe.Name()

	closeErr := probe.Close()
	removeErr := os.Remove(name)

	return errors.Join(closeErr, removeErr)
}

// storageRootOf is the storage folder as an absolute path. It creates the
// folder and writes to it, so a folder the server cannot write is refused
// while the person can still pick another one.
func storageRootOf(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", nil
	}

	check := checkStorage(root)
	if !check.Writable {
		return "", httpfault.RequestInvalid("body.storage.root", "a folder this server can write", check.Detail)
	}

	if err := os.MkdirAll(check.Root, storageFolderMode); err != nil {
		return "", httpfault.RequestInvalid("body.storage.root", "a folder this server can write", err.Error())
	}

	if err := probeWrite(check.Root); err != nil {
		return "", httpfault.RequestInvalid("body.storage.root", "a folder this server can write", err.Error())
	}

	return check.Root, nil
}
