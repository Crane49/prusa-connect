package devicestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
)

// The registration table, kept in the host's own database so a device somebody
// approved is still approved after a restart. The table is in this package's
// baseline, under the host's one ledger, because the host is its only writer.
//
// The statements are written once with '?' and renumbered for Postgres, the way
// every other store here does it.
const (
	registrationColumns = `connector, code, fingerprint, token_hash, serial, model, firmware, kind,
		approved, endorsed, completed, admitted_by, admitted_at, issued_at, expires_at, seq`

	selectRegistrationsSQL = `SELECT ` + registrationColumns + `
		FROM device_registration WHERE connector = ? ORDER BY seq`

	upsertRegistrationSQL = `INSERT INTO device_registration (` + registrationColumns + `)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (connector, code) DO UPDATE SET
			fingerprint = excluded.fingerprint,
			token_hash = excluded.token_hash,
			serial = excluded.serial,
			model = excluded.model,
			firmware = excluded.firmware,
			kind = excluded.kind,
			approved = excluded.approved,
			endorsed = excluded.endorsed,
			completed = excluded.completed,
			admitted_by = excluded.admitted_by,
			admitted_at = excluded.admitted_at,
			issued_at = excluded.issued_at,
			expires_at = excluded.expires_at,
			seq = excluded.seq`

	deleteRegistrationSQL = `DELETE FROM device_registration WHERE connector = ? AND code = ?`
)

// Announcer is what the store tells about a code it has not seen before. The
// device plane satisfies it.
type Announcer interface {
	RegistrationAnnounced(ctx context.Context, record admission.Record)
}

// Registrations is admission.Store over the host's database.
//
// It is also where the host first learns that a machine is waiting. A connector
// holds the registration table and mints the codes, and every change to one
// reaches this store before it reaches anything else, so this is the one place
// that can say "a code arrived" without the host asking a driver on a timer.
type Registrations struct {
	db     *sql.DB
	engine sqlstore.Engine

	// announce is told about each code the first time it is written. It is nil
	// for a process with no event bus, which is what the command line builds.
	announce Announcer

	// seen is the codes this process has written or read back, keyed by
	// connector and code. It keeps the announcement to one per code: a
	// machine registers again every few seconds until somebody decides, and
	// each of those writes the same row.
	//
	// It is seeded by Load, so a restart announces nothing the last run already
	// held. It grows with the codes a connector mints and shrinks with Forget,
	// and the connectors bound the table themselves.
	mu   sync.Mutex
	seen map[string]bool
}

var _ admission.Store = (*Registrations)(nil)

// NewRegistrations returns the store the connectors write their tables
// through. announce may be nil, for a process with no event bus.
func NewRegistrations(db *sql.DB, engine sqlstore.Engine, announce Announcer) (*Registrations, error) {
	if db == nil {
		return nil, errors.New("devicestore: a registration store needs an open database handle")
	}

	if err := engine.Check(); err != nil {
		return nil, err
	}

	return &Registrations{db: db, engine: engine, announce: announce, seen: map[string]bool{}}, nil
}

// registrationKey is one registration's place in the seen set. The two halves
// are joined by a byte neither a connector ref nor a code carries.
func registrationKey(connector, code string) string { return connector + "\x00" + code }

// firstSight reports whether this code is one this process has not written
// before, and records it either way.
func (r *Registrations) firstSight(connector, code string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := registrationKey(connector, code)
	if r.seen[key] {
		return false
	}

	r.seen[key] = true

	return true
}

// forget drops one code from the seen set, so a machine that registers again
// after a rejection is announced again.
func (r *Registrations) forget(connector, code string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.seen, registrationKey(connector, code))
}

// q renders a statement for the engine this deployment runs on.
func (r *Registrations) q(query string) string { return r.engine.Q(query) }

// Load returns every registration one connector holds, oldest first.
func (r *Registrations) Load(ctx context.Context, connector string) ([]admission.Record, error) {
	rows, err := r.db.QueryContext(ctx, r.q(selectRegistrationsSQL), connector)
	if err != nil {
		return nil, fmt.Errorf("read the registrations of %s: %w", connector, err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var out []admission.Record

	for rows.Next() {
		record, scanErr := scanRegistration(rows)
		if scanErr != nil {
			return nil, scanErr
		}

		out = append(out, record)

		// Seeded and not announced. These codes were minted by the last
		// run, so telling a subscriber they just arrived would announce a fleet
		// of waiting machines at every restart.
		r.firstSight(record.Connector, record.Code)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read the registrations of %s: %w", connector, err)
	}

	return out, nil
}

// scanRegistration reads one row. The two nullable instants are read through
// sql.NullTime, because "nobody has approved this" and "approved at the zero
// instant" have to stay different answers.
func scanRegistration(rows interface{ Scan(...any) error }) (admission.Record, error) {
	var (
		record   admission.Record
		admitted sql.NullTime
		expires  sql.NullTime
		issued   time.Time
	)

	err := rows.Scan(
		&record.Connector, &record.Code, &record.Fingerprint, &record.TokenHash,
		&record.Serial, &record.Model, &record.Firmware, &record.Kind,
		&record.Approved, &record.Endorsed, &record.Completed,
		&record.AdmittedBy, &admitted, &issued, &expires, &record.Seq,
	)
	if err != nil {
		return admission.Record{}, fmt.Errorf("scan a registration: %w", err)
	}

	record.IssuedAt = issued.UTC()

	if admitted.Valid {
		record.AdmittedAt = admitted.Time.UTC()
	}

	if expires.Valid {
		record.ExpiresAt = expires.Time.UTC()
	}

	return record, nil
}

// Save writes one registration, replacing the row already held for that code.
func (r *Registrations) Save(ctx context.Context, record admission.Record) error {
	_, err := r.db.ExecContext(ctx, r.q(upsertRegistrationSQL),
		record.Connector, record.Code, record.Fingerprint, record.TokenHash,
		record.Serial, record.Model, record.Firmware, record.Kind,
		record.Approved, record.Endorsed, record.Completed,
		record.AdmittedBy, nullableTime(record.AdmittedAt),
		record.IssuedAt.UTC(), nullableTime(record.ExpiresAt), record.Seq)
	if err != nil {
		return fmt.Errorf("save the registration %s on %s: %w", record.Code, record.Connector, err)
	}

	// After the write and never before it. The frame says a code is waiting,
	// and a code that is waiting is one this server would answer for if the
	// process stopped here. A code approved the moment it was written waits
	// for nobody: its approved frame says so, and an announcement beside it
	// would ask a person to decide something already decided.
	first := r.firstSight(record.Connector, record.Code)
	if r.announce != nil && first && !record.Approved {
		r.announce.RegistrationAnnounced(ctx, record)
	}

	return nil
}

// Forget removes one registration and every credential it held.
func (r *Registrations) Forget(ctx context.Context, connector, code string) error {
	if _, err := r.db.ExecContext(ctx, r.q(deleteRegistrationSQL), connector, code); err != nil {
		return fmt.Errorf("forget the registration %s on %s: %w", code, connector, err)
	}

	r.forget(connector, code)

	return nil
}

// nullableTime writes a zero instant as NULL, so a row nobody decided does not
// claim a decision at the zero instant.
func nullableTime(at time.Time) sql.NullTime {
	if at.IsZero() {
		return sql.NullTime{}
	}

	return sql.NullTime{Time: at.UTC(), Valid: true}
}
