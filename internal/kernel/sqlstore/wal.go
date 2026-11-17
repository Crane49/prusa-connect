// Package sqlstore holds what every SQL store in this project does the same
// way: the journal mode a SQLite file is put into before it serves a query, and
// what a build with no PostgreSQL driver in it answers.
//
// It is in kernel because that is the only group both callers can reach.
// internal/authz is a domain and internal/platform/history is an outbound
// adapter, and neither may import the other. Both may import kernel.
//
// The package holds the mechanism and not the vocabulary. Each store keeps its
// own sentinel and passes it in, so a refusal names the store an operator
// configured and not this package.
package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// walMode is what PRAGMA journal_mode answers on a database in write-ahead
// logging. SQLite reports the mode in lower case.
const walMode = "wal"

// walAttempts bounds how long EnableWAL waits for another process that is
// setting the same mode on the same file.
const walAttempts = 50

// walRetryPause is the gap between attempts, well under the time a journal mode
// change holds the file.
const walRetryPause = 10 * time.Millisecond

// EnableWAL puts the file behind db into write-ahead logging, once per handle
// and not once per connection. It reports notInWAL, wrapped with what the
// engine last said, when the mode will not take.
//
// journal_mode is a property of the database file and survives every close, so
// it does not belong in the DSN. Changing it takes a brief exclusive lock that
// busy_timeout does not cover, and modernc.org/sqlite applies DSN pragmas as
// each connection opens: several connections opening together against one file
// then fail on SQLITE_BUSY, before the caller's own transaction discipline
// runs. Reading the mode first means a database already in WAL takes no lock.
//
// The retry covers another process setting the same mode on the same file.
// Whichever one wins, the file ends in WAL and the next read sees it.
func EnableWAL(ctx context.Context, db *sql.DB, notInWAL error) error {
	var last error

	for attempt := range walAttempts {
		var mode string

		if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
			last = err
		} else if strings.EqualFold(mode, walMode) {
			return nil
		}

		if err := db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode); err != nil {
			last = err
		} else if strings.EqualFold(mode, walMode) {
			return nil
		}

		if attempt+1 == walAttempts {
			break
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", notInWAL, ctx.Err())
		case <-time.After(walRetryPause):
		}
	}

	return fmt.Errorf("%w after %d attempts: %w", notInWAL, walAttempts, last)
}
