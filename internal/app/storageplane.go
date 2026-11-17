package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

var _ httpapi.Storages = storagePlane{}

// storagePlane is the server's own storage as a transport sees it: the
// authorization decision, then the supervised call.
//
// The decision always comes first, so a refused caller learns nothing about
// what a storage holds. The resource store takes the same order
// (internal/app/resource/access.go).
type storagePlane struct {
	graph  *authz.Store
	stores stores

	// previews holds what was read out of the files a caller already asked
	// about, so a page that shows a folder does not reopen every file each time
	// it refreshes. It is in memory and never on disk.
	previews *previewCache

	// incoming writes the temporary copies. A plane built without one writes
	// them all the same and deletes none.
	incoming *incomingCopies

	// analysis asks the module that reads a file's type before the core
	// reads the file itself. A plane built without one reads every file
	// itself.
	analysis *fileAnalysis
}

// refuseClimb refuses a path carrying a ".." segment, in either spelling of a
// separator.
//
// The provider does not refuse one. storageapi.CleanRelPath normalises "\" to
// "/" and then resolves "..\..\etc\passwd" to "etc/passwd" inside the store.
// Nothing escapes, but the caller silently gets a different path from the one
// it asked for, and a refusal is better than that. This check reads a path
// the way the provider does: a split on "/" alone would let the backslash
// spelling through.
func refuseClimb(instance, name string) error {
	for segment := range strings.SplitSeq(strings.ReplaceAll(name, `\`, "/"), "/") {
		if segment != ".." {
			continue
		}

		return fault.StorageInvalidPath.
			New("the path leaves the storage it names").
			With(fault.StringParam("storage_id", instance), fault.StringParam("path", name)).
			Wrap(storageapi.ErrInvalidPath)
	}

	return nil
}

// permit refuses the caller before any storage is touched.
//
// The two actions a storage's contents take are separate from the three that
// cover an instance's declaration. Configuring a storage is permission to say
// where it points. Reading one is permission to see what somebody put in it.
// Each route below names the action it takes, so a reader and internal/arch's
// reachability gate see the same answer.
//
// A grant is held on the storage kind and not on one instance, so these are
// all-or-nothing across a deployment's storages. A per-instance boundary would
// be a change to the graph and not to this file.
func (p storagePlane) permit(ctx context.Context, principal string, action authz.Action) error {
	return permitStorageContents(ctx, p.graph, principal, action)
}

// permitStorageContents is the one check a storage's contents take, wherever
// they are reached from.
//
// It is a function and not a method because the storage routes are not the only
// way into a storage's bytes. Putting a file on a printer reads one too, and
// that path is a printer operation on a different plane. Both call this one
// check.
func permitStorageContents(
	ctx context.Context, graph *authz.Store, principal string, action authz.Action,
) error {
	who, err := authz.ParseSubject(principal)
	if err != nil {
		return fmt.Errorf("read the caller's principal: %w", err)
	}

	object, err := authz.NewObject(authz.TypeResourceKind, ref.KindStorage.String())
	if err != nil {
		return fmt.Errorf("name the storage kind: %w", err)
	}

	// Require and not Can, so the refusal is the graph's own and reads the
	// same as every other one. The object it names is the storage kind, where
	// the grant is held.
	if requireErr := graph.Require(ctx, who, action, object); requireErr != nil {
		return requireErr //nolint:wrapcheck // the graph's refusal is what the caller is owed.
	}

	return nil
}

// List returns the storages that are declared and running.
func (p storagePlane) List(ctx context.Context, principal string) ([]httpapi.Storage, error) {
	if err := p.permit(ctx, principal, authz.ActionReadStorage); err != nil {
		return nil, err
	}

	names := p.stores.Serving()
	slices.Sort(names)

	out := make([]httpapi.Storage, 0, len(names))

	for _, name := range names {
		caps, err := p.stores.capabilitiesOf(ctx, name)
		if err != nil {
			// A storage that stopped between Serving and this call is left out,
			// because every operation on it would fail.
			continue
		}

		label, err := p.stores.displayNameOf(ctx, name)
		if err != nil {
			label = name
		}

		allowed, err := p.stores.allowedExtensionsOf(ctx, name)
		if err != nil {
			continue
		}

		out = append(out, httpapi.Storage{
			Name: name, DisplayName: label,
			Browse: caps.Browse, Write: caps.Write,
			Move: caps.Rename, Ranges: caps.Range, DownloadURL: caps.DownloadURL,
			AllowedExtensions: allowed,
		})
	}

	return out, nil
}

// Files returns one directory of a storage.
func (p storagePlane) Files(
	ctx context.Context, principal string, storage string, path string,
) ([]httpapi.StorageEntry, error) {
	if err := p.permit(ctx, principal, authz.ActionReadStorage); err != nil {
		return nil, err
	}

	if err := refuseClimb(storage, path); err != nil {
		return nil, err
	}

	entries, err := p.stores.List(ctx, storage, path)
	if err != nil {
		return nil, err
	}

	out := make([]httpapi.StorageEntry, 0, len(entries))
	for _, entry := range entries {
		// The temporary copies are files on their way to a printer, and not
		// files anybody keeps here.
		if entry.IsDir && isIncoming(entry.Path) {
			continue
		}

		rendered := entryOf(entry)
		// A folder row states how many entries are inside it, and that needs
		// a listing of the folder.
		// ponytail: one readdir per folder on the page, so a directory of a
		// hundred folders is a hundred readdirs. A count on storageapi.FileInfo
		// would avoid it, and every module author would then have to fill it.
		if entry.IsDir {
			held, listErr := p.stores.List(ctx, storage, entry.Path)
			if listErr == nil {
				count := len(held)
				rendered.Entries = &count
			}
		}

		out = append(out, rendered)
	}

	return out, nil
}

// Stat describes one entry. For a file a slicer wrote it opens the file and
// reads a bounded part of it, which is where the print details come from.
func (p storagePlane) Stat(
	ctx context.Context, principal string, storage string, path string,
) (httpapi.StorageEntry, error) {
	if err := p.permit(ctx, principal, authz.ActionReadStorage); err != nil {
		return httpapi.StorageEntry{}, err
	}

	if err := refuseClimb(storage, path); err != nil {
		return httpapi.StorageEntry{}, err
	}

	info, err := p.stores.Stat(ctx, storage, path)
	if err != nil {
		return httpapi.StorageEntry{}, err
	}

	entry := entryOf(info)
	found, analysis := p.preview(ctx, storage, info, analysisWaitOf(ctx, analysisDetailWait))
	entry.Print = detailsOf(found)
	entry.Analysis = analysis

	return entry, nil
}

// Open returns one file for reading, and what a download needs to describe it.
func (p storagePlane) Open(
	ctx context.Context, principal string, storage string, path string,
) (httpapi.Object, error) {
	if err := p.permit(ctx, principal, authz.ActionReadStorage); err != nil {
		return httpapi.Object{}, err
	}

	if err := refuseClimb(storage, path); err != nil {
		return httpapi.Object{}, err
	}

	// Stat first: a directory has no bytes to serve, and a failed read would
	// show that only after the response has started.
	info, err := p.stores.Stat(ctx, storage, path)
	if err != nil {
		return httpapi.Object{}, err
	}

	if info.IsDir {
		return httpapi.Object{}, stagingFault(storage, path, "open",
			fmt.Errorf("%w: %q names a directory", ErrStorageCannotRead, path))
	}

	file, err := p.stores.Open(ctx, storage, path)
	if err != nil {
		return httpapi.Object{}, err
	}

	return httpapi.Object{
		Name:     info.Name,
		Modified: time.Unix(info.MTime, 0),
		Content:  file,
	}, nil
}

// DownloadURL returns a URL a client can fetch one file from directly, when
// the storage hands one out. ok is false for a storage that does not, and the
// caller then reads the file through Open.
//
// It is checked the same way Open is, permission first, so the URL goes only
// to a caller that may read the file. A folder has no URL.
func (p storagePlane) DownloadURL(
	ctx context.Context, principal string, storage string, path string,
) (string, bool, error) {
	if err := p.permit(ctx, principal, authz.ActionReadStorage); err != nil {
		return "", false, err
	}

	if err := refuseClimb(storage, path); err != nil {
		return "", false, err
	}

	// The capability is read first because it costs no I/O, so a download from
	// a storage that presigns nothing pays no extra stat.
	caps, err := p.stores.capabilitiesOf(ctx, storage)
	if err != nil {
		return "", false, err
	}

	if !caps.DownloadURL {
		return "", false, nil
	}

	info, err := p.stores.Stat(ctx, storage, path)
	if err != nil {
		return "", false, err
	}

	if info.IsDir {
		return "", false, nil
	}

	return p.stores.downloadURL(ctx, storage, path)
}

// Save writes a stream into a storage.
func (p storagePlane) Save(
	ctx context.Context, principal string, storage string, path string, body io.Reader,
) (httpapi.SaveResult, error) {
	if err := p.permit(ctx, principal, authz.ActionWriteStorage); err != nil {
		return httpapi.SaveResult{}, err
	}

	if err := refuseClimb(storage, path); err != nil {
		return httpapi.SaveResult{}, err
	}

	if err := refuseIncoming(storage, path); err != nil {
		return httpapi.SaveResult{}, err
	}

	written, err := p.stores.Save(ctx, storage, path, body)
	if err != nil {
		return httpapi.SaveResult{}, err
	}

	return httpapi.SaveResult{Path: path, Size: written.Size, SHA256: written.SHA256}, nil
}

// SaveTemporary writes a stream as a temporary copy: a file on its way to a
// printer that this server deletes once it is sent. See incomingCopies.
func (p storagePlane) SaveTemporary(
	ctx context.Context, principal string, storage string, name string, body io.Reader,
) (httpapi.SaveResult, error) {
	if err := p.permit(ctx, principal, authz.ActionWriteStorage); err != nil {
		return httpapi.SaveResult{}, err
	}

	if err := refuseClimb(storage, name); err != nil {
		return httpapi.SaveResult{}, err
	}

	copies := p.incoming
	if copies == nil {
		copies = newIncomingCopies(p.stores, slog.New(slog.DiscardHandler))
	}

	return copies.save(ctx, storage, name, body)
}

// Delete removes one entry.
func (p storagePlane) Delete(ctx context.Context, principal string, storage string, path string) error {
	if err := p.permit(ctx, principal, authz.ActionWriteStorage); err != nil {
		return err
	}

	if err := refuseClimb(storage, path); err != nil {
		return err
	}

	return p.stores.Delete(ctx, storage, path)
}

// DeleteMany removes several entries in one call.
//
// Every path is checked before any is removed, so a batch carrying one path
// that climbs or names the root removes nothing. The root is refused here
// because a batch has no required-path check at the transport, and a provider
// told to remove "" is told to empty the whole storage.
func (p storagePlane) DeleteMany(ctx context.Context, principal string, storage string, paths []string) error {
	if err := p.permit(ctx, principal, authz.ActionWriteStorage); err != nil {
		return err
	}

	for _, path := range paths {
		if err := refuseClimb(storage, path); err != nil {
			return err
		}

		if storageapi.CleanRelPath(path) == "" {
			return fault.StorageInvalidPath.
				New("a delete names the storage root").
				With(fault.StringParam("storage_id", storage), fault.StringParam("path", path)).
				Wrap(storageapi.ErrInvalidPath)
		}
	}

	if len(paths) == 0 {
		return nil
	}

	return p.stores.DeleteMany(ctx, storage, paths)
}

// CreateFolder makes a folder.
func (p storagePlane) CreateFolder(
	ctx context.Context, principal string, storage string, path string,
) error {
	if err := p.permit(ctx, principal, authz.ActionWriteStorage); err != nil {
		return err
	}

	if err := refuseClimb(storage, path); err != nil {
		return err
	}

	if err := refuseIncoming(storage, path); err != nil {
		return err
	}

	return p.stores.CreateFolder(ctx, storage, path)
}

// Move renames an entry inside one storage.
func (p storagePlane) Move(
	ctx context.Context, principal string, storage string, from string, to string,
) error {
	if err := p.permit(ctx, principal, authz.ActionWriteStorage); err != nil {
		return err
	}

	if err := refuseClimb(storage, from); err != nil {
		return err
	}

	if err := refuseClimb(storage, to); err != nil {
		return err
	}

	if err := refuseIncoming(storage, from); err != nil {
		return err
	}

	if err := refuseIncoming(storage, to); err != nil {
		return err
	}

	return p.stores.Move(ctx, storage, from, to)
}

// entryOf renders one entry. The provider's own path is already relative to its
// root and "/"-separated, which is the namespace every path here is in.
func entryOf(entry storageapi.FileInfo) httpapi.StorageEntry {
	return httpapi.StorageEntry{
		Name:      entry.Name,
		Path:      entry.Path,
		Folder:    entry.IsDir,
		Size:      entry.Size,
		Modified:  time.Unix(entry.MTime, 0).UTC(),
		SHA256:    entry.SHA256,
		Irregular: entry.Irregular,
	}
}
