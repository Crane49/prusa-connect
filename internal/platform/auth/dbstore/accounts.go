package dbstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/internal/platform/auth/local"
)

// The statements. They are written with '?' and hold no literal '?', so
// Engine.Q renumbers them for Postgres and changes nothing else.
//
// gosec reads a constant naming a password column as a hardcoded credential.
// These hold no secret. The hash arrives as a parameter and is never written
// into a statement.
//
//nolint:gosec // G101: statement text naming the password column, not a password.
const (
	accountColumns = `id, username, display_name, password_hash, created_at, password_changed_at`

	countAccountsSQL = `SELECT COUNT(*) FROM auth_account`

	// A single read returns the hash and the generation of the account's
	// credentials together, so a login mints at the generation of the hash it
	// checked.
	selectAccountWithGenerationSQL = `SELECT ` + accountColumns + `, COALESCE(g.generation, 0)
		FROM auth_account LEFT JOIN auth_subject_generation g ON g.subject = auth_account.id`

	selectAccountByUsernameSQL = selectAccountWithGenerationSQL + ` WHERE username = ?`

	selectAccountByIDSQL = selectAccountWithGenerationSQL + ` WHERE id = ?`

	raiseGenerationSQL = `INSERT INTO auth_subject_generation (subject, generation) VALUES (?, 1)
		ON CONFLICT (subject) DO UPDATE SET generation = auth_subject_generation.generation + 1`

	// Ordered by username, so a page an operator reads twice reads the same
	// way twice.
	listAccountsSQL = `SELECT ` + accountColumns + ` FROM auth_account ORDER BY username`

	insertAccountSQL = `INSERT INTO auth_account (` + accountColumns + `)
		VALUES (?, ?, ?, ?, ?, ?)`

	// An empty expected hash writes whatever is there. A named one writes only
	// over that hash, so two changes that checked the same password cannot
	// both land.
	updatePasswordSQL = `UPDATE auth_account SET password_hash = ?, password_changed_at = ?
		WHERE id = ? AND (? = '' OR password_hash = ?)`

	accountExistsSQL = `SELECT COUNT(*) FROM auth_account WHERE id = ?`

	selectAccountGenerationSQL = `SELECT COALESCE(
		(SELECT generation FROM auth_subject_generation WHERE subject = ?), 0)`

	deleteAccountSQL = `DELETE FROM auth_account WHERE id = ?`

	// The delete refuses the last account by counting inside the same
	// transaction, so two processes deleting the last two accounts at once
	// cannot both see a row left and both succeed.
	countOtherAccountsSQL = `SELECT COUNT(*) FROM auth_account WHERE id <> ?`
)

// Accounts is local.Store over the host's database.
type Accounts struct {
	db     *sql.DB
	engine Engine
	// subjects names every spelling an account's credentials are held under.
	// A password change and a delete raise the generation of each.
	subjects func(id string) []string
}

var _ local.Store = (*Accounts)(nil)

// NewAccounts returns the account store over an open handle. The schema is the
// host's and is already migrated by the time anything constructs this.
func NewAccounts(db *sql.DB, engine Engine) (*Accounts, error) {
	if db == nil {
		return nil, errors.New("dbstore: an account store needs an open database handle")
	}

	if err := checkEngine(engine); err != nil {
		return nil, err
	}

	return &Accounts{db: db, engine: engine, subjects: func(id string) []string { return []string{id} }}, nil
}

// WithCredentialSubjects says which subjects an account's credentials are held
// under, so a password change raises the generation of each. The composition
// root knows the spelling. A session names the bare id and an api credential
// names user:<id>.
func (a *Accounts) WithCredentialSubjects(subjects func(id string) []string) *Accounts {
	out := *a
	out.subjects = subjects

	return &out
}

// Count returns how many accounts exist. It decides whether the first-run setup
// path is open, so an error here must never read as zero. Every caller treats
// it as a refusal.
func (a *Accounts) Count(ctx context.Context) (int, error) {
	var count int

	if err := a.db.QueryRowContext(ctx, a.engine.Q(countAccountsSQL)).Scan(&count); err != nil {
		return 0, fmt.Errorf("count the accounts: %w", err)
	}

	return count, nil
}

// ByUsername returns the account holding a folded username.
func (a *Accounts) ByUsername(ctx context.Context, username string) (local.Account, error) {
	return a.one(ctx, selectAccountByUsernameSQL, username)
}

// ByID returns one account.
func (a *Accounts) ByID(ctx context.Context, id string) (local.Account, error) {
	return a.one(ctx, selectAccountByIDSQL, id)
}

// one reads a single row, reporting a miss as local.ErrNoAccount so a caller
// can tell "nobody by that name" from "the store could not answer".
func (a *Accounts) one(ctx context.Context, query string, arg any) (local.Account, error) {
	var (
		row       local.Account
		created   time.Time
		changedAt time.Time
	)

	err := a.db.QueryRowContext(ctx, a.engine.Q(query), arg).
		Scan(&row.ID, &row.Username, &row.DisplayName, &row.Hash, &created, &changedAt, &row.Generation)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return local.Account{}, local.ErrNoAccount
	case err != nil:
		return local.Account{}, fmt.Errorf("read an account: %w", err)
	}

	row.CreatedAt = created.UTC()
	row.PasswordAt = changedAt.UTC()

	return row, nil
}

// List returns every account, in username order.
func (a *Accounts) List(ctx context.Context) ([]local.Account, error) {
	rows, err := a.db.QueryContext(ctx, a.engine.Q(listAccountsSQL))
	if err != nil {
		return nil, fmt.Errorf("list the accounts: %w", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var out []local.Account

	for rows.Next() {
		var (
			row       local.Account
			created   time.Time
			changedAt time.Time
		)

		if err = rows.Scan(&row.ID, &row.Username, &row.DisplayName,
			&row.Hash, &created, &changedAt); err != nil {
			return nil, fmt.Errorf("scan an account: %w", err)
		}

		row.CreatedAt = created.UTC()
		row.PasswordAt = changedAt.UTC()

		out = append(out, row)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list the accounts: %w", err)
	}

	return out, nil
}

// Create writes a row, refusing an id or a username already held.
//
// The refusal comes from the unique constraints and not from a read before the
// write. Two processes creating the first administrator at once would both read
// an empty table, and the id of the first administrator is fixed, so the second
// insert has to fail. Which constraint was hit decides which refusal the caller
// gets, and that is read back from the store.
func (a *Accounts) Create(ctx context.Context, account local.Account) error {
	_, err := a.db.ExecContext(ctx, a.engine.Q(insertAccountSQL),
		account.ID, account.Username, account.DisplayName, account.Hash,
		account.CreatedAt.UTC(), account.PasswordAt.UTC())
	if err == nil {
		return nil
	}

	if taken := a.collision(ctx, account); taken != nil {
		return taken
	}

	return fmt.Errorf("create the account %q: %w", account.Username, err)
}

// collision names which uniqueness a failed insert broke, and reports nil for a
// failure that was neither. It reads the store instead of parsing the driver's
// message, because the two engines word that message differently and a string
// match would only be right on one of them.
func (a *Accounts) collision(ctx context.Context, account local.Account) error {
	if _, err := a.ByID(ctx, account.ID); err == nil {
		return fmt.Errorf("%w: %s", local.ErrIDTaken, account.ID)
	}

	if _, err := a.ByUsername(ctx, account.Username); err == nil {
		return local.ErrUsernameTaken
	}

	return nil
}

// SetPassword replaces one row's hash, over the expected one when it is named.
// It raises no generation, so it is for a rehash of the same password.
func (a *Accounts) SetPassword(ctx context.Context, id, expected, hash string, at time.Time) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin the password write of %q: %w", id, err)
	}

	defer func() { _ = tx.Rollback() }() // a committed transaction rolls back to nothing

	if err = a.writeHash(ctx, tx, id, expected, hash, at); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit the password write of %q: %w", id, err)
	}

	return nil
}

// writeHash writes one row's hash inside the caller's transaction, and tells a
// missing account from one whose hash is no longer the expected one.
func (a *Accounts) writeHash(ctx context.Context, tx *sql.Tx, id, expected, hash string, at time.Time) error {
	result, err := tx.ExecContext(ctx, a.engine.Q(updatePasswordSQL), hash, at.UTC(), id, expected, expected)
	if err != nil {
		return fmt.Errorf("set the password of %q: %w", id, err)
	}

	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count the rows the password change touched: %w", err)
	}

	if changed > 0 {
		return nil
	}

	var held int

	if err = tx.QueryRowContext(ctx, a.engine.Q(accountExistsSQL), id).Scan(&held); err != nil {
		return fmt.Errorf("read whether %q exists: %w", id, err)
	}

	if held == 0 {
		return local.ErrNoAccount
	}

	return local.ErrPasswordChanged
}

// ReplacePassword replaces one row's hash and raises the generation of every
// subject the account's credentials are held under, in one transaction. A
// login that read the old hash reads the old generation with it, so its
// session is refused. A login that read the new hash carries the new
// generation.
func (a *Accounts) ReplacePassword(ctx context.Context, id, expected, hash string, at time.Time) (int64, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin the password change of %q: %w", id, err)
	}

	defer func() { _ = tx.Rollback() }() // a committed transaction rolls back to nothing

	if err = a.writeHash(ctx, tx, id, expected, hash, at); err != nil {
		return 0, err
	}

	if err = a.raiseGenerations(ctx, tx, id); err != nil {
		return 0, err
	}

	// It is read inside the transaction, so the caller gets the generation
	// this write produced and never one that a later write produced.
	var generation int64

	if err = tx.QueryRowContext(ctx, a.engine.Q(selectAccountGenerationSQL), id).Scan(&generation); err != nil {
		return 0, fmt.Errorf("read the generation the password change of %q wrote: %w", id, err)
	}

	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit the password change of %q: %w", id, err)
	}

	return generation, nil
}

// raiseGenerations raises the generation of every subject an account's
// credentials are held under, inside the caller's transaction.
func (a *Accounts) raiseGenerations(ctx context.Context, tx *sql.Tx, id string) error {
	for _, subject := range a.subjects(id) {
		if _, err := tx.ExecContext(ctx, a.engine.Q(raiseGenerationSQL), subject); err != nil {
			return fmt.Errorf("raise the credential generation of %q: %w", subject, err)
		}
	}

	return nil
}

// Delete removes a row, refusing the last one there is.
//
// The count and the delete are one transaction. Nobody could log in to a server
// with no accounts, and the setup path would reopen for whoever reached it
// first, so two callers deleting the last two accounts at once must not both be
// told they left one behind.
func (a *Accounts) Delete(ctx context.Context, id string) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete the account %q: %w", id, err)
	}

	defer func() { _ = tx.Rollback() }() // a committed transaction rolls back to nothing

	var others int

	if err = tx.QueryRowContext(ctx, a.engine.Q(countOtherAccountsSQL), id).Scan(&others); err != nil {
		return fmt.Errorf("count the accounts beside %q: %w", id, err)
	}

	result, err := tx.ExecContext(ctx, a.engine.Q(deleteAccountSQL), id)
	if err != nil {
		return fmt.Errorf("delete the account %q: %w", id, err)
	}

	removed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count the rows the delete touched: %w", err)
	}

	switch {
	case removed == 0:
		return local.ErrNoAccount
	case others == 0:
		return local.ErrLastAccount
	}

	// A deleted person's credentials end in the same transaction, so a
	// session that was being minted as the delete ran is refused too.
	if err = a.raiseGenerations(ctx, tx, id); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("delete the account %q: %w", id, err)
	}

	return nil
}
