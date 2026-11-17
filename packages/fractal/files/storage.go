package files

import (
	"context"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// Storage is what the module reads the storages of this server through. The
// host serves it, and *storageapi.Client implements it.
type Storage interface {
	List(ctx context.Context, p storageapi.ListParams) (storageapi.Listing, error)
	Stat(ctx context.Context, p storageapi.StatParams) (storageapi.FileInfo, error)
	Read(ctx context.Context, p storageapi.ReadParams) (storageapi.Chunk, error)
}

var _ Storage = (*storageapi.Client)(nil)
