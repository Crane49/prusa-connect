// Package recstore is the host's side of sdk/stateapi: the key/value store an
// extension keeps its own data in, one namespace at a time.
//
// A module runs in its own process and reaches this store only through the
// methods of stateapi.Records.
//
// Store.Records binds the namespace into the value it returns. Every statement
// filters on it, and no method takes a namespace, so a module cannot name
// another module's records.
package recstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/adapters/db/internal/engine"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
)

// Dialect is the engine this store renders its SQL for.
type Dialect uint8

// The engines the store runs on. Every statement runs on both.
const (
	// DialectSQLite is the default.
	DialectSQLite Dialect = iota
	// DialectPostgres is PostgreSQL.
	DialectPostgres
)

// render rewrites a statement's '?' placeholders for PostgreSQL. A statement
// must contain no literal '?'.
func (d Dialect) render(query string) string {
	if d != DialectPostgres {
		return query
	}

	return engine.Numbered(query)
}

// ErrNamespace reports a namespace this store will not open. The host names
// the namespace, so this is a defect in the host, not in a module.
var ErrNamespace = errors.New("recstore: the namespace is not one this store opens")

// Store is the record store for every extension namespace.
type Store struct {
	db      *sql.DB
	dialect Dialect

	// writers has one mutex per namespace. It serialises that namespace's
	// writes so the quota check is correct: two writers that both read the
	// byte total before either commits would both find room. Versions need no
	// lock, because every write is one conditional statement.
	//
	// The lock is local to this process. Two hosts on one PostgreSQL database
	// can take a namespace over its limit.
	writers sync.Map
}

// New returns the record store over an open handle.
func New(db *sql.DB, dialect Dialect) (*Store, error) {
	if db == nil {
		return nil, errors.New("recstore: the store is opened on a database handle")
	}

	return &Store{db: db, dialect: dialect}, nil
}

// Records returns the records of one namespace, limited to limit bytes. Only
// the host calls it. An extension never supplies the namespace or the limit.
func (s *Store) Records(namespace string, limit int64) (stateapi.Records, error) {
	if strings.TrimSpace(namespace) == "" {
		return nil, fmt.Errorf("%w: it is empty", ErrNamespace)
	}

	if limit <= 0 || limit > stateapi.MaxGrantedNamespaceBytes {
		return nil, fmt.Errorf("%w: %s is to hold %d bytes, and a limit is from 1 to %d",
			ErrNamespace, namespace, limit, stateapi.MaxGrantedNamespaceBytes)
	}

	return &records{store: s, namespace: namespace, limit: limit}, nil
}

// Namespaces returns every namespace with a record, with its key count and
// byte total. It returns no record values.
func (s *Store) Namespaces(ctx context.Context) ([]Namespace, error) {
	const query = `SELECT namespace, COUNT(*), COALESCE(SUM(bytes), 0)
		FROM module_state GROUP BY namespace ORDER BY namespace`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("recstore: list the namespaces: %w", err)
	}

	defer func() {
		_ = rows.Close()
	}()

	var held []Namespace

	for rows.Next() {
		var one Namespace
		if err = rows.Scan(&one.Name, &one.Keys, &one.Bytes); err != nil {
			return nil, fmt.Errorf("recstore: read a namespace row: %w", err)
		}

		held = append(held, one)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("recstore: read the namespace rows: %w", err)
	}

	return held, nil
}

// Namespace is how much one extension stores.
type Namespace struct {
	Name string
	// Keys is the number of records.
	Keys int64
	// Bytes is what those records count toward the namespace's limit.
	Bytes int64
}

// Backend wraps the store as a stateapi.Backend. Close closes the handle.
type Backend struct{ *Store }

var _ stateapi.Backend = Backend{}

// Namespaces returns the store's Namespaces as stateapi.Accounting.
func (b Backend) Namespaces(ctx context.Context) ([]stateapi.Accounting, error) {
	held, err := b.Store.Namespaces(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]stateapi.Accounting, 0, len(held))
	for _, one := range held {
		out = append(out, stateapi.Accounting{Namespace: one.Name, Keys: one.Keys, Bytes: one.Bytes})
	}

	return out, nil
}

// Close closes the handle this store was opened on.
func (b Backend) Close() error {
	if err := b.Store.db.Close(); err != nil {
		return fmt.Errorf("recstore: close the store: %w", err)
	}

	return nil
}

// lockFor is the write lock for one namespace, created on first use.
func (s *Store) lockFor(namespace string) *sync.Mutex {
	held, _ := s.writers.LoadOrStore(namespace, &sync.Mutex{})

	lock, ok := held.(*sync.Mutex)
	if !ok {
		// Unreachable: only mutexes are stored. A fresh lock avoids a panic
		// while serving a module's request.
		return &sync.Mutex{}
	}

	return lock
}

// records is the stateapi.Records of one namespace. The namespace is a field
// and never a parameter, so no caller can change it.
type records struct {
	store     *Store
	namespace string
	// limit is the most bytes the namespace may store.
	limit int64
}

var _ stateapi.Records = (*records)(nil)

// Every statement that reaches a record. Each one must filter on namespace.
const (
	selectValueSQL = `SELECT record_value, version FROM module_state
		WHERE namespace = ? AND record_key = ?`

	selectVersionSQL = `SELECT version FROM module_state
		WHERE namespace = ? AND record_key = ?`

	selectKeysSQL = `SELECT record_key FROM module_state
		WHERE namespace = ? ORDER BY record_key`

	selectKeysUnderSQL = `SELECT record_key FROM module_state
		WHERE namespace = ? AND record_key LIKE ? ESCAPE '\' ORDER BY record_key`

	deleteRecordSQL = `DELETE FROM module_state WHERE namespace = ? AND record_key = ?`

	selectUsageSQL = `SELECT COALESCE(SUM(bytes), 0) FROM module_state WHERE namespace = ?`

	// The quota read: the bytes of the namespace and of the row this write
	// replaces.
	selectQuotaSQL = `SELECT COALESCE(SUM(bytes), 0),
		COALESCE(SUM(CASE WHEN record_key = ? THEN bytes ELSE 0 END), 0)
		FROM module_state WHERE namespace = ?`

	upsertRecordSQL = `INSERT INTO module_state
		(namespace, record_key, record_value, version, bytes, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT (namespace, record_key) DO UPDATE SET
			record_value = excluded.record_value,
			version      = module_state.version + 1,
			bytes        = excluded.bytes,
			updated_at   = excluded.updated_at
		RETURNING version`

	// CompareAndPut at stateapi.AbsentVersion. It returns no row when the
	// record already exists.
	insertAbsentSQL = `INSERT INTO module_state
		(namespace, record_key, record_value, version, bytes, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT (namespace, record_key) DO NOTHING
		RETURNING version`

	// CompareAndPut at a version. Of two writers at the same version, the
	// second matches no row.
	updateAtVersionSQL = `UPDATE module_state
		SET record_value = ?, version = version + 1, bytes = ?, updated_at = ?
		WHERE namespace = ? AND record_key = ? AND version = ?
		RETURNING version`
)

// Get returns the record under key.
func (r *records) Get(ctx context.Context, key stateapi.Key) (stateapi.Value, error) {
	if err := key.Validate(); err != nil {
		return stateapi.Value{}, err
	}

	var held stateapi.Value

	row := r.store.db.QueryRowContext(ctx,
		r.store.dialect.render(selectValueSQL), r.namespace, string(key))

	if err := row.Scan(&held.Bytes, &held.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return stateapi.Value{}, fmt.Errorf("%w: %s", stateapi.ErrNoRecord, key)
		}

		return stateapi.Value{}, fmt.Errorf("recstore: read %s: %w", key, err)
	}

	// A driver may return nil for a zero-byte value. Return an empty slice,
	// so a record is never nil.
	if held.Bytes == nil {
		held.Bytes = []byte{}
	}

	return held, nil
}

// Version returns the version of the record under key, without its bytes.
func (r *records) Version(ctx context.Context, key stateapi.Key) (uint64, error) {
	if err := key.Validate(); err != nil {
		return 0, err
	}

	var version uint64

	row := r.store.db.QueryRowContext(ctx,
		r.store.dialect.render(selectVersionSQL), r.namespace, string(key))

	if err := row.Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("%w: %s", stateapi.ErrNoRecord, key)
		}

		return 0, fmt.Errorf("recstore: read the version of %s: %w", key, err)
	}

	return version, nil
}

// Usage returns how much the namespace stores, and its limit.
func (r *records) Usage(ctx context.Context) (stateapi.Usage, error) {
	usage := stateapi.Usage{Limit: r.limit}

	row := r.store.db.QueryRowContext(ctx, r.store.dialect.render(selectUsageSQL), r.namespace)
	if err := row.Scan(&usage.Bytes); err != nil {
		return stateapi.Usage{}, fmt.Errorf("recstore: read what %s holds: %w", r.namespace, err)
	}

	return usage, nil
}

// List returns every key under prefix, sorted, without values.
func (r *records) List(ctx context.Context, prefix stateapi.Key) ([]stateapi.Key, error) {
	if err := stateapi.ValidatePrefix(prefix); err != nil {
		return nil, err
	}

	query, args := selectKeysSQL, []any{r.namespace}
	if prefix != "" {
		query = selectKeysUnderSQL
		args = []any{r.namespace, likePrefix(string(prefix))}
	}

	rows, err := r.store.db.QueryContext(ctx, r.store.dialect.render(query), args...)
	if err != nil {
		return nil, fmt.Errorf("recstore: list under %q: %w", prefix, err)
	}

	defer func() {
		_ = rows.Close()
	}()

	keys := []stateapi.Key{}

	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("recstore: read a key: %w", err)
		}

		keys = append(keys, stateapi.Key(key))
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("recstore: read the keys under %q: %w", prefix, err)
	}

	return keys, nil
}

// likePrefix returns a LIKE pattern that matches prefix literally. Without the
// escaping, a '%' in a caller's prefix would list the whole namespace.
func likePrefix(prefix string) string {
	replaced := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)

	return replaced + "%"
}

// Delete removes the record under key.
func (r *records) Delete(ctx context.Context, key stateapi.Key) error {
	if err := key.Validate(); err != nil {
		return err
	}

	lock := r.store.lockFor(r.namespace)
	lock.Lock()

	defer lock.Unlock()

	result, err := r.store.db.ExecContext(ctx,
		r.store.dialect.render(deleteRecordSQL), r.namespace, string(key))
	if err != nil {
		return fmt.Errorf("recstore: delete %s: %w", key, err)
	}

	removed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("recstore: delete %s: %w", key, err)
	}

	if removed == 0 {
		return fmt.Errorf("%w: %s", stateapi.ErrNoRecord, key)
	}

	return nil
}

// Put writes value under key whatever is there.
func (r *records) Put(ctx context.Context, key stateapi.Key, value []byte) (uint64, error) {
	return r.write(ctx, key, value, func(ctx context.Context, tx *sql.Tx, stored []byte) (uint64, error) {
		var version uint64

		row := tx.QueryRowContext(ctx, r.store.dialect.render(upsertRecordSQL),
			r.namespace, string(key), stored, stateapi.Cost(key, stored), time.Now().UTC())

		if err := row.Scan(&version); err != nil {
			return 0, fmt.Errorf("recstore: write %s: %w", key, err)
		}

		return version, nil
	})
}

// CompareAndPut writes value under key only if the record is at expected. A
// record that exists when AbsentVersion is expected is also ErrVersion.
func (r *records) CompareAndPut(
	ctx context.Context, key stateapi.Key, expected uint64, value []byte,
) (uint64, error) {
	return r.write(ctx, key, value, func(ctx context.Context, tx *sql.Tx, stored []byte) (uint64, error) {
		query, args := updateAtVersionSQL, []any{
			stored, stateapi.Cost(key, stored), time.Now().UTC(),
			r.namespace, string(key), expected,
		}

		if expected == stateapi.AbsentVersion {
			query = insertAbsentSQL
			args = []any{
				r.namespace, string(key), stored, stateapi.Cost(key, stored), time.Now().UTC(),
			}
		}

		var version uint64

		row := tx.QueryRowContext(ctx, r.store.dialect.render(query), args...)

		switch err := row.Scan(&version); {
		case err == nil:
			return version, nil
		case errors.Is(err, sql.ErrNoRows):
			return 0, fmt.Errorf("%w: %s is not at %d", stateapi.ErrVersion, key, expected)
		default:
			return 0, fmt.Errorf("recstore: write %s at %d: %w", key, expected, err)
		}
	})
}

// write is shared by Put and CompareAndPut. The quota is read in the same
// transaction as the write, under the namespace lock.
func (r *records) write(
	ctx context.Context,
	key stateapi.Key,
	value []byte,
	statement func(context.Context, *sql.Tx, []byte) (uint64, error),
) (uint64, error) {
	if err := key.Validate(); err != nil {
		return 0, err
	}

	if err := stateapi.ValidateValue(value); err != nil {
		return 0, err
	}

	// The column is NOT NULL, and a nil slice reaches PostgreSQL as NULL.
	if value == nil {
		value = []byte{}
	}

	lock := r.store.lockFor(r.namespace)
	lock.Lock()

	defer lock.Unlock()

	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("recstore: begin a write to %s: %w", key, err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	if err = r.checkQuota(ctx, tx, key, value); err != nil {
		return 0, err
	}

	version, err := statement(ctx, tx, value)
	if err != nil {
		return 0, err
	}

	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("recstore: commit the write to %s: %w", key, err)
	}

	return version, nil
}

// checkQuota refuses a write that would take the namespace over its byte
// limit. The row being replaced is subtracted first.
func (r *records) checkQuota(ctx context.Context, tx *sql.Tx, key stateapi.Key, value []byte) error {
	var held, replacing int64

	row := tx.QueryRowContext(ctx, r.store.dialect.render(selectQuotaSQL), string(key), r.namespace)
	if err := row.Scan(&held, &replacing); err != nil {
		return fmt.Errorf("recstore: read what %s holds: %w", r.namespace, err)
	}

	after := held - replacing + stateapi.Cost(key, value)
	if after > r.limit {
		return fmt.Errorf("%w: %s holds %d bytes, this write would make it %d and the limit is %d",
			stateapi.ErrNamespaceFull, r.namespace, held, after, r.limit)
	}

	return nil
}
