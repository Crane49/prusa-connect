package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/prusa3d/server-slicing/internal/catalog"
)

// FromLocal is the source of a binary the operator named in local_binaries.
const FromLocal catalog.Source = "local"

// Slicer is one PrusaSlicer this module can run.
type Slicer struct {
	Version catalog.Version `json:"-"`
	Channel catalog.Channel `json:"channel"`
	Source  catalog.Source  `json:"source"`
	// Binary is the absolute path of the executable. It never reaches a
	// client, because it names this server's own directories.
	Binary string   `json:"-"`
	Args   []string `json:"-"`
	// SHA256 is the digest of the download it was unpacked from, and empty
	// for a local binary. Verified says it matched a digest its source
	// published; an administrator confirmed an install without one.
	SHA256      string    `json:"sha256,omitempty"`
	Verified    bool      `json:"verified"`
	InstalledAt time.Time `json:"installed_at,omitzero"`
	// Released and Notes are what the manifest said of the release.
	Released string `json:"released,omitempty"`
	Notes    string `json:"notes,omitempty"`
	// Bytes is the room the build takes on disk.
	Bytes int64 `json:"bytes,omitempty"`
}

// Name is the version, which is how a job and a person name a slicer.
func (s Slicer) Name() string { return s.Version.String() }

// MarshalJSON writes the version as text beside the rest.
func (s Slicer) MarshalJSON() ([]byte, error) {
	type plain Slicer

	return json.Marshal(struct {
		plain

		Version string `json:"version"`
	}{plain: plain(s), Version: s.Version.String()})
}

// ErrNotInstalled reports a version that is not installed.
var ErrNotInstalled = errors.New("that version is not installed")

// ErrNotOffered reports a version no manifest offers for this host.
var ErrNotOffered = errors.New("no manifest offers that version for this host")

// ErrBusy reports a version that is being installed already.
var ErrBusy = errors.New("that version is being installed already")

// Phase is where an install is.
type Phase string

// The phases of an install.
const (
	Downloading Phase = "downloading"
	Unpacking   Phase = "unpacking"
	Checking    Phase = "checking"
	Failed      Phase = "failed"
)

// Progress is one install that is running or failed.
type Progress struct {
	Version string `json:"version"`
	Phase   Phase  `json:"phase"`
	Done    int64  `json:"done_bytes"`
	Total   int64  `json:"total_bytes,omitempty"`
	Error   string `json:"error,omitempty"`
}

// record is one installed build as the index on disk holds it. The binary is
// relative to the store's directory, so the data directory may move.
type record struct {
	Version     string          `json:"version"`
	Channel     catalog.Channel `json:"channel"`
	Source      catalog.Source  `json:"source"`
	Binary      string          `json:"binary"`
	Args        []string        `json:"args,omitempty"`
	SHA256      string          `json:"sha256"`
	Verified    bool            `json:"verified"`
	InstalledAt time.Time       `json:"installed_at"`
	Released    string          `json:"released,omitempty"`
	Notes       string          `json:"notes,omitempty"`
	Bytes       int64           `json:"bytes,omitempty"`
}

// indexName is the file that lists what is installed.
const indexName = "installed.json"

// Store is the builds installed under one directory, and the downloads of
// them in a cache beside it.
type Store struct {
	dir    string
	cache  string
	client *http.Client

	mu        sync.Mutex
	installed []Slicer
	running   map[string]*Progress
	// inUse counts the jobs running on each version, so a version is not
	// removed under a job.
	inUse map[string]int
}

// Open reads the index under dir. A missing index is an empty store. cache is
// where downloads are kept; it may be shared with another store.
func Open(dir, cache string, client *http.Client) (*Store, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("make the slicer directory: %w", err)
	}

	store := &Store{
		dir: dir, cache: cache, client: client,
		running: map[string]*Progress{}, inUse: map[string]int{},
	}

	data, err := os.ReadFile(filepath.Join(dir, indexName))
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read the slicer index: %w", err)
	}

	var records []record
	if err = json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("read the slicer index: %w", err)
	}

	for _, held := range records {
		version, parseErr := catalog.ParseVersion(held.Version)
		if parseErr != nil || !catalog.InsidePath(held.Binary) {
			continue
		}

		store.installed = append(store.installed, Slicer{
			Version: version, Channel: held.Channel, Source: held.Source,
			Binary: filepath.Join(dir, filepath.FromSlash(held.Binary)), Args: held.Args,
			SHA256: held.SHA256, Verified: held.Verified, InstalledAt: held.InstalledAt,
			Released: held.Released, Notes: held.Notes, Bytes: held.Bytes,
		})
	}

	return store, nil
}

// Installed returns what is installed, newest first.
func (s *Store) Installed() []Slicer {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := slices.Clone(s.installed)
	slices.SortFunc(out, func(a, b Slicer) int { return b.Version.Compare(a.Version) })

	return out
}

// Running returns the installs that are running or failed, by version.
func (s *Store) Running() []Progress {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Progress, 0, len(s.running))
	for _, held := range s.running {
		out = append(out, *held)
	}

	slices.SortFunc(out, func(a, b Progress) int {
		switch {
		case a.Version < b.Version:
			return -1
		case a.Version > b.Version:
			return 1
		}

		return 0
	})

	return out
}

// Start begins installing one candidate in the background and returns at
// once. done is called when it ends, with the error when it failed.
func (s *Store) Start(ctx context.Context, candidate catalog.Candidate, done func(error)) error {
	name := candidate.Version.String()

	s.mu.Lock()

	if held, busy := s.running[name]; busy && held.Phase != Failed {
		s.mu.Unlock()

		return fmt.Errorf("%w: %s", ErrBusy, name)
	}

	s.running[name] = &Progress{Version: name, Phase: Downloading, Total: candidate.Build.Size}
	s.mu.Unlock()

	go func() {
		err := s.install(ctx, candidate)
		if err != nil {
			s.phase(name, func(p *Progress) {
				p.Phase = Failed
				p.Error = err.Error()
			})
		} else {
			s.mu.Lock()
			delete(s.running, name)
			s.mu.Unlock()
		}

		if done != nil {
			done(err)
		}
	}()

	return nil
}

// install downloads, unpacks and checks one build, and records it.
func (s *Store) install(ctx context.Context, candidate catalog.Candidate) error {
	name := candidate.Version.String()

	archive, err := Fetch(ctx, s.client, candidate.Build, s.cache, func(done, total int64) {
		s.phase(name, func(p *Progress) { p.Done, p.Total = done, total })
	})
	if err != nil {
		return err
	}

	s.phase(name, func(p *Progress) { p.Phase = Unpacking })

	// Each install unpacks into a fresh directory and the index points at it,
	// so a job running on an older install of the same version keeps its files.
	dest, err := os.MkdirTemp(s.dir, name+"-")
	if err != nil {
		return fmt.Errorf("make the directory for %s: %w", name, err)
	}

	binary, err := Unpack(ctx, candidate.Build, archive, dest)
	if err != nil {
		_ = os.RemoveAll(dest) // a half-unpacked build is never used

		return err
	}

	s.phase(name, func(p *Progress) { p.Phase = Checking })

	probed, err := Probe(ctx, binary, candidate.Build.Args)
	if err == nil && probed.Compare(candidate.Version) != 0 {
		err = fmt.Errorf("%w: the build names %s and the manifest %s", ErrProbe, probed, candidate.Version)
	}

	if err != nil {
		_ = os.RemoveAll(dest) // a build that is not the version it claims is never used

		return err
	}

	return s.record(Slicer{
		Version: candidate.Version, Channel: candidate.Channel, Source: candidate.Source,
		Binary: binary, Args: candidate.Build.Args, SHA256: filepath.Base(archive), Verified: candidate.Verified(),
		InstalledAt: time.Now().UTC(), Released: candidate.Released, Notes: candidate.Notes,
		Bytes: DiskBytes(dest),
	})
}

// record adds or replaces one installed build and writes the index. The
// directory of a replaced build is removed unless a job is running on it.
func (s *Store) record(added Slicer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var replaced []Slicer

	s.installed = slices.DeleteFunc(s.installed, func(held Slicer) bool {
		if held.Version.Compare(added.Version) == 0 {
			replaced = append(replaced, held)

			return true
		}

		return false
	})
	s.installed = append(s.installed, added)

	if err := s.writeIndex(); err != nil {
		return err
	}

	for _, old := range replaced {
		if s.inUse[old.Name()] == 0 {
			s.removeFiles(old)
		}
	}

	return nil
}

// Remove uninstalls one version. A version a job is running on is refused.
func (s *Store) Remove(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.inUse[version] > 0 {
		return fmt.Errorf("%w: a job is slicing with %s", ErrBusy, version)
	}

	index := slices.IndexFunc(s.installed, func(held Slicer) bool { return held.Name() == version })
	if index < 0 {
		delete(s.running, version)

		return fmt.Errorf("%w: %s", ErrNotInstalled, version)
	}

	removed := s.installed[index]
	s.installed = slices.Delete(s.installed, index, index+1)

	if err := s.writeIndex(); err != nil {
		return err
	}

	s.removeFiles(removed)

	return nil
}

// Acquire marks a version as in use by a job, and the returned function ends
// that. A version in use is not removed.
func (s *Store) Acquire(version string) func() {
	s.mu.Lock()
	s.inUse[version]++
	s.mu.Unlock()

	return func() {
		s.mu.Lock()
		s.inUse[version]--
		s.mu.Unlock()
	}
}

// removeFiles deletes the directory a build was unpacked into. The caller
// holds the lock.
func (s *Store) removeFiles(old Slicer) {
	relative, err := filepath.Rel(s.dir, old.Binary)
	if err != nil {
		return
	}

	top, _, _ := cutPath(relative)
	if top == "" || top == ".." || top == indexName {
		return
	}

	_ = os.RemoveAll(filepath.Join(s.dir, top)) // what is left is only disk room, and the index no longer names it
}

func cutPath(relative string) (string, string, bool) {
	slashed := filepath.ToSlash(relative)
	for index := range len(slashed) {
		if slashed[index] == '/' {
			return slashed[:index], slashed[index+1:], true
		}
	}

	return slashed, "", false
}

// writeIndex writes the index under a temporary name and renames it into
// place. The caller holds the lock.
func (s *Store) writeIndex() error {
	records := make([]record, 0, len(s.installed))

	for _, held := range s.installed {
		relative, err := filepath.Rel(s.dir, held.Binary)
		if err != nil {
			return fmt.Errorf("name %s inside the slicer directory: %w", held.Name(), err)
		}

		records = append(records, record{
			Version: held.Name(), Channel: held.Channel, Source: held.Source,
			Binary: filepath.ToSlash(relative), Args: held.Args, SHA256: held.SHA256, Verified: held.Verified,
			InstalledAt: held.InstalledAt, Released: held.Released, Notes: held.Notes, Bytes: held.Bytes,
		})
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("write the slicer index: %w", err)
	}

	return WriteFileAtomic(filepath.Join(s.dir, indexName), data)
}

func (s *Store) phase(version string, change func(*Progress)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if held, ok := s.running[version]; ok {
		change(held)
	}
}

// DiskBytes is the size of every regular file under root, or of root itself
// when it is a file. A link is not followed.
func DiskBytes(root string) int64 {
	var total int64

	_ = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return nil //nolint:nilerr // an entry that cannot be read adds nothing to the size
		}

		if info, infoErr := entry.Info(); infoErr == nil {
			total += info.Size()
		}

		return nil
	}) // the walk reports nothing it does not skip

	return total
}

// WriteFileAtomic writes data under a temporary name beside name and renames
// it into place, so a reader never sees half a file.
func WriteFileAtomic(name string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(name), filepath.Base(name)+".*")
	if err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(name), err)
	}

	temporary := file.Name()

	_, err = file.Write(data)
	if syncErr := file.Sync(); err == nil {
		err = syncErr
	}

	if closeErr := file.Close(); err == nil {
		err = closeErr
	}

	if err == nil {
		err = os.Rename(temporary, name)
	}

	if err != nil {
		_ = os.Remove(temporary) // the write failed; the old file is still in place

		return fmt.Errorf("write %s: %w", filepath.Base(name), err)
	}

	return nil
}
