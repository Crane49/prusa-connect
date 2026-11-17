package files

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"

	fractalfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// metaFolder is the folder the disk store keeps its own data in. It is never
// published.
const metaFolder = ".pcl-meta"

// updatesBuffer is how many root hash changes Updated holds before a scan
// waits for the reader.
const updatesBuffer = 64

// Published is one object a content store of this server publishes. Listing
// is the bytes of a listing, and nil for a file. MTime is a file's time of
// change when the scan read it.
type Published struct {
	Listing []byte
	Storage string
	Path    string
	Size    int64
	MTime   int64
}

// dirRec is one listing of a tree: its identity, its bytes and the entries it
// was built from, under the names the storage gave them.
type dirRec struct {
	id   fractalfiles.Identity
	data []byte
	held []found
}

// tree is the last whole scan of one storage, kept as its listings by path.
// An unshared folder is cut out of it without reading the storage again.
type tree struct {
	dirs    map[string]dirRec
	root    fractalfiles.Identity
	objects map[fractalfiles.Identity][]Published
	// shares is the shared folders this tree was last made to match, and
	// covered the part of them a scan has read.
	shares  []Share
	covered []Share
}

func newTree(storage string, dirs map[string]dirRec, shares, covered []Share) *tree {
	t := &tree{
		dirs: dirs, root: dirs[""].id, shares: shares, covered: covered,
		objects: map[fractalfiles.Identity][]Published{},
	}

	for _, at := range slices.Sorted(maps.Keys(dirs)) {
		rec := dirs[at]
		t.objects[rec.id] = append(t.objects[rec.id],
			Published{Listing: rec.data, Storage: storage, Path: at, Size: int64(len(rec.data))})

		for _, one := range rec.held {
			if one.entry.Kind == fractalfiles.KindFile {
				size := int64(one.entry.Size) //nolint:gosec // a file length fits int64
				t.objects[one.entry.Hash] = append(t.objects[one.entry.Hash],
					Published{Storage: storage, Path: path.Join(at, one.stored), Size: size, MTime: one.mtime})
			}
		}
	}

	return t
}

// keep copies the listing at and every listing under it into out.
func (t *tree) keep(at string, out map[string]dirRec) bool {
	rec, ok := t.dirs[at]
	if !ok {
		return false
	}

	out[at] = rec

	for _, one := range rec.held {
		if one.entry.Kind == fractalfiles.KindDirectory {
			t.keep(path.Join(at, one.stored), out)
		}
	}

	return true
}

// Publisher builds the content stores of this server from the shared folders.
// It publishes one root hash per storage, and answers only for objects
// reachable from a current root hash. It is safe for concurrent use.
type Publisher struct {
	st       Storage
	shares   *Shares
	storages func(ctx context.Context) ([]string, error)
	log      *slog.Logger
	dataDir  string
	updated  chan string

	// scanning keeps two scans from running at once.
	scanning sync.Mutex
	// announcing keeps a name from being lost while announce drains Updated.
	announcing sync.Mutex

	digestsMu sync.Mutex
	digests   *digests

	mu    sync.RWMutex
	known map[string]bool
	trees map[string]*tree
}

// NewPublisher returns a Publisher that reads st, publishes what shares names,
// and keeps the digests it computes in dataDir. storages names every storage
// that has a content store.
func NewPublisher(
	st Storage, shares *Shares, dataDir string,
	storages func(ctx context.Context) ([]string, error), log *slog.Logger,
) *Publisher {
	return &Publisher{
		st:       st,
		shares:   shares,
		storages: storages,
		log:      log,
		dataDir:  dataDir,
		updated:  make(chan string, updatesBuffer),
		known:    map[string]bool{},
		trees:    map[string]*tree{},
	}
}

// Updated receives the name of a storage when it gets a content store, and
// whenever its root hash changes. A reader that falls behind finds each
// storage once, not once per change.
func (p *Publisher) Updated() <-chan string { return p.updated }

// RootHash returns the root hash of one storage. It is false before the first
// whole scan of the storage, and for a storage the operator removed.
func (p *Publisher) RootHash(ctx context.Context, storage string) (fractalfiles.Identity, bool) {
	p.prune(ctx)

	p.mu.RLock()
	defer p.mu.RUnlock()

	held, ok := p.trees[storage]
	if !ok {
		return fractalfiles.Identity{}, false
	}

	return held.root, true
}

// Lookup returns an object reachable from a current root hash. A folder
// unshared since the last scan is cut out of the tree first, so unsharing
// takes effect at once.
func (p *Publisher) Lookup(ctx context.Context, id fractalfiles.Identity) (Published, bool) {
	p.prune(ctx)

	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, held := range p.trees {
		if objects := held.objects[id]; len(objects) > 0 {
			return objects[0], true
		}
	}

	return Published{}, false
}

// ListingOf returns the bytes of a listing that storage publishes under a
// current root hash. It cuts unshared folders out first, like Lookup.
func (p *Publisher) ListingOf(ctx context.Context, storage string, id fractalfiles.Identity) ([]byte, bool) {
	p.prune(ctx)

	p.mu.RLock()
	defer p.mu.RUnlock()

	held, ok := p.trees[storage]
	if !ok {
		return nil, false
	}

	for _, object := range held.objects[id] {
		if object.Listing != nil {
			return object.Listing, true
		}
	}

	return nil, false
}

// prune cuts every unshared folder out of the trees and announces the new
// root hashes. A folder that was added waits for the next scan.
func (p *Publisher) prune(ctx context.Context) {
	current := p.shares.List()

	p.mu.RLock()
	stale := false

	for _, held := range p.trees {
		stale = stale || !slices.Equal(held.shares, current)
	}

	p.mu.RUnlock()

	if !stale {
		return
	}

	var changed []string

	p.mu.Lock()

	for storage, held := range p.trees {
		if slices.Equal(held.shares, current) {
			continue
		}

		next, err := p.restricted(ctx, storage, held, current)
		if err != nil {
			p.log.ErrorContext(
				ctx,
				"a tree cannot be cut to the shared folders and is withdrawn",
				"storage",
				storage,
				"error",
				err,
			)
			delete(p.trees, storage)
			changed = append(changed, storage)

			continue
		}

		p.trees[storage] = next

		if next.root != held.root {
			changed = append(changed, storage)
		}
	}

	p.mu.Unlock()

	for _, storage := range changed {
		p.announce(storage)
	}
}

// restricted returns held cut to the folders of current that it already holds.
func (p *Publisher) restricted(ctx context.Context, storage string, held *tree, current []Share) (*tree, error) {
	covered := slices.DeleteFunc(slices.Clone(current), func(s Share) bool {
		return s.Storage != storage || !slices.ContainsFunc(held.covered, func(o Share) bool {
			return inside(s.Path, o.Path)
		})
	})

	dirs := map[string]dirRec{}

	want := sharedTree(covered, storage)
	if _, _, err := p.restrict(ctx, storage, held, "", want, true, dirs); err != nil {
		return nil, err
	}

	return newTree(storage, dirs, current, covered), nil
}

// restrict rebuilds the listing at, and those under it, from the listings in
// held.
func (p *Publisher) restrict(
	ctx context.Context, storage string, held *tree, at string, want *shared, root bool, out map[string]dirRec,
) (dirRec, bool, error) {
	if want.whole {
		return held.dirs[at], held.keep(at, out), nil
	}

	var entries []found

	for _, name := range slices.Sorted(maps.Keys(want.children)) {
		rec, ok, err := p.restrict(ctx, storage, held, path.Join(at, name), want.children[name], false, out)
		if err != nil {
			return dirRec{}, false, err
		}

		if ok {
			entries = append(entries, directoryEntry(name, rec))
		}
	}

	if !root && len(entries) == 0 {
		return dirRec{}, false, nil
	}

	rec, err := p.seal(ctx, storage, at, entries)
	if err != nil {
		return dirRec{}, false, err
	}

	out[at] = rec

	return rec, true, nil
}

// directoryEntry is the entry of a folder in its parent.
func directoryEntry(stored string, rec dirRec) found {
	return found{stored: stored, entry: fractalfiles.Entry{
		Kind: fractalfiles.KindDirectory, Size: uint64(len(rec.data)), Hash: rec.id,
	}}
}

// seal encodes the listing of one folder.
func (p *Publisher) seal(ctx context.Context, storage, at string, held []found) (dirRec, error) {
	data, err := fractalfiles.Listing{Entries: p.named(ctx, storage, at, held)}.Encode()
	if err != nil {
		return dirRec{}, fmt.Errorf("encode the listing of %s: %w", at, err)
	}

	return dirRec{id: fractalfiles.Sum(data), data: data, held: held}, nil
}

// Run scans at once, after every change to the shared folders and every
// interval, until ctx ends. A scan that fails keeps the last root hash. An
// unshare is pruned and announced at once, also while a scan is reading.
func (p *Publisher) Run(ctx context.Context, every time.Duration) error {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	changed := make(chan struct{}, 1)
	watching := make(chan struct{})

	go func() {
		defer close(watching)

		for {
			select {
			case <-ctx.Done():
				return
			case <-p.shares.Changed():
				p.prune(ctx)

				select {
				case changed <- struct{}{}:
				default:
				}
			}
		}
	}()

	defer func() { <-watching }()

	for {
		if err := p.Scan(ctx); err != nil && ctx.Err() == nil {
			p.log.WarnContext(ctx, "a scan of the shared folders failed, and the last root hash stays", "error", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-changed:
		}
	}
}

// Scan reads the shared folders of every storage and publishes a new root
// hash for each storage whose whole tree it read. A storage whose shared
// folders changed during the read keeps what it had. A storage that fails
// keeps its last root hash, and Scan returns the failures.
func (p *Publisher) Scan(ctx context.Context) error {
	p.scanning.Lock()
	defer p.scanning.Unlock()

	sums, err := p.openDigests()
	if err != nil {
		return err
	}

	names, err := p.storages(ctx)
	if err != nil {
		return fmt.Errorf("name the storages: %w", err)
	}

	for _, name := range names {
		p.learn(name)
	}

	var failed []error

	for _, name := range p.knownStorages() {
		if scanErr := p.scanStorage(ctx, sums, name); scanErr != nil {
			failed = append(failed, fmt.Errorf("scan the storage %s: %w", name, scanErr))
		}
	}

	if saveErr := sums.save(); saveErr != nil {
		failed = append(failed, saveErr)
	}

	return errors.Join(failed...)
}

func (p *Publisher) openDigests() (*digests, error) {
	p.digestsMu.Lock()
	defer p.digestsMu.Unlock()

	if p.digests == nil {
		opened, err := openDigests(p.dataDir, p.log)
		if err != nil {
			return nil, err
		}

		p.digests = opened
	}

	return p.digests, nil
}

// learn records a storage and announces it on Updated the first time. Its
// content store then exists with no root hash until its first scan ends.
func (p *Publisher) learn(storage string) {
	p.mu.Lock()
	fresh := !p.known[storage]
	p.known[storage] = true
	p.mu.Unlock()

	if fresh {
		p.announce(storage)
	}
}

func (p *Publisher) knownStorages() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return slices.Sorted(maps.Keys(p.known))
}

// announce tells the reader of Updated that storage changed. It never waits:
// a full buffer is drained and refilled with each name once.
func (p *Publisher) announce(storage string) {
	p.announcing.Lock()
	defer p.announcing.Unlock()

	select {
	case p.updated <- storage:
		return
	default:
	}

	pending := map[string]bool{storage: true}

	for drained := false; !drained; {
		select {
		case name := <-p.updated:
			pending[name] = true
		default:
			drained = true
		}
	}

	for _, name := range slices.Sorted(maps.Keys(pending)) {
		select {
		case p.updated <- name:
		default: // shortcut: more storages than the buffer holds lose one name, and a server has a few.
		}
	}
}

// scanStorage builds the tree of one storage and publishes it whole. A
// storage the host does not declare has no root hash, and its store stays.
func (p *Publisher) scanStorage(ctx context.Context, sums *digests, storage string) error {
	// The names at the root are read only to learn that the storage is
	// there, and none of them is published.
	if _, err := p.st.List(ctx, storageapi.ListParams{Storage: storage}); err != nil {
		if errors.Is(err, storageapi.ErrNoFile) {
			p.replace(storage, nil)

			return nil
		}

		return err
	}

	shares := p.shares.List()
	run := &scan{p: p, sums: sums, storage: storage, dirs: map[string]dirRec{}, visited: map[digestKey]bool{}}

	if _, _, _, err := run.folder(ctx, "", sharedTree(shares, storage), true); err != nil {
		return err
	}

	covered := slices.DeleteFunc(slices.Clone(shares), func(s Share) bool { return s.Storage != storage })

	if p.replace(storage, newTree(storage, run.dirs, shares, covered)) {
		sums.keep(storage, run.visited)
	}

	return nil
}

// replace publishes next for storage. A nil next means no root hash. It
// returns false, and installs nothing, when the shared folders changed since
// next was built.
func (p *Publisher) replace(storage string, next *tree) bool {
	p.mu.Lock()

	if next != nil && !slices.Equal(next.shares, p.shares.List()) {
		p.mu.Unlock()

		return false
	}

	previous, had := p.trees[storage]

	if next == nil {
		delete(p.trees, storage)
	} else {
		p.trees[storage] = next
	}
	p.mu.Unlock()

	if had != (next != nil) || had && previous.root != next.root {
		p.announce(storage)
	}

	return true
}

// shared is one folder of the tree of shared folders. A whole folder is
// shared with everything in it. Any other folder is on the path to a shared
// one, and only that path is published.
type shared struct {
	whole    bool
	children map[string]*shared
}

// sharedTree builds the folders of storage that are shared or on the path to
// a shared one. A folder inside a shared one adds nothing.
func sharedTree(shares []Share, storage string) *shared {
	root := &shared{children: map[string]*shared{}}

	for _, share := range shares {
		if share.Storage != storage {
			continue
		}

		at := root
		for name := range strings.SplitSeq(share.Path, "/") {
			next, ok := at.children[name]
			if !ok {
				next = &shared{children: map[string]*shared{}}
				at.children[name] = next
			}

			at = next
		}

		at.whole = true
	}

	return root
}

// scan is one scan of one storage.
type scan struct {
	p       *Publisher
	sums    *digests
	storage string
	dirs    map[string]dirRec
	visited map[digestKey]bool
}

// found is one entry of a folder with the name the storage gave it.
type found struct {
	stored string
	entry  fractalfiles.Entry
	mtime  int64
}

// folder builds the listing of one folder. ok is false for a folder to leave
// out: one that is missing, or an empty one on the path to a share.
func (s *scan) folder(
	ctx context.Context, at string, want *shared, root bool,
) (id fractalfiles.Identity, size uint64, ok bool, err error) {
	var held []found

	if want.whole {
		held, err = s.wholeFolder(ctx, at)
		if errors.Is(err, storageapi.ErrNoFile) {
			s.p.log.InfoContext(ctx, "a shared folder is not there and is left out",
				"storage", s.storage, "path", at)

			return id, 0, false, nil
		}
	} else {
		held, err = s.pathFolder(ctx, at, want)
	}

	if err != nil {
		return id, 0, false, err
	}

	if !want.whole && !root && len(held) == 0 {
		return id, 0, false, nil
	}

	rec, err := s.p.seal(ctx, s.storage, at, held)
	if err != nil {
		return id, 0, false, err
	}

	s.dirs[at] = rec

	return rec.id, uint64(len(rec.data)), true, nil
}

// pathFolder builds the entries of a folder on the path to a shared one: only
// the folders that lead to a share.
func (s *scan) pathFolder(ctx context.Context, at string, want *shared) ([]found, error) {
	var held []found

	for _, name := range slices.Sorted(maps.Keys(want.children)) {
		id, size, ok, err := s.folder(ctx, path.Join(at, name), want.children[name], false)
		if err != nil {
			return nil, err
		}

		if ok {
			held = append(held, found{stored: name,
				entry: fractalfiles.Entry{Kind: fractalfiles.KindDirectory, Size: size, Hash: id}})
		}
	}

	return held, nil
}

// wholeFolder builds the entries of a shared folder: every file and folder in
// it, except the store's own folder.
func (s *scan) wholeFolder(ctx context.Context, at string) ([]found, error) {
	listed, err := s.list(ctx, at)
	if err != nil {
		return nil, err
	}

	held := make([]found, 0, len(listed))

	for _, entry := range listed {
		inside := path.Join(at, entry.Name)

		if entry.Dir {
			if entry.Name == metaFolder {
				continue
			}

			id, size, ok, folderErr := s.folder(ctx, inside, &shared{whole: true}, false)
			if folderErr != nil {
				return nil, folderErr
			}

			if ok {
				held = append(held, found{stored: entry.Name,
					entry: fractalfiles.Entry{Kind: fractalfiles.KindDirectory, Size: size, Hash: id}})
			}

			continue
		}

		id, size, fileErr := s.file(ctx, inside, entry)

		switch {
		case errors.Is(fileErr, storageapi.ErrNoFile):
			continue
		case errors.Is(fileErr, storageapi.ErrUnreadable), errors.Is(fileErr, storageapi.ErrDenied):
			s.p.log.WarnContext(ctx, "a file cannot be read and is left out",
				"storage", s.storage, "path", inside, "error", fileErr.Error())

			continue
		case fileErr != nil:
			return nil, fileErr
		}

		held = append(held, found{stored: entry.Name, mtime: entry.MTime,
			entry: fractalfiles.Entry{Kind: fractalfiles.KindFile, Size: length(size), Hash: id}})
	}

	return held, nil
}

// list reads every page of one folder.
func (s *scan) list(ctx context.Context, at string) ([]storageapi.ListEntry, error) {
	var (
		all    []storageapi.ListEntry
		cursor string
	)

	for {
		page, err := s.p.st.List(ctx, storageapi.ListParams{Storage: s.storage, Path: at, Cursor: cursor})
		if err != nil {
			return nil, err
		}

		all = append(all, page.Entries...)
		if page.Next == "" {
			return all, nil
		}

		cursor = page.Next
	}
}

// file returns the identity and the length of one file. It reads the file
// only when neither the storage nor the digest cache has its digest.
func (s *scan) file(ctx context.Context, at string, entry storageapi.ListEntry) (fractalfiles.Identity, int64, error) {
	if entry.SHA256 != "" {
		if id, err := fromHex(entry.SHA256); err == nil {
			return id, entry.Size, nil
		}
	}

	key := digestKey{Storage: s.storage, Path: at, Size: entry.Size, MTime: entry.MTime}
	s.visited[key] = true

	if id, ok := s.sums.get(key); ok {
		return id, entry.Size, nil
	}

	id, size, err := s.hash(ctx, at)
	if err != nil {
		return fractalfiles.Identity{}, 0, err
	}

	// A file that changed while it was read is hashed again by the next scan.
	if size == entry.Size {
		s.sums.put(key, id)
	}

	return id, size, nil
}

// hash reads one file through the host and returns its identity and length.
func (s *scan) hash(ctx context.Context, at string) (fractalfiles.Identity, int64, error) {
	hasher := fractalfiles.NewHasher()

	var offset int64

	for {
		chunk, err := s.p.st.Read(ctx, storageapi.ReadParams{
			Storage: s.storage, Path: at, Offset: offset, Length: storageapi.MaxReadBytes,
		})
		if err != nil {
			return fractalfiles.Identity{}, 0, err
		}

		_, _ = hasher.Write(chunk.Data) // a hash never fails to take bytes
		offset += int64(len(chunk.Data))

		if chunk.EOF || len(chunk.Data) == 0 {
			return hasher.Identity(), offset, nil
		}
	}
}

// named gives every entry its published name, in Normalization Form C. A
// converted name the folder already has gets a suffix. An invalid one is left out.
func (p *Publisher) named(ctx context.Context, storage, at string, held []found) []fractalfiles.Entry {
	taken := map[string]bool{}
	out := make([]fractalfiles.Entry, 0, len(held))

	var converted []found

	for _, one := range held {
		if norm.NFC.IsNormalString(one.stored) && fractalfiles.CheckName(one.stored) == nil {
			taken[one.stored] = true
			one.entry.Name = one.stored
			out = append(out, one.entry)

			continue
		}

		converted = append(converted, one)
	}

	for _, one := range converted {
		name := norm.NFC.String(one.stored)
		if err := fractalfiles.CheckName(name); err != nil {
			p.log.WarnContext(ctx, "a name breaks the rule for names and is left out",
				"storage", storage, "folder", at, "name", one.stored)

			continue
		}

		if taken[name] {
			name = unique(name, taken)
		}

		p.log.InfoContext(ctx, "a name is published in Normalization Form C",
			"storage", storage, "folder", at, "stored", one.stored, "published", name)

		taken[name] = true
		one.entry.Name = name
		out = append(out, one.entry)
	}

	return out
}

func length(size int64) uint64 {
	return uint64(max(size, 0))
}

// unique returns name with the first suffix " (2)", " (3)" and so on before
// its extension that no entry of the folder has.
func unique(name string, taken map[string]bool) string {
	ext := path.Ext(name)
	if ext == name {
		ext = ""
	}

	base := strings.TrimSuffix(name, ext)

	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, n, ext)
		if !taken[candidate] {
			return candidate
		}
	}
}

// fromHex converts the lowercase hex SHA-256 a storage keeps into an identity.
func fromHex(text string) (fractalfiles.Identity, error) {
	digest, err := hex.DecodeString(text)
	if err != nil {
		return fractalfiles.Identity{}, fmt.Errorf("the stored digest is not hex: %w", err)
	}

	return fractalfiles.ParseBytes(append([]byte{byte(fractalfiles.SHA256)}, digest...))
}

// inside reports whether name is folder or a path under it.
func inside(name, folder string) bool {
	return name == folder || strings.HasPrefix(name, folder+"/")
}
