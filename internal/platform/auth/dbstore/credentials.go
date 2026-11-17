package dbstore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
)

// The statements, written once with '?' for both engines.
//
// gosec reads a constant whose name ends in credential as a hardcoded one.
// These are SQL statements over the credential table and hold no secret: the
// only secret this package handles is a digest, and it arrives as a parameter.
//
//nolint:gosec // G101: statement text over the credential table, not a credential.
const (
	credentialColumns = `public_id, subject, kind, name, digest, issued_at, expires_at, idle_secs, last_seen,
		minted_by, generation, minter_generation, root_creator, root_generation, scopes, origin`

	// A lookup reads the current generations of the subject and of the minter
	// in the same statement, so a request costs no second round trip for them.
	selectCredentialSQL = `SELECT c.public_id, c.subject, c.kind, c.name, c.digest, c.issued_at,
		c.expires_at, c.idle_secs, c.last_seen, c.minted_by, c.generation, c.minter_generation,
		c.root_creator, c.root_generation, c.scopes, c.origin,
		COALESCE(g.generation, 0), COALESCE(m.generation, 0), COALESCE(r.generation, 0)
		FROM auth_credential c
		LEFT JOIN auth_subject_generation g ON g.subject = c.subject
		LEFT JOIN auth_subject_generation m ON m.subject = c.minted_by
		LEFT JOIN auth_subject_generation r ON r.subject = c.root_creator
		WHERE c.public_id = ?`

	selectGenerationSQL = `SELECT generation FROM auth_subject_generation WHERE subject = ?`

	// An api credential an older build minted names nobody as its minter.
	// The kind is spelled the way Kind.String writes it.
	countTokensWithoutMinterSQL = `SELECT COUNT(*) FROM auth_credential
		WHERE kind = 'api' AND minted_by = ''`

	// Oldest first, with the id as the tie-break, so two reads of one subject
	// render in one order.
	selectSubjectCredentialsSQL = `SELECT ` + credentialColumns +
		` FROM auth_credential WHERE subject = ? ORDER BY issued_at, public_id`

	// One statement for an insert and for the rewrite of a row already there.
	// Save means "write this row", so a caller never has to know which of the
	// two happens.
	upsertCredentialSQL = `INSERT INTO auth_credential (` + credentialColumns + `)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (public_id) DO UPDATE SET
			subject = excluded.subject,
			kind = excluded.kind,
			name = excluded.name,
			digest = excluded.digest,
			issued_at = excluded.issued_at,
			expires_at = excluded.expires_at,
			idle_secs = excluded.idle_secs,
			last_seen = excluded.last_seen,
			minted_by = excluded.minted_by,
			generation = excluded.generation,
			minter_generation = excluded.minter_generation,
			root_creator = excluded.root_creator,
			root_generation = excluded.root_generation,
			scopes = excluded.scopes,
			origin = excluded.origin`

	touchCredentialSQL = `UPDATE auth_credential SET last_seen = ? WHERE public_id = ?`

	deleteCredentialSQL = `DELETE FROM auth_credential WHERE public_id = ?`

	deleteSubjectCredentialsSQL = `DELETE FROM auth_credential WHERE subject = ?`

	// The two ends of a password change. Each deletes only rows below the
	// current generation, so a session opened with the new password stays.
	deleteOlderSubjectCredentialsSQL = `DELETE FROM auth_credential WHERE subject = ? AND generation <
		COALESCE((SELECT generation FROM auth_subject_generation WHERE subject = ?), 0)`

	deleteOlderMintedCredentialsSQL = `DELETE FROM auth_credential WHERE minted_by = ? AND minter_generation <
		COALESCE((SELECT generation FROM auth_subject_generation WHERE subject = ?), 0)`

	deleteOlderRootCredentialsSQL = `DELETE FROM auth_credential WHERE root_creator = ? AND root_generation <
		COALESCE((SELECT generation FROM auth_subject_generation WHERE subject = ?), 0)`

	// Housekeeping. The verifier refuses a credential past its absolute expiry
	// whether or not the row is there, so this only keeps the table from
	// growing without bound.
	deleteExpiredCredentialsSQL = `DELETE FROM auth_credential WHERE expires_at < ?`
)

// touchSlack is how stale a credential's last-seen may be before a request
// writes it back.
//
// The sliding window is auth.SessionIdle, measured in hours. Writing the row on
// every request would put one write on the database for every read a browser
// makes, which on SQLite means taking the write lock behind every page of a
// listing. A minute of slack costs a session at most a minute of its window and
// removes almost all of those writes.
const touchSlack = time.Minute

// Credentials is auth.Store over the host's database, so a session and an api
// token survive a restart.
//
// Sessions are stored here and not in memory. In memory a restart would be a
// re-login for every operator on the site and a dead token for every script.
// The row holds no secret, and a revocation deletes the row.
type Credentials struct {
	db     *sql.DB
	engine Engine

	// touched is the last-seen this process has already written, per public id,
	// so touchSlack can skip a statement that would move the value by a
	// millisecond. It is a cache of what was written and never a source of
	// truth: a miss writes.
	mu      sync.Mutex
	touched map[string]time.Time
}

var _ auth.Store = (*Credentials)(nil)

// NewCredentials returns the credential store over an open handle.
func NewCredentials(db *sql.DB, engine Engine) (*Credentials, error) {
	if db == nil {
		return nil, errors.New("dbstore: a credential store needs an open database handle")
	}

	if err := checkEngine(engine); err != nil {
		return nil, err
	}

	return &Credentials{db: db, engine: engine, touched: make(map[string]time.Time)}, nil
}

// Lookup returns the row for a public id, or auth.ErrNoCredential.
func (c *Credentials) Lookup(ctx context.Context, publicID string) (auth.Credential, error) {
	var current, minterCurrent, rootCurrent int64

	cred, err := scanCredential(c.db.QueryRowContext(ctx, c.engine.Q(selectCredentialSQL), publicID),
		&current, &minterCurrent, &rootCurrent)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return auth.Credential{}, auth.ErrNoCredential
	case err != nil:
		return auth.Credential{}, fmt.Errorf("read the credential %q: %w", publicID, err)
	}

	cred.CurrentGeneration = current
	cred.MinterCurrentGeneration = minterCurrent
	cred.RootCurrentGeneration = rootCurrent

	return cred, nil
}

// ListSubject returns every row held for one subject, oldest first.
func (c *Credentials) ListSubject(ctx context.Context, subject string) ([]auth.Credential, error) {
	rows, err := c.db.QueryContext(ctx, c.engine.Q(selectSubjectCredentialsSQL), subject)
	if err != nil {
		return nil, fmt.Errorf("list the credentials of %q: %w", subject, err)
	}

	defer rows.Close()

	held := make([]auth.Credential, 0)

	for rows.Next() {
		cred, scanErr := scanCredential(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("read a credential of %q: %w", subject, scanErr)
		}

		held = append(held, cred)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list the credentials of %q: %w", subject, err)
	}

	return held, nil
}

// scanner is the one method *sql.Row and *sql.Rows share.
type scanner interface {
	Scan(dest ...any) error
}

// scanCredential reads one row in the column order credentialColumns names,
// and then into extra. A lookup reads the current generations through extra.
func scanCredential(row scanner, extra ...any) (auth.Credential, error) {
	var (
		cred     auth.Credential
		kind     string
		scopes   string
		digest   string
		idleSecs int64
		issued   time.Time
		expires  time.Time
		lastSeen time.Time
	)

	dest := append([]any{&cred.PublicID, &cred.Subject, &kind, &cred.Name, &digest,
		&issued, &expires, &idleSecs, &lastSeen, &cred.MintedBy, &cred.Generation, &cred.MinterGeneration,
		&cred.RootCreator, &cred.RootGeneration, &scopes, &cred.Origin}, extra...)

	err := row.Scan(dest...)
	if err != nil {
		return auth.Credential{}, err
	}

	decoded, decodeErr := hex.DecodeString(digest)
	if decodeErr != nil || len(decoded) != len(cred.Digest) {
		// A row whose digest is not a digest can never match a presented
		// token. Reporting it as a store failure makes the verifier refuse
		// instead of comparing against zeroes.
		return auth.Credential{}, fmt.Errorf("the stored digest of %q is not a sha-256: %w",
			cred.PublicID, errors.Join(decodeErr, errUnusableDigest))
	}

	copy(cred.Digest[:], decoded)

	cred.Kind = kindOf(kind)
	cred.IssuedAt = issued.UTC()
	cred.ExpiresAt = expires.UTC()
	cred.Idle = time.Duration(idleSecs) * time.Second
	cred.LastSeen = lastSeen.UTC()
	cred.Scopes = splitScopes(scopes)

	return cred, nil
}

// errUnusableDigest is the cause behind a row whose digest column cannot be a
// SHA-256. It reaches a log line and never a client.
var errUnusableDigest = errors.New("dbstore: the stored digest is not 32 bytes of hex")

// Save writes a row.
func (c *Credentials) Save(ctx context.Context, cred auth.Credential) error {
	_, err := c.db.ExecContext(ctx, c.engine.Q(upsertCredentialSQL),
		cred.PublicID, cred.Subject, cred.Kind.String(), cred.Name, hex.EncodeToString(cred.Digest[:]),
		cred.IssuedAt.UTC(), cred.ExpiresAt.UTC(), int64(cred.Idle/time.Second), cred.LastSeen.UTC(),
		cred.MintedBy, cred.Generation, cred.MinterGeneration, cred.RootCreator, cred.RootGeneration,
		joinScopes(cred.Scopes), cred.Origin)
	if err != nil {
		return fmt.Errorf("save the credential %q: %w", cred.PublicID, err)
	}

	c.remember(cred.PublicID, cred.LastSeen)

	return nil
}

// Touch slides a row's idle window, skipping the write when this process
// already wrote a last-seen within touchSlack of this one.
//
// A row that is not there is auth.ErrNoCredential, and the slack never hides
// that. A public id this process has not written always runs the statement.
func (c *Credentials) Touch(ctx context.Context, publicID string, seen time.Time) error {
	if c.fresh(publicID, seen) {
		return nil
	}

	result, err := c.db.ExecContext(ctx, c.engine.Q(touchCredentialSQL), seen.UTC(), publicID)
	if err != nil {
		return fmt.Errorf("slide the credential %q: %w", publicID, err)
	}

	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count the rows the slide touched: %w", err)
	}

	if changed == 0 {
		c.forget(publicID)

		return auth.ErrNoCredential
	}

	c.remember(publicID, seen)

	return nil
}

// Revoke deletes a row. Deleting one that is not there is not an error: the
// caller wanted it gone and it is gone.
func (c *Credentials) Revoke(ctx context.Context, publicID string) error {
	if _, err := c.db.ExecContext(ctx, c.engine.Q(deleteCredentialSQL), publicID); err != nil {
		return fmt.Errorf("revoke the credential %q: %w", publicID, err)
	}

	c.forget(publicID)

	return nil
}

// RevokeSubject deletes every row held for one subject and reports how many it
// removed. A password change calls it, so the sessions opened under the old
// password end on every transport at once.
func (c *Credentials) RevokeSubject(ctx context.Context, subject string) (int, error) {
	result, err := c.db.ExecContext(ctx, c.engine.Q(deleteSubjectCredentialsSQL), subject)
	if err != nil {
		return 0, fmt.Errorf("revoke every credential of %q: %w", subject, err)
	}

	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count the credentials revoked: %w", err)
	}

	// The deleted public ids are not read back, so the whole cache goes. It
	// holds only last-seen values, and losing one costs a single extra write.
	c.mu.Lock()
	clear(c.touched)
	c.mu.Unlock()

	return int(removed), nil
}

// Generation returns a subject's current generation, zero when the subject
// has never had one raised.
func (c *Credentials) Generation(ctx context.Context, subject string) (int64, error) {
	var generation int64

	err := c.db.QueryRowContext(ctx, c.engine.Q(selectGenerationSQL), subject).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}

	if err != nil {
		return 0, fmt.Errorf("read the generation of %q: %w", subject, err)
	}

	return generation, nil
}

// CountTokensWithoutMinter counts the api credentials that name nobody as
// their minter, which an older build wrote. Such a token made for a service
// account or for another person is not ended by the password change of the
// person who made it, so the shell reset tells the operator. It runs after the
// reset ended the person's own tokens, so those are not counted.
func (c *Credentials) CountTokensWithoutMinter(ctx context.Context) (int, error) {
	var count int

	if err := c.db.QueryRowContext(ctx, c.engine.Q(countTokensWithoutMinterSQL)).Scan(&count); err != nil {
		return 0, fmt.Errorf("count the api credentials with no minter: %w", err)
	}

	return count, nil
}

// EndSubjects deletes every row of each subject below its current generation,
// and every row a subject minted below its current generation, all in one
// transaction. Either every subject is ended or none is.
func (c *Credentials) EndSubjects(ctx context.Context, subjects []string) (auth.Ended, error) {
	for _, subject := range subjects {
		if err := auth.ValidSubject(subject); err != nil {
			return auth.Ended{}, err
		}
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return auth.Ended{}, fmt.Errorf("begin ending the credentials: %w", err)
	}

	defer func() { _ = tx.Rollback() }() // A no-op after the commit below.

	removed := auth.Ended{Held: make([]int, len(subjects))}

	for i, subject := range subjects {
		count, deleteErr := execCount(ctx, tx, c.engine.Q(deleteOlderSubjectCredentialsSQL), subject, subject)
		if deleteErr != nil {
			return auth.Ended{}, fmt.Errorf("revoke the older credentials of %q: %w", subject, deleteErr)
		}

		removed.Held[i] = count
	}

	// The rows a subject minted for itself are already gone above, so what is
	// left under its name was minted for somebody else.
	for _, subject := range subjects {
		count, deleteErr := execCount(ctx, tx, c.engine.Q(deleteOlderMintedCredentialsSQL), subject, subject)
		if deleteErr != nil {
			return auth.Ended{}, fmt.Errorf("revoke the older credentials %q minted: %w", subject, deleteErr)
		}

		removed.MintedForOthers += count

		count, deleteErr = execCount(ctx, tx, c.engine.Q(deleteOlderRootCredentialsSQL), subject, subject)
		if deleteErr != nil {
			return auth.Ended{}, fmt.Errorf("revoke the older credentials minted under %q: %w", subject, deleteErr)
		}

		removed.MintedForOthers += count
	}

	if err = tx.Commit(); err != nil {
		return auth.Ended{}, fmt.Errorf("commit ending the credentials: %w", err)
	}

	// The deleted public ids are not read back, so the whole cache goes, as in
	// RevokeSubject.
	c.mu.Lock()
	clear(c.touched)
	c.mu.Unlock()

	return removed, nil
}

// execCount runs one statement in a transaction and reports the rows it
// changed.
func execCount(ctx context.Context, tx *sql.Tx, query string, args ...any) (int, error) {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count the rows changed: %w", err)
	}

	return int(count), nil
}

// DeleteExpired removes every credential whose absolute expiry has passed and
// reports how many. The verifier refuses an expired credential whether or not
// the row is there, so this is housekeeping. Without it the table grows by one
// row per login.
func (c *Credentials) DeleteExpired(ctx context.Context, now time.Time) (int, error) {
	result, err := c.db.ExecContext(ctx, c.engine.Q(deleteExpiredCredentialsSQL), now.UTC())
	if err != nil {
		return 0, fmt.Errorf("delete the expired credentials: %w", err)
	}

	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count the expired credentials deleted: %w", err)
	}

	return int(removed), nil
}

// fresh reports whether this process wrote a last-seen recently enough that
// another write would move it by less than touchSlack.
func (c *Credentials) fresh(publicID string, seen time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	written, ok := c.touched[publicID]

	return ok && seen.Sub(written) < touchSlack
}

func (c *Credentials) remember(publicID string, seen time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.touched[publicID] = seen
}

func (c *Credentials) forget(publicID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.touched, publicID)
}

// kindOf reads back what Kind.String wrote. An unrecognised word becomes
// auth.KindUnknown, which no transport accepts, so a row written by a build
// that knew a kind this one does not is refused and never admitted as something
// else.
func kindOf(name string) auth.Kind {
	switch name {
	case auth.KindSession.String():
		return auth.KindSession
	case auth.KindAPI.String():
		return auth.KindAPI
	case auth.KindMQTT.String():
		return auth.KindMQTT
	default:
		return auth.KindUnknown
	}
}

// joinScopes writes scopes the way the scopes column holds them: separated by
// one space, as OAuth writes a scope list.
func joinScopes(scopes []string) string { return strings.Join(scopes, " ") }

// splitScopes reads the scopes column. An empty column is no scope at all, and
// never one empty scope.
func splitScopes(column string) []string { return strings.Fields(column) }
