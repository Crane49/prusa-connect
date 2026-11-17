package authz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"

	// The engine the graph ships on. It is linked into every build rather than
	// put behind a tag, because a binary that cannot open its authorization
	// graph refuses every subscription and narrows no route, and that is not a
	// state anybody should reach by forgetting a tag. It is pure Go, so
	// CGO_ENABLED=0 still links no cgo.
	_ "modernc.org/sqlite"
)

// sqliteDriver is the name modernc.org/sqlite registers itself under.
const sqliteDriver = "sqlite"

// sqlitePragmas is how this project opens SQLite. foreign_keys is the one that
// must be set: authz_tuple carries a composite foreign key into
// authz_relation_shape and SQLite ignores foreign keys unless the connection
// asked for them, so Migrate refuses a handle without it. Setting it in the DSN
// is the only way to reach every connection the pool will ever open, so the
// string lives here and not at each call site.
//
// _txlock=immediate makes every write transaction take the write lock as it
// begins. Bootstrap and every grant write read the graph before they change it,
// and a deferred transaction that upgrades after another writer has committed
// is refused outright rather than made to wait: busy_timeout does not cover it,
// because retrying a stale snapshot can never succeed. Two processes starting
// against one file leave one of them dead on SQLITE_BUSY, and a deployment that
// fails to start is the cost of a lock this handle takes a few milliseconds
// sooner.
//
// Write-ahead logging is set once by sqlstore.EnableWAL instead of here,
// because it is a property of the file and not of a connection.
const sqlitePragmas = "_pragma=foreign_keys(1)" +
	"&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_txlock=immediate"

// ErrJournalMode reports a graph that will not go into write-ahead logging. WAL
// is what lets a reader run while a writer holds the file, and without it every
// read on a busy deployment queues behind the current write.
var ErrJournalMode = errors.New("authz: the graph is not in write-ahead logging")

// graphDirPerm is what the directory holding the graph is created with. The
// tuples are the whole access-control state of the deployment.
const graphDirPerm = 0o700

// ErrDatabasePath reports a path SQLite cannot be pointed at.
var ErrDatabasePath = errors.New("authz: unusable database path")

// OpenSQLite opens the graph's database at path, creating the file and the
// directory above it when they are not there. A first run therefore starts on
// an empty graph rather than refusing to start: there is nothing to fetch and,
// on an air-gapped install, nowhere to fetch it from.
//
// The caller owns the handle and closes it, which is the same division Open
// documents: authorization writes land in the same transaction discipline as
// the domain writes beside them.
//
// At most one pool may be named, and naming none takes sqlstore.DefaultPool.
// A handle with no bound at all lets a burst open a connection per waiting
// request, and on SQLite that is a burst queueing on the file rather than in
// the pool, where a cancelled request would have left.
func OpenSQLite(ctx context.Context, path string, pool ...sqlstore.Pool) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: the path is empty", ErrDatabasePath)
	}

	// The pragmas ride on a query string, so a path holding the separator would
	// silently become a shorter path with settings nobody asked for.
	if strings.Contains(path, "?") {
		return nil, fmt.Errorf("%w: %q holds a question mark", ErrDatabasePath, path)
	}

	if err := os.MkdirAll(filepath.Dir(path), graphDirPerm); err != nil {
		return nil, fmt.Errorf("create the directory for the authorization graph: %w", err)
	}

	// The file is the whole host database: every tuple and grant, and the
	// credentials and accounts beside them.
	if err := sqlstore.OwnFile(path); err != nil {
		return nil, fmt.Errorf("open the authorization graph: %w", err)
	}

	bounds := sqlstore.PoolOr(pool)

	db, err := sql.Open(sqliteDriver, path+"?"+sqlitePragmas+bounds.PageCachePragma())
	if err != nil {
		return nil, fmt.Errorf("open the authorization graph at %s: %w", path, err)
	}

	bounds.Apply(db)

	if err = sqlstore.EnableWAL(ctx, db, ErrJournalMode); err != nil {
		_ = db.Close() // the failure being returned is the one worth reporting

		return nil, err
	}

	return db, nil
}
