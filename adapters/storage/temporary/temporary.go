// Package temporary is the staging store: the disk-backed storage a file is
// put in on its way to a printer. The system is free to clean it.
//
// It is a reference implementation of sdk/storageapi. The implementation is
// adapters/storage/diskstore, the same as the local adapter. It is a separate
// adapter so that an operator addresses it as `temporary`.
package temporary

import (
	"github.com/prusa3d/prusa-connect/adapters/storage/diskstore"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// Type is the `type` of a storage resource that selects this adapter.
const Type = "temporary"

// DefaultDisplayName is the label the UI shows when the operator names none.
const DefaultDisplayName = "Temporary"

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { storageapi.Register(Type, Doc(), New) }

// Doc is this store's entry in the published extension reference. It is the
// diskstore entry with a sentence about staging added.
func Doc() extension.Doc {
	described := diskstore.Doc(Type, DefaultDisplayName)
	described.Summary = "Keeps objects in a directory on the server's own disk that the system is free to clean."
	described.Description = "Staging storage: a file lands here on its way to a printer and nothing outside " +
		"this server should expect it to survive. " + described.Description

	return described
}

// New builds a staging provider from its spec.
func New(spec storageapi.ProviderSpec) (storageapi.Provider, error) {
	return diskstore.Open(spec, DefaultDisplayName)
}
