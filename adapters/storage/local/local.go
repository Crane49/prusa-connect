// Package local is a storage provider keeping objects in a directory on the
// server's own disk.
//
// It only registers the type. The implementation is
// adapters/storage/diskstore.
package local

import (
	"github.com/prusa3d/prusa-connect/adapters/storage/diskstore"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// Type is the `type` of a storage resource that selects this adapter.
const Type = "local"

// DefaultDisplayName is the label the UI shows when the operator names none.
const DefaultDisplayName = "Local disk"

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { storageapi.Register(Type, Doc(Type, DefaultDisplayName), New) }

// Doc is the entry of a disk-backed store in the published extension
// reference.
func Doc(kind, label string) extension.Doc { return diskstore.Doc(kind, label) }

// New builds a Provider from its spec.
func New(spec storageapi.ProviderSpec) (storageapi.Provider, error) {
	return diskstore.Open(spec, DefaultDisplayName)
}
