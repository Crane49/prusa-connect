package sqlstore

import (
	"database/sql"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The pool a database handle is opened with when the operator names no numbers.
// Each value is an upper bound. A deployment that stays under one never notices
// it.
const (
	// DefaultMaxOpen bounds how many queries run at once. Beyond it a caller
	// waits in the pool, where its context can still cancel it. A caller
	// waiting inside the database cannot be cancelled.
	DefaultMaxOpen = 16
	// DefaultMaxIdle is how many of those stay open while nothing is asking.
	// Half the pool: a quiet server keeps enough connections open to answer a
	// burst without dialling, and closes the rest.
	DefaultMaxIdle = 8
	// DefaultMaxLifetime is how long one connection is used before it is
	// replaced. Replacing connections lets a failover, a rotated credential or
	// a restarted proxy reach a long-running server without restarting it.
	DefaultMaxLifetime = 30 * time.Minute
	// DefaultStatementTimeout bounds one statement on PostgreSQL. A query here
	// is answering a request, and no request should hold the database longer
	// than this.
	DefaultStatementTimeout = 30 * time.Second
)

// Pool is how a database handle is bounded: how many connections it may hold,
// how long it keeps one, and how long a single statement may run.
//
// An Open with no pool named applies DefaultPool. A caller that fills one field
// fills them all: to database/sql a zero field means no bound at all.
type Pool struct {
	// MaxOpen is the most connections open at once.
	MaxOpen int
	// MaxIdle is the most kept open while idle.
	MaxIdle int
	// MaxLifetime is how long one connection is used before being replaced.
	MaxLifetime time.Duration
	// StatementTimeout bounds one statement. It reaches the server as a
	// connection setting in the DSN, so it applies to PostgreSQL only. SQLite
	// is bounded by busy_timeout and by the caller's context.
	StatementTimeout time.Duration
	// PageCacheBytes bounds the page cache of one SQLite connection. Zero
	// keeps SQLite's own size. It applies to SQLite only.
	PageCacheBytes int
}

// DefaultPool is the pool a handle opened without one gets.
func DefaultPool() Pool {
	return Pool{
		MaxOpen:          DefaultMaxOpen,
		MaxIdle:          DefaultMaxIdle,
		MaxLifetime:      DefaultMaxLifetime,
		StatementTimeout: DefaultStatementTimeout,
	}
}

// PoolOr is the pool a variadic Open parameter names, or the default when it
// names none. Every Open here takes at most one pool, and this function says
// what an empty parameter means.
func PoolOr(named []Pool) Pool {
	if len(named) == 0 {
		return DefaultPool()
	}

	return named[0]
}

// Apply bounds the handle. A field left at zero keeps database/sql's own
// default, which is no bound, so callers pass a whole pool.
func (p Pool) Apply(db *sql.DB) {
	if p.MaxOpen > 0 {
		db.SetMaxOpenConns(p.MaxOpen)
	}

	if p.MaxIdle > 0 {
		db.SetMaxIdleConns(p.MaxIdle)
	}

	if p.MaxLifetime > 0 {
		db.SetConnMaxLifetime(p.MaxLifetime)
	}
}

// PageCachePragma is the DSN parameter that sets the page cache of every
// connection the pool opens, or an empty string when PageCacheBytes is zero.
// It starts with "&", so a caller appends it to a DSN that already carries a
// parameter.
//
// SQLite reads a negative cache_size as a size in KiB, so the bound holds
// whatever page size the file was made with. A size that is not a whole KiB is
// rounded up.
func (p Pool) PageCachePragma() string {
	if p.PageCacheBytes <= 0 {
		return ""
	}

	kib := (p.PageCacheBytes + kibibyte - 1) / kibibyte

	return "&_pragma=cache_size(-" + strconv.Itoa(kib) + ")"
}

// kibibyte is the unit SQLite reads a negative cache_size in.
const kibibyte = 1024

// WithStatementTimeout puts the timeout into a PostgreSQL DSN, as the
// connection setting of the same name. Every connection the pool opens then
// carries it. There is no other way to reach them all, because database/sql
// opens a new connection without telling the caller.
//
// A DSN that already names the setting is returned unchanged, in either form
// lib/pq accepts.
func WithStatementTimeout(dsn string, timeout time.Duration) string {
	if dsn == "" || timeout <= 0 || strings.Contains(dsn, statementTimeout) {
		return dsn
	}

	milliseconds := strconv.FormatInt(timeout.Milliseconds(), 10)

	if !strings.Contains(dsn, "://") {
		return dsn + " " + statementTimeout + "=" + milliseconds
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		// Unparseable here is unparseable for the driver too, so it is handed
		// over as it was written and the driver reports it.
		return dsn
	}

	query := parsed.Query()
	query.Set(statementTimeout, milliseconds)
	parsed.RawQuery = query.Encode()

	return parsed.String()
}

// statementTimeout is PostgreSQL's own name for the setting. lib/pq forwards it
// from the DSN to the server without reading it.
const statementTimeout = "statement_timeout"
