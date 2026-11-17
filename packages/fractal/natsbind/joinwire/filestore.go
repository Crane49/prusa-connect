package joinwire

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

// The format and the permissions of the FileStore file.
const (
	// storeFormat is the version of the file format. FileStore refuses a file
	// with another version.
	storeFormat = 1
	// fileMode lets only the owner read the file, because it lists Node IDs
	// and public keys.
	fileMode = 0o600
	// dirMode is the permission of the directory FileStore creates for the
	// file.
	dirMode = 0o700
)

// ErrCorruptStore means FileStore cannot read or parse its file. It returns
// the error and never an empty list, because with an empty list AskFirst
// would lose the approvals an operator gave.
var ErrCorruptStore = errors.New("the ask-first store file cannot be read")

// storeFile is the JSON content of the file.
type storeFile struct {
	Format  int      `json:"format"`
	Records []Record `json:"records"`
}

// FileStore saves the records of AskFirst in one JSON file. Save writes a
// temporary file in the same directory, syncs it and renames it over the
// file. After a crash during Save the file has its earlier content.
type FileStore struct {
	path string

	mu     sync.Mutex
	byID   map[fractal.NodeID]Record
	loaded bool
}

// NewFileStore returns a FileStore for the file at path. The first Load or
// Save reads the file, and the first Save creates it.
func NewFileStore(path string) *FileStore {
	return &FileStore{path: filepath.Clean(path)}
}

// Load returns every record, sorted by Node ID.
func (s *FileStore) Load(context.Context) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.read(); err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(s.byID))
	for _, r := range s.byID {
		out = append(out, r)
	}
	slices.SortFunc(out, func(x, y Record) int { return compareID(x.NodeID, y.NodeID) })
	return out, nil
}

// Save replaces the record with the same Node ID and writes the whole file
// again.
func (s *FileStore) Save(_ context.Context, r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.read(); err != nil {
		return err
	}
	previous, held := s.byID[r.NodeID]
	s.byID[r.NodeID] = r
	if err := s.writeAll(); err != nil {
		// The file did not change, so the record in memory goes back to its
		// earlier value.
		if held {
			s.byID[r.NodeID] = previous
		} else {
			delete(s.byID, r.NodeID)
		}
		return err
	}
	return nil
}

// Delete removes the record of one Node ID and writes the whole file again. A
// Node ID with no record changes nothing and returns nil.
func (s *FileStore) Delete(_ context.Context, id fractal.NodeID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.read(); err != nil {
		return err
	}
	previous, held := s.byID[id]
	if !held {
		return nil
	}
	delete(s.byID, id)
	if err := s.writeAll(); err != nil {
		// The file did not change, so the record goes back into memory.
		s.byID[id] = previous
		return err
	}
	return nil
}

// writeAll writes every record in memory to the file. The caller holds s.mu.
func (s *FileStore) writeAll() error {
	records := make([]Record, 0, len(s.byID))
	for _, record := range s.byID {
		records = append(records, record)
	}
	slices.SortFunc(records, func(x, y Record) int { return compareID(x.NodeID, y.NodeID) })
	data, err := json.Marshal(storeFile{Format: storeFormat, Records: records})
	if err != nil {
		return fmt.Errorf("encode the waiting identities: %w", err)
	}
	if writeErr := writeWholeFile(s.path, data); writeErr != nil {
		return fmt.Errorf("write the waiting identities: %w", writeErr)
	}
	return nil
}

// read loads the file into memory on its first call and does nothing after
// that. A file that does not exist is an empty list. The caller holds s.mu.
func (s *FileStore) read() error {
	if s.loaded {
		return nil
	}
	data, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		data = nil
	case err != nil:
		return fmt.Errorf("%w: %s: %w", ErrCorruptStore, s.path, err)
	}
	s.byID = map[fractal.NodeID]Record{}
	if data != nil {
		var file storeFile
		if decodeErr := json.Unmarshal(data, &file); decodeErr != nil {
			return fmt.Errorf("%w: %s: %w", ErrCorruptStore, s.path, decodeErr)
		}
		if file.Format != storeFormat {
			return fmt.Errorf("%w: %s: format %d", ErrCorruptStore, s.path, file.Format)
		}
		for _, r := range file.Records {
			if idErr := r.NodeID.Validate(); idErr != nil {
				return fmt.Errorf("%w: %s: %w", ErrCorruptStore, s.path, idErr)
			}
			s.byID[r.NodeID] = r
		}
	}
	s.loaded = true
	return nil
}

// writeWholeFile writes data to a temporary file in the directory of path,
// syncs it, closes it and renames it to path. After a crash before the rename
// the file at path has its earlier content. After a crash later it has the new
// content. No reader sees a file that is written in part.
func writeWholeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("make %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".fractal-askfirst-*.tmp")
	if err != nil {
		return fmt.Errorf("open a temporary file in %s: %w", dir, err)
	}
	name := tmp.Name()
	if writeErr := writeAndClose(tmp, data); writeErr != nil {
		_ = os.Remove(name) // The write error is the one returned.
		return writeErr
	}
	if modeErr := os.Chmod(name, fileMode); modeErr != nil {
		_ = os.Remove(name) // The chmod error is the one returned.
		return fmt.Errorf("set the permission of %s: %w", name, modeErr)
	}
	if renameErr := os.Rename(name, path); renameErr != nil {
		_ = os.Remove(name) // The rename error is the one returned.
		return fmt.Errorf("rename %s to %s: %w", name, path, renameErr)
	}
	return nil
}

// writeAndClose writes data to f, syncs f to disk and closes it. It returns the
// error of Close too, because the operating system can report a failed write
// only then.
func writeAndClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		_ = f.Close() // The write error is the one returned.
		return fmt.Errorf("write %s: %w", f.Name(), err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close() // The sync error is the one returned.
		return fmt.Errorf("sync %s: %w", f.Name(), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", f.Name(), err)
	}
	return nil
}

// MemoryStore keeps the records of AskFirst in memory. Its zero value is ready
// to use.
type MemoryStore struct {
	mu   sync.Mutex
	byID map[fractal.NodeID]Record
	// SaveErr, when set, is the error every Save returns. A test sets it to
	// check what AskFirst does when the store fails.
	SaveErr error
	// LoadErr, when set, is the error every Load returns.
	LoadErr error
	// DeleteErr, when set, is the error every Delete returns.
	DeleteErr error
}

// Load returns every record, sorted by Node ID.
func (s *MemoryStore) Load(context.Context) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.LoadErr != nil {
		return nil, s.LoadErr
	}
	out := make([]Record, 0, len(s.byID))
	for _, r := range s.byID {
		out = append(out, r)
	}
	slices.SortFunc(out, func(x, y Record) int { return compareID(x.NodeID, y.NodeID) })
	return out, nil
}

// Save replaces the record with the same Node ID.
func (s *MemoryStore) Save(_ context.Context, r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.SaveErr != nil {
		return s.SaveErr
	}
	if s.byID == nil {
		s.byID = map[fractal.NodeID]Record{}
	}
	s.byID[r.NodeID] = r
	return nil
}

// Delete removes the record of one Node ID.
func (s *MemoryStore) Delete(_ context.Context, id fractal.NodeID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.DeleteErr != nil {
		return s.DeleteErr
	}
	delete(s.byID, id)
	return nil
}
