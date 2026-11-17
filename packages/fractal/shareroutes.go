package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/prusa3d/fractal-link/files"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// shareListing is the answer of GET /files/shares.
type shareListing struct {
	// Items is every shared folder, sorted by storage and path.
	Items []files.Share `json:"items"`
}

// storageLister is the host's storage.list, as far as the share routes use it.
type storageLister interface {
	List(ctx context.Context, params storageapi.ListParams) (storageapi.Listing, error)
}

// sharesOrRefuse returns the shared folders. Before the settings arrived, or
// with no files block, it answers the request itself and returns nil.
func (t *tree) sharesOrRefuse(w http.ResponseWriter) *files.Shares {
	t.mu.RLock()
	shares, serving := t.shares, t.shadow != nil
	t.mu.RUnlock()

	switch {
	case shares != nil:
		return shares
	case !serving:
		notServingYet(w)
	default:
		problem(w, statusNotFound, FilesOff.Code().String(),
			"This server does not run the file extension",
			"Add a files block to the settings of this link and start it again.")
	}

	return nil
}

// listShares answers GET /files/shares with every shared folder. The host
// takes view_resource on this link before it forwards the request.
func (t *tree) listShares(w http.ResponseWriter, _ *http.Request) {
	shares := t.sharesOrRefuse(w)
	if shares == nil {
		return
	}

	items := shares.List()
	if items == nil {
		items = []files.Share{}
	}

	answer(w, shareListing{Items: items})
}

// changeShare answers PUT, which shares a folder, and DELETE, which unshares
// it. The host takes operate_resource on this link before it forwards either.
func (t *tree) changeShare(w http.ResponseWriter, r *http.Request) {
	shares := t.sharesOrRefuse(w)
	if shares == nil {
		return
	}

	change := shares.Share
	if r.Method == http.MethodDelete {
		change = shares.Unshare
	} else if !t.folderReadable(w, r) {
		return
	}

	err := change(r.PathValue("storage"), r.PathValue("path"))

	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, files.ErrRootShare), errors.Is(err, files.ErrBadShare):
		problem(w, statusBadRequest, ShareInvalid.Code().String(),
			"The address does not name a folder that can be shared",
			"Name a folder inside the storage. The root folder of a storage cannot be shared.")
	default:
		t.logger().WarnContext(r.Context(), "could not write the shared folders", "error", err)
		problem(w, statusInternal, ShareNotSaved.Code().String(),
			"This server could not write the shared folders",
			"Check that the module may write its data directory, then send the request again.")
	}
}

// folderReadable lists the folder as the person who asked. A folder may be
// shared only by someone with read_storage on it.
func (t *tree) folderReadable(w http.ResponseWriter, r *http.Request) bool {
	t.mu.RLock()
	lister := t.lister
	t.mu.RUnlock()

	_, err := lister.List(r.Context(), storageapi.ListParams{
		Principal: routesapi.PrincipalOf(r),
		Storage:   r.PathValue("storage"),
		Path:      r.PathValue("path"),
	})

	switch {
	case err == nil:
		return true
	case errors.Is(err, storageapi.ErrDenied):
		problem(w, statusForbidden, ShareDenied.Code().String(),
			"You may not read the storage that holds this folder",
			"Ask the operator for read_storage on this server, then share the folder again.")
	case errors.Is(err, storageapi.ErrNoFile), errors.Is(err, storageapi.ErrInvalidPath),
		errors.Is(err, storageapi.ErrUnreadable):
		problem(w, statusBadRequest, ShareInvalid.Code().String(),
			"The address does not name a folder that can be shared",
			"Name a storage this server declares and a folder inside it.")
	default:
		t.logger().WarnContext(r.Context(), "could not list the folder to share", "error", err)
		problem(w, statusUnavailable, CallFailed.Code().String(),
			"This server could not check the folder",
			"Send the request again.")
	}

	return false
}

func (t *tree) logger() *slog.Logger {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.log
}
