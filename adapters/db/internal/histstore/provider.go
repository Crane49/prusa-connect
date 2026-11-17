package histstore

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/adapters/db/internal/engine"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// DefaultRawRetention is how long a raw row is kept when the config says
// nothing about retention. `retention: {raw: "0"}` keeps everything.
const DefaultRawRetention = 48 * time.Hour

// sweepInterval is how often expired rows are deleted.
const sweepInterval = 10 * time.Minute

// OpenBudget bounds opening one store and migrating it, so a start that hangs
// fails and the supervisor can retry it.
const OpenBudget = 30 * time.Second

// Settings is what an operator writes under a history instance's config block.
// Both engines share the shape. The host reads none of the keys.
type Settings struct {
	// Path is the database file, for an engine that has one.
	Path string `json:"path"`
	// DSN is the connection string, for an engine that has one.
	DSN string `json:"dsn"`
	// PageCacheBytes bounds the page cache of one SQLite connection. Zero
	// keeps SQLite's default. A Postgres store refuses any other value.
	PageCacheBytes int `json:"page_cache_bytes"`
	// Retention is what this instance keeps.
	Retention struct {
		// Raw is how long a raw row is kept, as a duration string. An absent
		// key means DefaultRawRetention and "0" keeps everything.
		Raw *string `json:"raw"`
	} `json:"retention"`
}

// ReadSettings decodes one instance's config block and its raw retention. An
// unknown key is an error.
func ReadSettings(spec historyapi.ProviderSpec) (Settings, time.Duration, error) {
	settings := Settings{}

	if err := engine.ReadSettings(spec.ID, spec.Config, &settings); err != nil {
		return Settings{}, 0, err
	}

	if err := engine.CheckPageCache(spec.ID, settings.PageCacheBytes); err != nil {
		return Settings{}, 0, err
	}

	keep, err := settings.keep(spec.ID)
	if err != nil {
		return Settings{}, 0, err
	}

	return settings, keep, nil
}

// keep is the declared raw retention, or the default when the key is absent.
func (s Settings) keep(id string) (time.Duration, error) {
	if s.Retention.Raw == nil {
		return DefaultRawRetention, nil
	}

	written := *s.Retention.Raw
	if written == "" || written == "0" {
		return 0, nil
	}

	keep, err := time.ParseDuration(written)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: retention.raw is %q, which is not a duration",
			engine.ErrSettings, id, written)
	}

	if keep < 0 {
		return 0, fmt.Errorf("%w: %s: retention.raw is %q, and a window cannot be negative",
			engine.ErrSettings, id, written)
	}

	return keep, nil
}

// Provider is one declared history instance: the store and its retention
// sweep.
type Provider struct {
	*Store

	log *slog.Logger

	stop   context.CancelFunc
	swept  chan struct{}
	closed sync.Once
}

var _ historyapi.Provider = (*Provider)(nil)

// Start builds the provider over an open handle, migrates the store and starts
// the sweep. It runs inside the supervised start, so a store that fails to
// migrate is retried while the server serves without it. `required: true` on
// the declaration makes that failure stop the boot. On an error db is closed.
func Start(
	ctx context.Context, log *slog.Logger, db *sql.DB, dialect Dialect, keep time.Duration,
) (*Provider, error) {
	store, err := New(db, dialect, WithRawRetention(keep))
	if err != nil {
		_ = db.Close()

		return nil, err
	}

	if err = store.Migrate(ctx); err != nil {
		_ = store.Close()

		return nil, err
	}

	provider := &Provider{Store: store, log: log}

	if keep <= 0 {
		return provider, nil
	}

	sweepCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	provider.stop = cancel
	provider.swept = make(chan struct{})

	go provider.sweep(sweepCtx)

	return provider, nil
}

// sweep deletes expired rows until the provider is closed. A failed sweep is
// logged and the loop continues.
func (p *Provider) sweep(ctx context.Context) {
	defer close(p.swept)

	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		if err := p.Store.Prune(ctx); err != nil && ctx.Err() == nil {
			p.log.WarnContext(ctx, "the history store could not expire its old rows", "error", err.Error())
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Close stops the sweep and closes the handle. It is safe to call more than
// once.
func (p *Provider) Close() error {
	var err error

	p.closed.Do(func() {
		if p.stop != nil {
			p.stop()
			<-p.swept
		}

		err = p.Store.Close()
	})

	return err
}
