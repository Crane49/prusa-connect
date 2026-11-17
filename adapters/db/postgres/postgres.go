// Package postgres is the history provider and the record backend on
// PostgreSQL, in a database the deployment already runs.
//
// The driver is behind the postgres build tag, declared in adapters/tags.json.
// A build without the tag refuses at open.
//
// The store has its own tables and its own migration ledger. It does not
// touch the host's schema. Nothing is dialled at open, so a database that is
// down fails inside the supervised start, which retries.
package postgres

import (
	"context"

	"github.com/prusa3d/prusa-connect/adapters/db/internal/engine"
	"github.com/prusa3d/prusa-connect/adapters/db/internal/histstore"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// Type is the `type` of an instance that selects this adapter.
const Type = "postgres"

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { historyapi.Register(Type, doc(), New) }

// doc is this provider's entry in the published extension reference.
func doc() extension.Doc {
	return extension.Doc{
		Summary: "Keeps metrics and events in a PostgreSQL database the deployment already runs.",
		Description: "The history provider for a deployment that has a server engine. The tables and " +
			"the migration ledger are this store's own inside that database, and the single-writer " +
			"contention that gives the SQLite adapter a file of its own does not arise here. The " +
			"driver is behind the postgres build tag, which this adapter declares in " +
			"adapters/tags.json; a build without it refuses at open rather than at the first query.",
		Version:   "1.0.0",
		Contract:  historyapi.Contract,
		Stability: extension.StabilityExperimental,
		Consumes: []event.Class{
			event.ClassTelemetry, event.ClassState, event.ClassCommand, event.ClassAudit,
		},
		Options: []extension.Option{
			{
				Key:      "dsn",
				Type:     extension.OptionSecret,
				Required: true,
				Summary:  "The connection string this instance opens its store on.",
			},
			{
				Key:     "retention",
				Type:    extension.OptionObject,
				Default: `{"raw": "48h"}`,
				Summary: "What this instance keeps: one key, raw, a duration, and \"0\" keeps everything.",
			},
		},
	}
}

// New opens the database, brings the store's schema up to date and returns the
// provider.
func New(spec historyapi.ProviderSpec) (historyapi.Provider, error) {
	settings, keep, err := histstore.ReadSettings(spec)
	if err != nil {
		return nil, err
	}

	if err = engine.RefusePageCacheOnPostgres(spec.ID, settings.PageCacheBytes); err != nil {
		return nil, err
	}

	db, err := engine.OpenPostgres(settings.DSN)
	if err != nil {
		return nil, err
	}

	// The budget bounds the migration, the first statement that reaches the
	// server.
	ctx, cancel := context.WithTimeout(context.Background(), histstore.OpenBudget)
	defer cancel()

	return histstore.Start(ctx, spec.Log(), db, histstore.DialectPostgres, keep)
}
