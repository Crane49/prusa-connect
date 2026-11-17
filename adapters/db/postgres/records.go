package postgres

import (
	"context"

	"github.com/prusa3d/prusa-connect/adapters/db/internal/engine"
	"github.com/prusa3d/prusa-connect/adapters/db/internal/recstore"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// The record backend of sdk/stateapi on PostgreSQL.

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { stateapi.Register(Type, recordDoc(), NewRecords) }

// recordDoc is this backend's entry in the published extension reference.
func recordDoc() extension.Doc {
	return extension.Doc{
		Summary: "Keeps each module's key/value namespace in a PostgreSQL database.",
		Description: "The record backend behind sdk/stateapi on the server engine. The tables and the " +
			"migration ledger are this instance's own inside the database its dsn names, and it " +
			"touches no core schema. The driver is behind the postgres build tag this adapter " +
			"declares in adapters/tags.json.",
		Version:   "1.0.0",
		Contract:  stateapi.Contract,
		Stability: extension.StabilityExperimental,
		Options: []extension.Option{
			{
				Key:      "dsn",
				Type:     extension.OptionSecret,
				Required: true,
				Summary:  "The connection string this instance opens its store on.",
			},
		},
	}
}

// NewRecords opens the database, brings the schema up to date and returns the
// backend.
func NewRecords(spec stateapi.ProviderSpec) (stateapi.Backend, error) {
	settings, err := recstore.ReadSettings(spec)
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

	ctx, cancel := context.WithTimeout(context.Background(), recstore.OpenBudget)
	defer cancel()

	return recstore.Start(ctx, db, recstore.DialectPostgres)
}
