// Package memory keeps storage objects in the process's own memory.
//
// It is a second implementation of sdk/storageapi, for tests and
// demonstrations. A restart loses everything. It compiles against sdk/ alone
// and enters a binary by being named in a build configuration, so it shows
// what an adapter author has to work with.
//
// Stores differ in the optional interfaces. This one is a
// [storageapi.RangeReader] and a [storageapi.Renamer]. It gives out no file
// handles and no URLs, so it is neither a [storageapi.LocalFiles] nor a
// [storageapi.Presigner].
package memory

import (
	"bytes"
	"context"

	"fmt"
	"hash"
	"io"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// Type is the value of a storage resource's `type` in configuration that
// selects this adapter.
const Type = "memory"

// DefaultDisplayName is the label the UI shows when the operator names none.
const DefaultDisplayName = "In memory"

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { storageapi.Register(Type, Doc(), New) }

// Doc is what the published extension reference says about this store.
func Doc() extension.Doc {
	return extension.Doc{
		Summary: "Keeps objects in the server's own memory, for tests and demonstrations.",
		Description: "Everything this store holds lives in the process and is lost when it stops, so an " +
			"operator pointing a printer at it is pointing it at storage that empties on restart. It " +
			"serves byte ranges and renames, and it hands out no file handles and no download URLs. " +
			"spec.Path is ignored: there is no directory to name.",
		Version:   "1.0.0",
		Contract:  storageapi.Contract,
		Stability: extension.StabilityStable,
	}
}

// New builds a Provider from its spec. It is the storageapi.ProviderFactory for
// Type.
func New(spec storageapi.ProviderSpec) (storageapi.Provider, error) { return Open(spec), nil }

// Provider is the sdk/storageapi.Provider this package registers. It is safe
// for concurrent use.
type Provider struct {
	id          string
	displayName string
	allowed     []string

	mu      sync.RWMutex
	objects map[string]object
	dirs    map[string]int64
}

// The interfaces this store implements, checked by the compiler.
var (
	_ storageapi.Provider    = (*Provider)(nil)
	_ storageapi.RangeReader = (*Provider)(nil)
	_ storageapi.Renamer     = (*Provider)(nil)
)

// object is one stored object: its bytes, their digest and the write time.
type object struct {
	data  []byte
	sha   string
	mtime int64
}

// Open builds a Provider from its spec, falling back to DefaultDisplayName when
// the spec names no display name.
func Open(spec storageapi.ProviderSpec) *Provider {
	displayName := spec.Name
	if displayName == "" {
		displayName = DefaultDisplayName
	}

	id := spec.ID
	if id == "" {
		id = spec.Type
	}

	return &Provider{
		id:          id,
		displayName: displayName,
		allowed:     spec.AllowedExtensions,
		objects:     map[string]object{},
		dirs:        map[string]int64{},
	}
}

// Name is the storage id callers address.
func (p *Provider) Name() string { return p.id }

// DisplayName is the label the UI renders.
func (p *Provider) DisplayName() string { return p.displayName }

// Capabilities declares browse and write. storageapi.Probe adds the optional
// abilities from the interfaces this type implements.
func (p *Provider) Capabilities() storageapi.Caps {
	return storageapi.Caps{Browse: true, Write: true}
}

// List returns the entries directly inside relDir.
func (p *Provider) List(ctx context.Context, relDir string) ([]storageapi.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	prefix := storageapi.CleanRelPath(relDir)

	p.mu.RLock()
	defer p.mu.RUnlock()

	if _, isFile := p.objects[prefix]; isFile {
		return nil, storageapi.ErrInvalidPath
	}

	if _, ok := p.dirs[prefix]; prefix != "" && !ok {
		return nil, storageapi.ErrInvalidPath
	}

	out := []storageapi.FileInfo{}

	for name, stored := range p.objects {
		if child, ok := childOf(prefix, name); ok {
			out = append(out, storageapi.FileInfo{
				Name: child, Path: name, Size: int64(len(stored.data)),
				MTime: stored.mtime, SHA256: stored.sha,
			})
		}
	}

	for name, mtime := range p.dirs {
		if child, ok := childOf(prefix, name); ok {
			out = append(out, storageapi.FileInfo{Name: child, Path: name, IsDir: true, MTime: mtime})
		}
	}

	// A map iterates in a random order, and a listing must be stable.
	slices.SortFunc(out, func(a, b storageapi.FileInfo) int { return strings.Compare(a.Path, b.Path) })

	return out, nil
}

// childOf reports the immediate child of prefix that name names, if it is one.
func childOf(prefix, name string) (string, bool) {
	if name == "" || name == prefix {
		return "", false
	}

	rest := name
	if prefix != "" {
		if !strings.HasPrefix(name, prefix+"/") {
			return "", false
		}

		rest = name[len(prefix)+1:]
	}

	if strings.Contains(rest, "/") {
		return "", false
	}

	return rest, true
}

// Stat describes one entry.
func (p *Provider) Stat(ctx context.Context, relPath string) (storageapi.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return storageapi.FileInfo{}, err
	}

	clean := storageapi.CleanRelPath(relPath)

	p.mu.RLock()
	defer p.mu.RUnlock()

	if stored, ok := p.objects[clean]; ok {
		return storageapi.FileInfo{
			Name: path.Base(clean), Path: clean, Size: int64(len(stored.data)),
			MTime: stored.mtime, SHA256: stored.sha,
		}, nil
	}

	if mtime, ok := p.dirs[clean]; ok || clean == "" {
		return storageapi.FileInfo{Name: path.Base(clean), Path: clean, IsDir: true, MTime: mtime}, nil
	}

	return storageapi.FileInfo{}, storageapi.ErrInvalidPath
}

// Open opens an object for reading. The reader reads the stored slice, with
// no copy.
func (p *Provider) Open(ctx context.Context, relPath string) (io.ReadCloser, error) {
	data, err := p.bytesOf(ctx, relPath)
	if err != nil {
		return nil, err
	}

	return io.NopCloser(bytes.NewReader(data)), nil
}

// OpenRange serves part of an object. As the contract asks, an offset at or
// past the length returns a reader at end of file, not an error.
func (p *Provider) OpenRange(
	ctx context.Context, relPath string, offset, length int64,
) (io.ReadCloser, error) {
	data, err := p.bytesOf(ctx, relPath)
	if err != nil {
		return nil, err
	}

	if offset < 0 {
		return nil, storageapi.ErrInvalidPath
	}

	if offset > int64(len(data)) {
		offset = int64(len(data))
	}

	rest := data[offset:]
	if length >= 0 && length < int64(len(rest)) {
		rest = rest[:length]
	}

	return io.NopCloser(bytes.NewReader(rest)), nil
}

func (p *Provider) bytesOf(ctx context.Context, relPath string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	clean := storageapi.CleanRelPath(relPath)

	p.mu.RLock()
	defer p.mu.RUnlock()

	stored, ok := p.objects[clean]
	if !ok {
		return nil, storageapi.ErrInvalidPath
	}

	return stored.data, nil
}

// Create starts a write at relPath. Nothing is under the name until Commit.
func (p *Provider) Create(ctx context.Context, relPath string) (storageapi.Writer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	clean := storageapi.CleanRelPath(relPath)
	if clean == "" {
		return nil, storageapi.ErrInvalidPath
	}

	if err := storageapi.ValidateExtension(path.Base(clean), p.allowed); err != nil {
		return nil, err
	}

	return &writer{store: p, name: clean, digest: storageapi.NewDigest()}, nil
}

// Delete removes an object, or a directory and everything under it.
func (p *Provider) Delete(ctx context.Context, relPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	clean := storageapi.CleanRelPath(relPath)
	if clean == "" {
		return storageapi.ErrInvalidPath
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.objects, clean)
	delete(p.dirs, clean)

	for name := range p.objects {
		if strings.HasPrefix(name, clean+"/") {
			delete(p.objects, name)
		}
	}

	for name := range p.dirs {
		if strings.HasPrefix(name, clean+"/") {
			delete(p.dirs, name)
		}
	}

	return nil
}

// DeleteMany attempts every path and reports how many failed. The ones that
// succeed are still removed.
func (p *Provider) DeleteMany(ctx context.Context, relPaths []string) error {
	failed := 0

	for _, rel := range relPaths {
		if err := p.Delete(ctx, rel); err != nil {
			failed++
		}
	}

	if failed == 0 {
		return nil
	}

	return fmt.Errorf("%w: %d of %d", storageapi.ErrDeletePartial, failed, len(relPaths))
}

// Mkdir creates a directory and its parents.
func (p *Provider) Mkdir(ctx context.Context, relPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	clean := storageapi.CleanRelPath(relPath)
	if clean == "" {
		return storageapi.ErrInvalidPath
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, taken := p.objects[clean]; taken {
		return storageapi.ErrInvalidPath
	}

	p.makeDirs(clean)

	return nil
}

// makeDirs records the directory and every parent above it. The caller holds
// the write lock.
func (p *Provider) makeDirs(clean string) {
	now := time.Now().Unix()

	for at := clean; at != "" && at != "."; at = parentOf(at) {
		if _, ok := p.dirs[at]; !ok {
			p.dirs[at] = now
		}
	}
}

func parentOf(clean string) string {
	cut := strings.LastIndex(clean, "/")
	if cut < 0 {
		return ""
	}

	return clean[:cut]
}

// Rename moves an object or a directory within the store.
func (p *Provider) Rename(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	source, destination := storageapi.CleanRelPath(from), storageapi.CleanRelPath(to)
	if source == "" || destination == "" {
		return storageapi.ErrInvalidPath
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if stored, ok := p.objects[source]; ok {
		// A rename creates a name, so the extension allowlist applies.
		if err := storageapi.ValidateExtension(path.Base(destination), p.allowed); err != nil {
			return err
		}

		delete(p.objects, source)
		p.makeDirs(parentOf(destination))
		p.objects[destination] = stored

		return nil
	}

	if _, ok := p.dirs[source]; !ok {
		return storageapi.ErrInvalidPath
	}

	p.moveTree(source, destination)

	return nil
}

// moveTree moves a directory and everything under it. The caller holds the
// write lock.
func (p *Provider) moveTree(source, destination string) {
	delete(p.dirs, source)
	p.makeDirs(destination)

	for name, stored := range p.objects {
		if strings.HasPrefix(name, source+"/") {
			delete(p.objects, name)
			p.objects[destination+name[len(source):]] = stored
		}
	}

	for name, mtime := range p.dirs {
		if strings.HasPrefix(name, source+"/") {
			delete(p.dirs, name)
			p.dirs[destination+name[len(source):]] = mtime
		}
	}
}

// writer is one object being written. The bytes are buffered until Commit,
// so a half-written object is never visible.
type writer struct {
	store    *Provider
	name     string
	digest   hash.Hash
	buf      bytes.Buffer
	finished bool
}

var _ storageapi.Writer = (*writer)(nil)

func (w *writer) Write(p []byte) (int, error) {
	if w.finished {
		return 0, storageapi.ErrWriteClosed
	}

	written, err := w.buf.Write(p)

	// hash.Hash never reports an error and never writes short.
	_, _ = w.digest.Write(p[:written])

	return written, err
}

// Commit publishes the object under its name and reports what was written.
func (w *writer) Commit(ctx context.Context) (storageapi.WriteResult, error) {
	if w.finished {
		return storageapi.WriteResult{}, storageapi.ErrWriteClosed
	}

	if err := ctx.Err(); err != nil {
		return storageapi.WriteResult{}, err
	}

	w.store.mu.Lock()
	defer w.store.mu.Unlock()

	if _, isDir := w.store.dirs[w.name]; isDir {
		return storageapi.WriteResult{}, storageapi.ErrInvalidPath
	}

	w.finished = true

	data := w.buf.Bytes()
	sum := storageapi.Sum(w.digest)

	w.store.makeDirs(parentOf(w.name))
	w.store.objects[w.name] = object{data: data, sha: sum, mtime: time.Now().Unix()}

	return storageapi.WriteResult{Path: w.name, Size: int64(len(data)), SHA256: sum}, nil
}

// Abort discards the write. It does nothing after Commit, so a caller can
// defer it.
func (w *writer) Abort(_ context.Context) error {
	if w.finished {
		return nil
	}

	w.finished = true
	w.buf.Reset()

	return nil
}
