package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
)

// databaseWait bounds how long a starting server waits for a PostgreSQL server
// that does not answer yet. It stays under the 60 seconds the shipped
// Kubernetes startupProbe gives the process to bind, so a database that never
// comes ends as this server's own error and not as a killed pod.
const databaseWait = 45 * time.Second

// databaseBackoffCap is the longest pause between two pings, so a database
// that comes up late is noticed within a few seconds.
const databaseBackoffCap = 5 * time.Second

// databaseBackoff is the ladder between two pings of a database that has not
// answered yet.
func databaseBackoff() supervisor.BackoffPolicy {
	return supervisor.BackoffPolicy{Base: time.Second, Cap: databaseBackoffCap}
}

// awaitDatabase pings the host database until it answers, for a server started
// together with its database by compose, Kubernetes or systemd. Exiting on the
// first refused connection would turn every such start into a crash loop, and
// a binary started by hand has no supervisor to restart it at all.
//
// Only reachability is waited for. A database that answers and then refuses
// the schema is the migration's error, reported at once.
func awaitDatabase(
	ctx context.Context,
	ping func(context.Context) error,
	where string,
	logger *slog.Logger,
	backoff supervisor.BackoffPolicy,
	limit time.Duration,
) error {
	waiting, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	for attempt := 1; ; attempt++ {
		err := ping(waiting)
		if err == nil {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		logger.WarnContext(ctx, "waiting for the database to accept connections",
			"at", where, "attempt", attempt, "error", err.Error())

		if !supervisor.SleepFor(waiting, backoff.Delay(attempt)) {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			return fmt.Errorf("reach the host database at %s within %s: %w", where, limit, err)
		}
	}
}
