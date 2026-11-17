// Package diskstore keeps storage objects in a directory on the server's own
// disk, confined to it.
//
// It registers no storage type. adapters/storage/local and
// adapters/storage/temporary each register one name and build on this
// package, because registering one name twice panics before main.
package diskstore

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// Doc is the entry of a disk-backed store in the published extension
// reference. The local and the temporary adapter share it. The options are
// the keys the host reads into storageapi.ProviderSpec.
func Doc(kind, label string) extension.Doc {
	return extension.Doc{
		Summary: "Keeps objects in a directory on the server's own disk, addressed as " + kind + ".",
		Description: "Every caller-supplied name is resolved through a directory handle on the store, so a " +
			"symlink or a Windows junction planted inside it is refused rather than followed. A hardlink " +
			"and a bind mount are not steps in path resolution and are outside what any confinement can " +
			"see. Uploads are written to a sibling temporary file and renamed on success, so a transfer " +
			"that fails partway leaves the existing object alone. The SHA-256 of each object is kept in a " +
			"reserved directory at the store root, so a later Stat reports the digest the write computed. " +
			"The label defaults to " + label + ".",
		Version:   "1.0.0",
		Contract:  storageapi.Contract,
		Stability: extension.StabilityStable,
		Options: []extension.Option{
			{
				Key:     "path",
				Type:    extension.OptionString,
				Summary: "The directory this instance owns. It is created when it does not exist.",
			},
			{
				Key:     "name",
				Type:    extension.OptionString,
				Default: label,
				Summary: "The label a page shows for this store.",
			},
			{
				Key:  "allowed_extensions",
				Type: extension.OptionList,
				Summary: "What an upload into this storage may be called. Omit the key for the built-in " +
					"list of print-file and image extensions; write an empty list to permit any extension.",
			},
		},
	}
}

// Provider implements storageapi.Provider on a directory.
type Provider struct {
	id          string
	displayName string
	store       store
	allowed     []string
}

// The optional interfaces this store implements. It is not a Presigner or a
// RangeReader. storageapi reads that from the missing methods.
var (
	_ storageapi.Provider   = (*Provider)(nil)
	_ storageapi.LocalFiles = (*Provider)(nil)
	_ storageapi.Renamer    = (*Provider)(nil)
)

// Open builds a Provider from its spec. label is the display name when the
// spec has none.
func Open(spec storageapi.ProviderSpec, label string) (*Provider, error) {
	opened, err := openStore(spec.Path)
	if err != nil {
		return nil, err
	}

	displayName := spec.Name
	if displayName == "" {
		displayName = label
	}

	id := spec.ID
	if id == "" {
		id = spec.Type
	}

	return &Provider{
		id:          id,
		displayName: displayName,
		store:       opened,
		allowed:     spec.AllowedExtensions,
	}, nil
}

// AllowedExtensions is the upload allowlist Create enforces. Empty means any.
func (p *Provider) AllowedExtensions() []string { return p.allowed }

// Root is the absolute directory this provider keeps its objects in.
func (p *Provider) Root() string { return p.store.root }

// Name is the storage id callers address.
func (p *Provider) Name() string { return p.id }

// DisplayName is the label the UI renders.
func (p *Provider) DisplayName() string { return p.displayName }

// Capabilities declares browse and write. storageapi.Probe fills in the rest
// of Caps from the optional interfaces this type implements.
func (p *Provider) Capabilities() storageapi.Caps {
	return storageapi.Caps{Browse: true, Write: true}
}

// begin starts every method. It fails when the context is already done and
// opens the confinement handle. The context is not checked again: a local
// disk operation cannot be interrupted part way.
func (p *Provider) begin(ctx context.Context) (*os.Root, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return p.store.open()
}

// List returns the entries directly inside relDir.
func (p *Provider) List(ctx context.Context, relDir string) ([]storageapi.FileInfo, error) {
	prefix := storageapi.CleanRelPath(relDir)
	if reserved(prefix) {
		return nil, storageapi.ErrInvalidPath
	}

	root, err := p.begin(ctx)
	if err != nil {
		return nil, err
	}

	defer func() { _ = root.Close() }()

	dir, err := root.Open(name(relDir))
	if err != nil {
		return nil, pathError(err)
	}

	defer func() { _ = dir.Close() }()

	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}

	out := make([]storageapi.FileInfo, 0, len(entries))

	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			// The entry was deleted between the read and the stat.
			continue
		}

		rel := entry.Name()
		if prefix != "" {
			rel = prefix + "/" + rel
		}

		if reserved(rel) {
			continue
		}

		listed := storageapi.FileInfo{
			Name:  entry.Name(),
			Path:  rel,
			IsDir: entry.IsDir(),
			Size:  info.Size(),
			MTime: info.ModTime().Unix(),
			// The entry's own type, so a link to a folder is irregular too.
			Irregular: !entry.Type().IsRegular() && !entry.IsDir(),
		}

		// This costs one extra open and read per file. Without the digest
		// every caller would need a Stat per file.
		if !listed.IsDir {
			listed.SHA256 = readSidecar(root, filepath.FromSlash(rel), info.ModTime())
		}

		out = append(out, listed)
	}

	return out, nil
}

// Stat describes one entry.
func (p *Provider) Stat(ctx context.Context, relPath string) (storageapi.FileInfo, error) {
	clean := storageapi.CleanRelPath(relPath)
	if reserved(clean) {
		return storageapi.FileInfo{}, storageapi.ErrInvalidPath
	}

	root, err := p.begin(ctx)
	if err != nil {
		return storageapi.FileInfo{}, err
	}

	defer func() { _ = root.Close() }()

	target := name(relPath)

	info, err := root.Stat(target)
	if err != nil {
		return storageapi.FileInfo{}, pathError(err)
	}

	described := storageapi.FileInfo{
		Name:  path.Base(clean),
		Path:  clean,
		IsDir: info.IsDir(),
		Size:  info.Size(),
		MTime: info.ModTime().Unix(),
	}

	// An object from CreateLocal, or one an operator copied into the
	// directory, has no sidecar. Its digest is reported as empty.
	if !described.IsDir {
		described.SHA256 = readSidecar(root, target, info.ModTime())
	}

	return described, nil
}

// Open opens an object for reading.
func (p *Provider) Open(ctx context.Context, relPath string) (io.ReadCloser, error) {
	if reserved(storageapi.CleanRelPath(relPath)) {
		return nil, storageapi.ErrInvalidPath
	}

	root, err := p.begin(ctx)
	if err != nil {
		return nil, err
	}

	defer func() { _ = root.Close() }()

	f, err := root.Open(name(relPath))
	if err != nil {
		return nil, pathError(err)
	}

	if err = requireFile(f); err != nil {
		return nil, err
	}

	return f, nil
}

// requireFile rejects a directory. It checks the open handle, not the name,
// because the caller reads from the handle. It closes f on failure.
func requireFile(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()

		return err
	}

	if info.IsDir() {
		_ = f.Close()

		return storageapi.ErrInvalidPath
	}

	return nil
}

// Create starts a write at relPath, creating parent directories as needed.
//
// The bytes go to a temporary file beside the destination, and Commit renames
// it into place. A transfer that fails partway leaves the existing object
// alone. The temporary file is in the destination's directory, so the rename
// stays on one filesystem.
func (p *Provider) Create(ctx context.Context, relPath string) (storageapi.Writer, error) {
	clean := storageapi.CleanRelPath(relPath)
	if err := storageapi.ValidateExtension(path.Base(clean), p.allowed); err != nil {
		return nil, err
	}

	destination := name(relPath)
	if destination == "." || reserved(clean) {
		return nil, storageapi.ErrInvalidPath
	}

	root, err := p.begin(ctx)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(destination)
	if err = root.MkdirAll(dir, dirMode); err != nil {
		_ = root.Close()

		return nil, dirError(err)
	}

	temp, f, err := createTemp(root, dir, filepath.Base(destination))
	if err != nil {
		_ = root.Close()

		return nil, pathError(err)
	}

	// The confinement handle stays open for the life of the writer, so the
	// rename and the sidecar go to the same store as the temporary file.
	// Commit and Abort each close it.
	return &writer{
		root:        root,
		file:        f,
		temp:        temp,
		destination: destination,
		published:   clean,
		digest:      storageapi.NewDigest(),
	}, nil
}

// Delete removes an object, or a directory and everything under it.
func (p *Provider) Delete(ctx context.Context, relPath string) error {
	clean := storageapi.CleanRelPath(relPath)
	if clean == "" || reserved(clean) {
		return storageapi.ErrInvalidPath
	}

	root, err := p.begin(ctx)
	if err != nil {
		return err
	}

	defer func() { _ = root.Close() }()

	target := name(relPath)
	if err = pathError(root.RemoveAll(target)); err != nil {
		return err
	}

	removeSidecar(root, target)

	return nil
}

// DeleteMany attempts every path and reports how many failed. A failure does
// not stop the others.
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

// Rename moves an object or a directory within the store.
func (p *Provider) Rename(ctx context.Context, from, to string) error {
	cleanFrom, cleanTo := storageapi.CleanRelPath(from), storageapi.CleanRelPath(to)
	if cleanFrom == "" || cleanTo == "" || reserved(cleanFrom) || reserved(cleanTo) {
		return storageapi.ErrInvalidPath
	}

	root, err := p.begin(ctx)
	if err != nil {
		return err
	}

	defer func() { _ = root.Close() }()

	source, destination := name(from), name(to)

	// A rename creates a name like an upload does, so the extension
	// allowlist applies. Lstat does not follow a link at the source.
	info, err := root.Lstat(source)
	if err != nil {
		return pathError(err)
	}

	if !info.IsDir() {
		if err = storageapi.ValidateExtension(path.Base(cleanTo), p.allowed); err != nil {
			return err
		}
	}

	if err = root.MkdirAll(filepath.Dir(destination), dirMode); err != nil {
		return dirError(err)
	}

	if err = pathError(root.Rename(source, destination)); err != nil {
		return err
	}

	moveSidecar(root, source, destination)

	return nil
}

// Mkdir creates a directory and its parents.
func (p *Provider) Mkdir(ctx context.Context, relPath string) error {
	clean := storageapi.CleanRelPath(relPath)
	if clean == "" || reserved(clean) {
		return storageapi.ErrInvalidPath
	}

	root, err := p.begin(ctx)
	if err != nil {
		return err
	}

	defer func() { _ = root.Close() }()

	return dirError(root.MkdirAll(name(relPath), dirMode))
}

// OpenLocal resolves a name and opens it in one step. The caller gets the
// open file, so a link planted at that name afterwards has no effect.
func (p *Provider) OpenLocal(ctx context.Context, relPath string) (storageapi.LocalFile, error) {
	clean := storageapi.CleanRelPath(relPath)
	if clean == "" || reserved(clean) {
		return nil, storageapi.ErrInvalidPath
	}

	root, err := p.begin(ctx)
	if err != nil {
		return nil, err
	}

	defer func() { _ = root.Close() }()

	f, err := root.Open(name(relPath))
	if err != nil {
		return nil, pathError(err)
	}

	if err = requireFile(f); err != nil {
		return nil, err
	}

	return f, nil
}

// CreateLocal creates or truncates an object inside the store and returns the
// open handle. A link planted at the destination is refused, not followed.
func (p *Provider) CreateLocal(ctx context.Context, relPath string) (storageapi.LocalWriter, error) {
	clean := storageapi.CleanRelPath(relPath)
	if clean == "" || reserved(clean) {
		return nil, storageapi.ErrInvalidPath
	}

	root, err := p.begin(ctx)
	if err != nil {
		return nil, err
	}

	defer func() { _ = root.Close() }()

	target := name(relPath)
	if err = root.MkdirAll(filepath.Dir(target), dirMode); err != nil {
		return nil, dirError(err)
	}

	// O_TRUNC, not O_EXCL: staging the same object again must overwrite.
	f, err := root.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
	if err != nil {
		return nil, pathError(err)
	}

	// The caller writes the bytes, so the store has no digest for them.
	// Remove the sidecar of an earlier write under the same name.
	removeSidecar(root, target)

	return staged{File: f, name: filepath.Join(p.store.root, target)}, nil
}

// staged makes Name return the absolute path. A file opened through os.Root
// reports only its path inside the root.
type staged struct {
	*os.File

	name string
}

// Name is the absolute path of the object the handle holds open.
func (f staged) Name() string { return f.name }
