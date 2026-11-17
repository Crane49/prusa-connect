package sqlite

import (
	"context"

	"github.com/prusa3d/prusa-connect/adapters/db/internal/engine"
	"github.com/prusa3d/prusa-connect/adapters/db/internal/recstore"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// The record backend of sdk/stateapi on SQLite. It is registered separately
// from the history provider. A deployment may declare one, the other, or both,
// on the same file or on separate files.

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { stateapi.Register(Type, recordDoc(), NewRecords) }

// recordDoc is this backend's entry in the published extension reference.
func recordDoc() extension.Doc {
	return extension.Doc{
		Summary: "Keeps each module's key/value namespace in a SQLite database file of its own.",
		Description: "The record backend behind sdk/stateapi: a namespace per installed module, " +
			"six calls over an opaque value, a usage call, and limits the host checks on its own side. A module " +
			"is handed the same interface whichever backend a deployment declared, so it cannot " +
			"tell one from another. The file is the instance's own and holds no core table.",
		Version:   "1.0.0",
		Contract:  stateapi.Contract,
		Stability: extension.StabilityExperimental,
		Options: []extension.Option{
			{
				Key:      "path",
				Type:     extension.OptionString,
				Required: true,
				Summary:  "The database file this instance owns. It is created if it is not there.",
			},
			{
				Key:     "page_cache_bytes",
				Type:    extension.OptionInt,
				Default: "0",
				Summary: "How many bytes of database pages one connection keeps in memory. 0 keeps SQLite's own size of 2000 KiB.",
			},
		},
	}
}

// NewRecords opens the file, brings the schema up to date and returns the
// backend.
func NewRecords(spec stateapi.ProviderSpec) (stateapi.Backend, error) {
	settings, err := recstore.ReadSettings(spec)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), recstore.OpenBudget)
	defer cancel()

	db, err := engine.OpenSQLite(ctx, settings.Path, settings.PageCacheBytes)
	if err != nil {
		return nil, err
	}

	return recstore.Start(ctx, db, recstore.DialectSQLite)
}
