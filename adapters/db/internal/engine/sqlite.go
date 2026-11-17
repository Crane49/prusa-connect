package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	// Pure Go, so it links in every build, also with CGO_ENABLED=0.
	_ "modernc.org/sqlite"
)

// sqliteDriver is the name modernc.org/sqlite registers itself under.
const sqliteDriver = "sqlite"

// SQLitePragmas is how the store opens its file.
//
// With synchronous=NORMAL in WAL mode a committed transaction can roll back
// after a power failure (https://www.sqlite.org/pragma.html#pragma_synchronous).
// That is acceptable for a sample. SQLite ignores foreign keys unless the
// connection turns them on. journal_mode belongs to the file, so WantWAL sets it.
const SQLitePragmas = "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"

// pageCachePragma is the DSN parameter that bounds the page cache of every
// connection, or "" for zero. A negative cache_size is a size in KiB.
func pageCachePragma(pageCacheBytes int) string {
	if pageCacheBytes <= 0 {
		return ""
	}

	kib := (pageCacheBytes + kibibyte - 1) / kibibyte

	return "&_pragma=cache_size(-" + strconv.Itoa(kib) + ")"
}

// kibibyte is the smallest page cache CheckPageCache accepts.
const kibibyte = 1024

// CheckPageCache refuses a page_cache_bytes that is negative or under one KiB.
// A value under one KiB is probably written in KiB, and the option is bytes.
func CheckPageCache(id string, pageCacheBytes int) error {
	if pageCacheBytes < 0 || (pageCacheBytes > 0 && pageCacheBytes < kibibyte) {
		return fmt.Errorf("%w: %s: page_cache_bytes is %d, expected 0 or a byte count of at least %d",
			ErrSettings, id, pageCacheBytes, kibibyte)
	}

	return nil
}

// RefusePageCacheOnPostgres refuses page_cache_bytes on a Postgres store,
// where the value would have no effect.
func RefusePageCacheOnPostgres(id string, pageCacheBytes int) error {
	if pageCacheBytes != 0 {
		return fmt.Errorf("%w: %s: page_cache_bytes is set, and a Postgres store has no page cache. "+
			"Remove the key", ErrSettings, id)
	}

	return nil
}

// storeDirPerm is the mode of the directory the store is created in.
const storeDirPerm = 0o700

// storeFilePerm is the mode of the store's file. SQLite gives the write-ahead
// log and its index the mode of the database file when it creates them.
const storeFilePerm = 0o600

// ErrDatabasePath reports a path SQLite cannot open.
var ErrDatabasePath = errors.New("engine: unusable database path")

// ErrJournalMode reports a store that will not go into write-ahead logging.
// Without it every read waits behind the current write.
var ErrJournalMode = errors.New("engine: the store is not in write-ahead logging")

// ErrPostgresUnlinked reports a build with no PostgreSQL driver in it.
var ErrPostgresUnlinked = errors.New("engine: this build carries no postgres driver")

// OpenSQLite opens the store's own database file and creates it and its
// directory when missing. The file is never ATTACHed to the host database, so
// a history write does not block a host write. pageCacheBytes bounds the page
// cache of every connection. Zero keeps SQLite's default.
func OpenSQLite(ctx context.Context, path string, pageCacheBytes int) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: the path is empty", ErrDatabasePath)
	}

	// The pragmas follow a "?" in the DSN, so a "?" in the path would cut the
	// path short.
	if strings.Contains(path, "?") {
		return nil, fmt.Errorf("%w: %q holds a question mark", ErrDatabasePath, path)
	}

	if err := os.MkdirAll(filepath.Dir(path), storeDirPerm); err != nil {
		return nil, fmt.Errorf("create the directory for the store: %w", err)
	}

	if err := ownFile(path); err != nil {
		return nil, fmt.Errorf("open the store: %w", err)
	}

	db, err := sql.Open(sqliteDriver, path+"?"+SQLitePragmas+pageCachePragma(pageCacheBytes))
	if err != nil {
		return nil, fmt.Errorf("open the store at %s: %w", path, err)
	}

	bindPool(db)

	if err = WantWAL(ctx, db, ErrJournalMode); err != nil {
		_ = db.Close() // the failure being returned is the one worth reporting

		return nil, err
	}

	return db, nil
}

// ownFile creates the store's file when missing and sets it to owner-only. The
// driver would create it through the process umask. An existing -wal or -shm
// file is narrowed too, because SQLite keeps the mode of a file it finds.
func ownFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, storeFilePerm)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}

	if err = file.Close(); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}

	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		if err = os.Chmod(name, storeFilePerm); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("make %s its owner's alone: %w", name, err)
		}
	}

	return nil
}
