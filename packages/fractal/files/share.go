package files

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// sharesFile is the name of the file in the module's data directory that
// holds the shared folders.
const sharesFile = "files-shares.json"

// sharesVersion is the version of the layout of sharesFile.
const sharesVersion = 1

// The errors Share and Unshare return.
var (
	// ErrRootShare means the path names the root folder of a storage, which
	// cannot be shared.
	ErrRootShare = errors.New("the root folder of a storage cannot be shared")
	// ErrBadShare means the storage name is missing or too long, or the path
	// is too long.
	ErrBadShare = errors.New("the storage or the path cannot name a folder")
)

// Share is one shared folder: a storage of this server and a path in it,
// relative to its root and with no leading slash.
type Share struct {
	Storage string `json:"storage"`
	Path    string `json:"path"`
}

// sharesDocument is the content of sharesFile. Storages is every storage a
// share ever named, so its content store stays after its last unshare.
type sharesDocument struct {
	Version  int      `json:"version"`
	Shares   []Share  `json:"shares"`
	Storages []string `json:"storages"`
}

// Shares is the set of shared folders, kept in the module's data directory.
// It is safe for concurrent use.
type Shares struct {
	file    string
	changed chan struct{}

	mu   sync.Mutex
	held sharesDocument
}

// OpenShares reads the shared folders from dataDir. A directory that holds
// none yet gives an empty set.
func OpenShares(dataDir string) (*Shares, error) {
	if dataDir == "" {
		return nil, errors.New("the shared folders need a data directory")
	}

	s := &Shares{
		file:    filepath.Join(dataDir, sharesFile),
		changed: make(chan struct{}, 1),
		held:    sharesDocument{Version: sharesVersion},
	}

	data, err := os.ReadFile(s.file)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read the shared folders: %w", err)
	}

	if err = json.Unmarshal(data, &s.held); err != nil {
		return nil, fmt.Errorf("read the shared folders in %s: %w", s.file, err)
	}

	if s.held.Version != sharesVersion {
		return nil, fmt.Errorf("the shared folders in %s have version %d, and this module reads %d",
			s.file, s.held.Version, sharesVersion)
	}

	return s, nil
}

// Share shares one folder and everything in it. Sharing a folder that is
// already shared changes nothing.
func (s *Shares) Share(storage, folder string) error {
	share, err := shareOf(storage, folder)
	if err != nil {
		return err
	}

	return s.change(func(held *sharesDocument) bool {
		if slices.Contains(held.Shares, share) {
			return false
		}

		held.Shares = append(held.Shares, share)
		if !slices.Contains(held.Storages, storage) {
			held.Storages = append(held.Storages, storage)
		}

		return true
	})
}

// Unshare stops sharing one folder. A folder inside it that was shared on its
// own stays shared. Unsharing a folder that is not shared changes nothing.
func (s *Shares) Unshare(storage, folder string) error {
	share, err := shareOf(storage, folder)
	if err != nil {
		return err
	}

	return s.change(func(held *sharesDocument) bool {
		at := slices.Index(held.Shares, share)
		if at < 0 {
			return false
		}

		held.Shares = slices.Delete(held.Shares, at, at+1)

		return true
	})
}

// List returns the shared folders, sorted by storage and path.
func (s *Shares) List() []Share {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := slices.Clone(s.held.Shares)
	slices.SortFunc(out, func(a, b Share) int {
		if c := strings.Compare(a.Storage, b.Storage); c != 0 {
			return c
		}

		return strings.Compare(a.Path, b.Path)
	})

	return out
}

// Storages returns every storage a share ever named, sorted.
func (s *Shares) Storages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := slices.Clone(s.held.Storages)
	slices.Sort(out)

	return out
}

// Changed receives a value after a change to the shared folders. One value
// stands for any number of changes since it was last read.
func (s *Shares) Changed() <-chan struct{} { return s.changed }

// change applies edit to a copy of the set, writes the copy and keeps it. The
// set in memory stays as it was when the write fails.
func (s *Shares) change(edit func(held *sharesDocument) bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := sharesDocument{
		Version:  sharesVersion,
		Shares:   slices.Clone(s.held.Shares),
		Storages: slices.Clone(s.held.Storages),
	}
	if !edit(&next) {
		return nil
	}

	if err := writeAtomically(s.file, next); err != nil {
		return err
	}

	s.held = next

	select {
	case s.changed <- struct{}{}:
	default:
	}

	return nil
}

// plainText reports whether s is valid UTF-8 without a control character, so
// the file keeps it unchanged.
func plainText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}

// shareOf checks a storage name and a folder and returns the share in its
// canonical form.
func shareOf(storage, folder string) (Share, error) {
	clean := storageapi.CleanRelPath(folder)

	switch {
	case storage == "" || len(storage) > storageapi.MaxStorageBytes || strings.Contains(storage, "/") ||
		!plainText(storage) || !plainText(folder):
		return Share{}, fmt.Errorf("%w: the storage name %q", ErrBadShare, storage)
	case len(clean) > storageapi.MaxPathBytes:
		return Share{}, fmt.Errorf("%w: the path is %d bytes", ErrBadShare, len(clean))
	case clean == "":
		return Share{}, ErrRootShare
	}

	return Share{Storage: storage, Path: clean}, nil
}
