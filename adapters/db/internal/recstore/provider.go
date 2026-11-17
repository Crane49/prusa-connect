package recstore

import (
	"context"
	"database/sql"
	"time"

	"github.com/prusa3d/prusa-connect/adapters/db/internal/engine"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// OpenBudget bounds opening one store and migrating it, so a start that hangs
// fails and the supervisor can retry it.
const OpenBudget = 30 * time.Second

// Settings is what an operator writes under a record instance's config block.
// The host reads none of the keys.
type Settings struct {
	// Path is the database file, for an engine that has one.
	Path string `json:"path"`
	// DSN is the connection string, for an engine that has one.
	DSN string `json:"dsn"`
	// PageCacheBytes bounds the page cache of one SQLite connection. Zero
	// keeps SQLite's default. A Postgres store refuses any other value.
	PageCacheBytes int `json:"page_cache_bytes"`
}

// ReadSettings decodes one instance's config block.
func ReadSettings(spec stateapi.ProviderSpec) (Settings, error) {
	settings := Settings{}
	if err := engine.ReadSettings(spec.ID, spec.Config, &settings); err != nil {
		return Settings{}, err
	}

	if err := engine.CheckPageCache(spec.ID, settings.PageCacheBytes); err != nil {
		return Settings{}, err
	}

	return settings, nil
}

// Start builds the backend over an open handle and migrates its schema. It
// runs inside the supervised start, so a store that fails to migrate is
// retried while the server serves without it. `required: true` on the
// declaration makes that failure stop the boot. On an error db is closed.
func Start(ctx context.Context, db *sql.DB, dialect Dialect) (stateapi.Backend, error) {
	store, err := New(db, dialect)
	if err != nil {
		_ = db.Close()

		return nil, err
	}

	backend := Backend{Store: store}

	if err = store.Migrate(ctx); err != nil {
		_ = backend.Close()

		return nil, err
	}

	return backend, nil
}
