// Package sqlite is the history provider and the record backend on SQLite, in
// a database file of its own.
//
// It is a reference implementation of sdk/historyapi, meant to be copied. A
// deployment that declares no history instance opens no file.
//
// The file is never ATTACHed to the host database. SQLite has one writer per
// database, and sample inserts would block the host's writes.
//
// The retention comes from this adapter's config block. The host deletes
// nothing. A query for an expired window is refused.
package sqlite

import (
	"context"

	"github.com/prusa3d/prusa-connect/adapters/db/internal/engine"
	"github.com/prusa3d/prusa-connect/adapters/db/internal/histstore"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// Type is the `type` of an instance that selects this adapter.
const Type = "sqlite"

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { historyapi.Register(Type, doc(), New) }

// doc is this provider's entry in the published extension reference. Consumes
// lists every event class an instance is fed. Telemetry becomes samples and
// every other class becomes facts.
func doc() extension.Doc {
	return extension.Doc{
		Summary: "Keeps metrics and events in a SQLite database file of its own.",
		Description: "The shipped default history provider, and off until an instance is declared. " +
			"Telemetry is stored as raw samples against the state path it was reported under, and " +
			"state, command and audit events are stored whole. The file is never ATTACHed to the core " +
			"database, because SQLite has one writer per write-ahead log and a sample flood beside " +
			"the authorization graph puts a small graph write at the busy timeout.",
		Version:   "1.0.0",
		Contract:  historyapi.Contract,
		Stability: extension.StabilityExperimental,
		Consumes: []event.Class{
			event.ClassTelemetry, event.ClassState, event.ClassCommand, event.ClassAudit,
		},
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
			{
				Key:     "retention",
				Type:    extension.OptionObject,
				Default: `{"raw": "48h"}`,
				Summary: "What this instance keeps: one key, raw, a duration, and \"0\" keeps everything.",
			},
		},
	}
}

// New opens the file, brings its schema up to date and returns the provider.
func New(spec historyapi.ProviderSpec) (historyapi.Provider, error) {
	settings, keep, err := histstore.ReadSettings(spec)
	if err != nil {
		return nil, err
	}

	// The factory gets no context, so the adapter sets its own deadline. A
	// migration that hangs on a locked file must still end.
	ctx, cancel := context.WithTimeout(context.Background(), histstore.OpenBudget)
	defer cancel()

	db, err := engine.OpenSQLite(ctx, settings.Path, settings.PageCacheBytes)
	if err != nil {
		return nil, err
	}

	return histstore.Start(ctx, spec.Log(), db, histstore.DialectSQLite, keep)
}
