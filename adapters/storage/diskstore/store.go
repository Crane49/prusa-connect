package diskstore

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

const (
	// dirMode and fileMode are the modes the store creates with. Other users
	// cannot read an operator's uploads.
	dirMode  = 0o750
	fileMode = 0o600
)

// errStoreMoved reports that the configured path no longer leads to the
// directory this provider was built on.
var errStoreMoved = errors.New("local: the storage root was replaced")

// store is the root path and the identity of the directory it resolved to.
// Every operation opens the path again and compares the identity, so a link
// or a directory swapped in at the store's own name is detected.
type store struct {
	root     string      // absolute, cleaned
	identity fs.FileInfo // what root resolved to when the provider was built
}

// openStore creates the configured directory and records its identity. Each
// path component is created through a handle on its parent, so a link planted
// along the path cannot move the store.
func openStore(configured string) (store, error) {
	if strings.TrimSpace(configured) == "" {
		return store{}, errors.New("local: spec.Path is required, it is the directory the store keeps its objects in")
	}

	abs, err := filepath.Abs(configured)
	if err != nil {
		return store{}, fmt.Errorf("local: resolve storage root: %w", err)
	}

	abs = filepath.Clean(abs)

	volume := filepath.VolumeName(abs)
	anchor := volume + string(filepath.Separator)

	root, err := os.OpenRoot(anchor)
	if err != nil {
		return store{}, fmt.Errorf("local: open %s: %w", anchor, err)
	}

	for part := range strings.SplitSeq(strings.TrimPrefix(abs, volume), string(filepath.Separator)) {
		if part == "" {
			continue
		}

		next, descendErr := descend(root, part)

		_ = root.Close()

		if descendErr != nil {
			return store{}, descendErr
		}

		root = next
	}

	defer func() { _ = root.Close() }()

	// Take the identity through the handle. On Windows a FileInfo from
	// os.Stat reads its file index later, by opening the name again.
	identity, err := root.Stat(".")
	if err != nil {
		return store{}, fmt.Errorf("local: identify storage root %s: %w", abs, err)
	}

	return store{root: abs, identity: identity}, nil
}

// descend creates one component under root when it is missing and returns a
// handle on it.
func descend(root *os.Root, name string) (*os.Root, error) {
	if err := root.Mkdir(name, dirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("local: create %s under %s: %w", name, root.Name(), err)
	}

	opened, err := root.OpenRoot(name)
	if err != nil {
		return nil, fmt.Errorf("local: open %s under %s: %w", name, root.Name(), err)
	}

	return opened, nil
}

// open returns the confinement handle for one operation. It fails with
// errStoreMoved when the path leads to a different directory.
func (s store) open() (*os.Root, error) {
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return nil, err
	}

	identity, err := root.Stat(".")
	if err == nil && os.SameFile(identity, s.identity) {
		return root, nil
	}

	_ = root.Close()

	if err != nil {
		return nil, err
	}

	return nil, errStoreMoved
}

// name turns an untrusted relative path into the name given to os.Root.
// CleanRelPath removes "../" segments. os.Root refuses a link inside the
// store that points out of it.
func name(rel string) string {
	clean := storageapi.CleanRelPath(rel)
	if clean == "" {
		return "." // os.Root spells the store directory itself "."
	}

	return filepath.FromSlash(clean)
}

// errNoEntry is returned both for a name that does not exist and for a name
// that resolves outside the store. One error for both means a caller cannot
// learn which names point out of the store. It wraps ErrInvalidPath and
// fs.ErrNotExist, so a host can answer "not found".
var errNoEntry = fmt.Errorf("%w: %w", storageapi.ErrInvalidPath, fs.ErrNotExist)

// pathError turns a failed resolution into errNoEntry. An escape refused by
// os.Root has no errno. An error with an errno is a filesystem fault and is
// returned unchanged.
func pathError(err error) error {
	if err == nil {
		return nil
	}

	var errno syscall.Errno
	if errors.Is(err, fs.ErrNotExist) || !errors.As(err, &errno) {
		return errNoEntry
	}

	return err
}

// dirError turns a parent directory that cannot be created into
// ErrInvalidPath. The cause is a file or a refused link at one of the path
// components, which is the caller's path and not a server fault.
func dirError(err error) error {
	if errors.Is(err, fs.ErrExist) || errors.Is(err, syscall.ENOTDIR) {
		return storageapi.ErrInvalidPath
	}

	return pathError(err)
}

// createTemp opens an exclusive temporary file beside base, through root.
// os.CreateTemp is not used, because it resolves the directory outside the
// confinement. The name includes the destination's base name and 128 random
// bits.
func createTemp(root *os.Root, dir, base string) (string, *os.File, error) {
	temp := filepath.Join(dir, "."+base+".part-"+rand.Text())

	f, err := root.OpenFile(temp, os.O_RDWR|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return "", nil, err
	}

	return temp, f, nil
}
