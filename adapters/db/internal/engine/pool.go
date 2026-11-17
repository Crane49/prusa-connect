package engine

import (
	"database/sql"
	"strconv"
	"strings"
	"time"
)

// The pool bounds of every handle this package opens. They are constants,
// because an adapter does not read the host's database settings.
const (
	// DefaultMaxOpen bounds how many queries run at once. Above it a caller
	// waits in the pool, where its context can still cancel it.
	DefaultMaxOpen = 8
	// DefaultMaxIdle is how many connections stay open while idle.
	DefaultMaxIdle = 4
	// DefaultMaxLifetime is how long one connection is used before it is
	// replaced, so a failover or a rotated credential takes effect.
	DefaultMaxLifetime = 30 * time.Minute
	// DefaultStatementTimeout bounds one statement on PostgreSQL.
	DefaultStatementTimeout = 30 * time.Second
)

// statementTimeout is PostgreSQL's name for the setting. lib/pq forwards it
// from the DSN to the server.
const statementTimeout = "statement_timeout"

// bindPool bounds the handle, so a flood of writes waits in the pool and does
// not open a connection each.
func bindPool(db *sql.DB) {
	db.SetMaxOpenConns(DefaultMaxOpen)
	db.SetMaxIdleConns(DefaultMaxIdle)
	db.SetConnMaxLifetime(DefaultMaxLifetime)
}

// withStatementTimeout adds the timeout to a PostgreSQL DSN, so every
// connection the pool opens gets it. It appends text and does not re-encode
// the DSN. A DSN that already names the setting is returned unchanged.
func withStatementTimeout(dsn string) string {
	if dsn == "" || strings.Contains(dsn, statementTimeout) {
		return dsn
	}

	setting := statementTimeout + "=" + strconv.FormatInt(DefaultStatementTimeout.Milliseconds(), 10)

	switch {
	case !strings.Contains(dsn, "://"):
		return dsn + " " + setting
	case strings.Contains(dsn, "?"):
		return dsn + "&" + setting
	default:
		return dsn + "?" + setting
	}
}
