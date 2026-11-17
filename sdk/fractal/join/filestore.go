package join

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// storeFormat is the "format" number of the store files. A store refuses a
// file with another number.
const storeFormat = 1

// storeMode is the permission of the store files. They contain connection
// credentials, so only the owner may read them.
const storeMode = 0o600

// ErrCorruptStore means FileAdmissionStore or FileMembershipStore found its
// file and cannot use it: the read failed, the JSON does not parse, the format
// number is unknown, or a Node ID or Node Path in it is not valid. A file that
// does not exist is not an error.
var ErrCorruptStore = errors.New("the store file cannot be read")

// admissionFile is the JSON layout of the admissions file.
type admissionFile struct {
	Format     int         `json:"format"`
	Admissions []Admission `json:"admissions"`
}

// membershipFile is the JSON layout of the membership file.
type membershipFile struct {
	Format     int        `json:"format"`
	Membership Membership `json:"membership"`
}

// FileAdmissionStore is an AdmissionStore that keeps every record in one JSON
// file. A save replaces the file atomically, so a crash during a save leaves
// the old file complete.
type FileAdmissionStore struct {
	path string

	mu     sync.Mutex
	byID   map[fractal.NodeID]Admission
	loaded bool
}

// NewFileAdmissionStore returns a store that uses the file at path. The store
// reads the file on the first call to LoadAdmissions or SaveAdmission. It
// creates the file, and the directory, on the first SaveAdmission.
func NewFileAdmissionStore(path string) *FileAdmissionStore {
	return &FileAdmissionStore{path: filepath.Clean(path)}
}

// LoadAdmissions returns every record, ordered by Node ID.
func (s *FileAdmissionStore) LoadAdmissions(context.Context) ([]Admission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	out := make([]Admission, 0, len(s.byID))
	for _, a := range s.byID {
		out = append(out, a)
	}
	slices.SortFunc(out, func(x, y Admission) int { return compareNodeID(x.NodeID, y.NodeID) })
	return out, nil
}

// SaveAdmission adds or replaces one record and writes the whole file again.
func (s *FileAdmissionStore) SaveAdmission(_ context.Context, a Admission) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	previous, held := s.byID[a.NodeID]
	s.byID[a.NodeID] = a
	records := make([]Admission, 0, len(s.byID))
	for _, r := range s.byID {
		records = append(records, r)
	}
	slices.SortFunc(records, func(x, y Admission) int { return compareNodeID(x.NodeID, y.NodeID) })
	data, err := json.Marshal(admissionFile{Format: storeFormat, Admissions: records})
	if err != nil {
		return fmt.Errorf("encode the admissions: %w", err)
	}
	if writeErr := writeWholeFile(s.path, data); writeErr != nil {
		// The file did not change, so the map in memory goes back to the
		// record it had before.
		if held {
			s.byID[a.NodeID] = previous
		} else {
			delete(s.byID, a.NodeID)
		}
		return fmt.Errorf("write the admissions: %w", writeErr)
	}
	return nil
}

// load reads the file into s.byID, once. The caller must hold the lock.
func (s *FileAdmissionStore) load() error {
	if s.loaded {
		return nil
	}
	data, err := readWholeFile(s.path)
	if err != nil {
		return err
	}
	s.byID = map[fractal.NodeID]Admission{}
	if data != nil {
		var file admissionFile
		if decodeErr := json.Unmarshal(data, &file); decodeErr != nil {
			return fmt.Errorf("%w: %s: %w", ErrCorruptStore, s.path, decodeErr)
		}
		if file.Format != storeFormat {
			return fmt.Errorf("%w: %s: format %d", ErrCorruptStore, s.path, file.Format)
		}
		for _, a := range file.Admissions {
			if idErr := a.NodeID.Validate(); idErr != nil {
				return fmt.Errorf("%w: %s: %w", ErrCorruptStore, s.path, idErr)
			}
			s.byID[a.NodeID] = a
		}
	}
	s.loaded = true
	return nil
}

// FileMembershipStore is a MembershipStore that keeps the Membership in one
// JSON file. A save replaces the file atomically.
type FileMembershipStore struct {
	path string
	mu   sync.Mutex
}

// NewFileMembershipStore returns a store that uses the file at path.
func NewFileMembershipStore(path string) *FileMembershipStore {
	return &FileMembershipStore{path: filepath.Clean(path)}
}

// LoadMembership reads the file. ok is false when the file does not exist.
func (s *FileMembershipStore) LoadMembership(context.Context) (Membership, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := readWholeFile(s.path)
	if err != nil {
		return Membership{}, false, err
	}
	if data == nil {
		return Membership{}, false, nil
	}
	var file membershipFile
	if decodeErr := json.Unmarshal(data, &file); decodeErr != nil {
		return Membership{}, false, fmt.Errorf("%w: %s: %w", ErrCorruptStore, s.path, decodeErr)
	}
	if file.Format != storeFormat {
		return Membership{}, false, fmt.Errorf("%w: %s: format %d", ErrCorruptStore, s.path, file.Format)
	}
	if pathErr := file.Membership.NodePath.Validate(); pathErr != nil {
		return Membership{}, false, fmt.Errorf("%w: %s: %w", ErrCorruptStore, s.path, pathErr)
	}
	return file.Membership, true, nil
}

// SaveMembership writes the Membership to the file and replaces the old one.
func (s *FileMembershipStore) SaveMembership(_ context.Context, m Membership) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.Marshal(membershipFile{Format: storeFormat, Membership: m})
	if err != nil {
		return fmt.Errorf("encode the membership: %w", err)
	}
	if writeErr := writeWholeFile(s.path, data); writeErr != nil {
		return fmt.Errorf("write the membership: %w", writeErr)
	}
	return nil
}

// readWholeFile reads the file at path. When the file does not exist it
// returns nil bytes and no error.
func readWholeFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("%w: %s: %w", ErrCorruptStore, path, err)
	}
	return data, nil
}

// writeWholeFile writes data to a temporary file in the directory of path,
// syncs it and renames it to path. A reader never gets a half-written file.
func writeWholeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("make %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".fractal-join-*.tmp")
	if err != nil {
		return fmt.Errorf("open a temporary file in %s: %w", dir, err)
	}
	name := tmp.Name()
	if writeErr := writeAndClose(tmp, data); writeErr != nil {
		_ = os.Remove(name) // The caller gets the write error, which matters more.
		return writeErr
	}
	if modeErr := os.Chmod(name, storeMode); modeErr != nil {
		_ = os.Remove(name) // A file with credentials must not stay with a wider permission.
		return fmt.Errorf("set the permission of %s: %w", name, modeErr)
	}
	if renameErr := os.Rename(name, path); renameErr != nil {
		_ = os.Remove(name) // No code reads the temporary file, so remove it.
		return fmt.Errorf("rename %s to %s: %w", name, path, renameErr)
	}
	return nil
}

// writeAndClose writes data to f, syncs f to the disk and closes it. Close can
// report a write error, so its error is checked.
func writeAndClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		_ = f.Close() // The caller gets the write error, which matters more.
		return fmt.Errorf("write %s: %w", f.Name(), err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close() // The caller gets the sync error, which matters more.
		return fmt.Errorf("sync %s: %w", f.Name(), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", f.Name(), err)
	}
	return nil
}
