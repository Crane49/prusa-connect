package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// The routes that change what a storage holds without carrying a file: remove,
// make a folder and move. Each takes write_storage, and each takes a small JSON
// document or none, so all of them keep the document body bound.

type storagePathBody struct {
	Body struct {
		Path string `json:"path" doc:"The entry the operation acted on."`
	}
}

type createStorageFolderInput struct {
	StorageRef

	Body struct {
		Path string `json:"path" doc:"The folder to create, relative to the storage root." maxLength:"1024"`
	}
}

// deleteStorageEntriesInput is one batch delete. The list is bounded because
// the whole batch runs inside one call budget of the storage, so a longer list
// is sent as several requests.
type deleteStorageEntriesInput struct {
	StorageRef

	Body struct {
		Paths []string `json:"paths" doc:"The entries to remove, each relative to the storage root. A folder is removed with everything under it." minItems:"1" maxItems:"1000" nullable:"false"`
	}
}

type moveStorageEntryInput struct {
	StorageRef

	Body struct {
		From string `json:"from" doc:"The entry to move, relative to the storage root." maxLength:"1024"`
		To   string `json:"to"   doc:"Where it should be, relative to the same storage root." maxLength:"1024"`
	}
}

func registerDeleteObject(api huma.API, stores Storages) {
	register(api, huma.Operation{
		OperationID: "delete-storage-entry",
		Method:      http.MethodDelete,
		Path:        storageFilesPath,
		Summary:     "Remove an entry from a storage",
		Description: "Deletes a file, or a folder and everything under it. Unlike a printer, a storage " +
			"needs no separate call per kind: it is this server's own disk and it knows which it has. " +
			"It takes write_storage.",
		Tags:   []string{"storages"},
		Errors: storageErrors,
	}, func(ctx context.Context, in *requiredPathInput) (*struct{}, error) {
		if err := stores.Delete(ctx, principalFrom(ctx), in.Storage, in.Path); err != nil {
			return nil, err
		}

		return nil, nil //nolint:nilnil // 204: the deletion is the whole answer.
	})
}

// registerDeleteStorageEntries removes several entries in one request. It is a
// POST to a collection of deletions because a DELETE with a body is one that
// proxies and HTTP libraries are free to drop.
func registerDeleteStorageEntries(api huma.API, stores Storages) {
	register(api, huma.Operation{
		OperationID: "delete-storage-entries",
		Method:      http.MethodPost,
		Path:        storageDeletionsPath,
		Summary:     "Remove several entries from a storage",
		Description: "Deletes every file and folder the list names, a folder with everything under it. " +
			"Every path is checked first, and a list holding a path with a .. segment or naming the " +
			"storage root removes nothing. The deletes are not one atomic step: every entry is tried, " +
			"the ones removed stay removed, and when any entry is still there the answer is " +
			"core.storage/DELETE_INCOMPLETE, so list the folder and send what remains. It takes " +
			"write_storage.",
		Tags:   []string{"storages"},
		Errors: storageErrors,
	}, func(ctx context.Context, in *deleteStorageEntriesInput) (*struct{}, error) {
		if err := stores.DeleteMany(ctx, principalFrom(ctx), in.Storage, in.Body.Paths); err != nil {
			return nil, err
		}

		return nil, nil //nolint:nilnil // 204: the deletions are the whole answer.
	})
}

func registerCreateStorageFolder(api huma.API, stores Storages) {
	register(api, huma.Operation{
		OperationID: "create-storage-folder",
		Method:      http.MethodPost,
		Path:        storageFoldersPath,
		Summary:     "Make a folder in a storage",
		Description: "Creates a folder and any parent it needs. It takes write_storage.",
		Tags:        []string{"storages"},
		Errors:      storageErrors,
	}, func(ctx context.Context, in *createStorageFolderInput) (*storagePathBody, error) {
		if err := stores.CreateFolder(ctx, principalFrom(ctx), in.Storage, in.Body.Path); err != nil {
			return nil, err
		}

		out := &storagePathBody{}
		out.Body.Path = in.Body.Path

		return out, nil
	})
}

func registerMoveStorageEntry(api huma.API, stores Storages) {
	register(api, huma.Operation{
		OperationID: "move-storage-entry",
		Method:      http.MethodPost,
		Path:        storageMovesPath,
		Summary:     "Move or rename an entry in a storage",
		Description: "Moves an entry to another path inside the same storage. It does not cross " +
			"storages: two storages share no namespace and no atomic step between them, so moving " +
			"between them is a download and an upload. It takes write_storage.",
		Tags:   []string{"storages"},
		Errors: storageErrors,
	}, func(ctx context.Context, in *moveStorageEntryInput) (*storagePathBody, error) {
		if err := stores.Move(ctx, principalFrom(ctx), in.Storage, in.Body.From, in.Body.To); err != nil {
			return nil, err
		}

		out := &storagePathBody{}
		out.Body.Path = in.Body.To

		return out, nil
	})
}
