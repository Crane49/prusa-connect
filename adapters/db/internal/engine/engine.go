// Package engine is the SQL plumbing shared by the adapters under adapters/db:
// opening SQLite and PostgreSQL, migrations, and the placeholder rewrite that
// lets one statement run on both engines.
//
// An adapter compiles against sdk/ and the standard library only, so it cannot
// import the host's own SQL packages.
package engine

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// walReadyAttempts is how many times WantWAL tries. The only writer that can
// race it is a previous process that has not finished closing.
const walReadyAttempts = 8

// walReadyPause is the gap between attempts.
const walReadyPause = 20 * time.Millisecond

// WantWAL puts the file behind db into write-ahead logging.
//
// journal_mode belongs to the file, so it is set once per handle and not in
// the DSN. modernc.org/sqlite applies DSN pragmas on every new connection, and
// several connections changing the mode at once fail with SQLITE_BUSY. The
// mode is read first, so a file already in WAL takes no exclusive lock.
func WantWAL(ctx context.Context, db *sql.DB, notInWAL error) error {
	var last error

	for attempt := range walReadyAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return errors.Join(notInWAL, ctx.Err())
			case <-time.After(walReadyPause):
			}
		}

		if inWAL(ctx, db, "PRAGMA journal_mode", &last) || inWAL(ctx, db, "PRAGMA journal_mode = WAL", &last) {
			return nil
		}
	}

	return errors.Join(notInWAL, last)
}

// inWAL runs one pragma and reports whether the answer is write-ahead logging.
// A failure is kept in last for the caller's error.
func inWAL(ctx context.Context, db *sql.DB, pragma string, last *error) bool {
	var mode string

	if err := db.QueryRowContext(ctx, pragma).Scan(&mode); err != nil {
		*last = err

		return false
	}

	return strings.EqualFold(mode, "wal")
}

// Numbered rewrites a statement's '?' placeholders as $1, $2 and so on, for
// PostgreSQL. It splits on every '?', so a statement must contain no literal
// '?'.
func Numbered(query string) string {
	pieces := strings.Split(query, "?")
	if len(pieces) == 1 {
		return query
	}

	var out strings.Builder

	out.Grow(len(query) + len(pieces)*4)

	for at, piece := range pieces[:len(pieces)-1] {
		out.WriteString(piece)
		out.WriteByte('$')
		out.WriteString(strconv.Itoa(at + 1))
	}

	out.WriteString(pieces[len(pieces)-1])

	return out.String()
}

// ErrSettings reports a config block one of these adapters cannot read.
var ErrSettings = errors.New("engine: invalid settings")

// ReadSettings decodes one instance's config block into the adapter's
// settings. An unknown key is an error, so a typo does not leave a value unset
// without notice.
func ReadSettings(id string, raw json.RawMessage, into any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrSettings, id, err)
	}

	return nil
}
