package dbstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
)

// The statements over the pairing table, written once with '?' for both
// engines.
const (
	pairingColumns = `provider, subject, principal, label, created_at, generation, created_by, creator_generation,
		root_creator, root_generation`

	// A lookup reads the principal's current generation in the same statement.
	selectPairingSQL = `SELECT p.provider, p.subject, p.principal, p.label, p.created_at, p.generation,
		p.created_by, p.creator_generation, p.root_creator, p.root_generation,
		COALESCE(g.generation, 0), COALESCE(c.generation, 0), COALESCE(r.generation, 0)
		FROM auth_pairing p
		LEFT JOIN auth_subject_generation g ON g.subject = p.principal
		LEFT JOIN auth_subject_generation c ON c.subject = p.created_by
		LEFT JOIN auth_subject_generation r ON r.subject = p.root_creator
		WHERE p.provider = ? AND p.subject = ?`

	// Provider then subject, which is the primary key's order, so two reads
	// render alike.
	selectPairingsSQL = `SELECT ` + pairingColumns + ` FROM auth_pairing ORDER BY provider, subject`

	selectProviderPairingsSQL = `SELECT ` + pairingColumns +
		` FROM auth_pairing WHERE provider = ? ORDER BY provider, subject`

	// A plain insert, not an upsert. The store refuses a subject that is
	// already paired, because changing the principal a subject acts as takes
	// two steps from an administrator.
	insertPairingSQL = `INSERT INTO auth_pairing (` + pairingColumns + `) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	deletePairingSQL = `DELETE FROM auth_pairing WHERE provider = ? AND subject = ?`

	countPairingsWithoutCreatorSQL = `SELECT COUNT(*) FROM auth_pairing WHERE created_by = ''`

	// Only pairings below the principal's current generation, so one written
	// after the password change stays.
	deletePrincipalPairingsSQL = `DELETE FROM auth_pairing WHERE principal = ? AND generation <
		COALESCE((SELECT generation FROM auth_subject_generation WHERE subject = ?), 0)`

	// Only pairings the person wrote, or whose chain the person is the root
	// of, below their current generation.
	deleteCreatedPairingsSQL = `DELETE FROM auth_pairing WHERE
		(created_by = ? AND creator_generation <
			COALESCE((SELECT generation FROM auth_subject_generation WHERE subject = ?), 0))
		OR (root_creator = ? AND root_generation <
			COALESCE((SELECT generation FROM auth_subject_generation WHERE subject = ?), 0))`
)

// Pairings is auth.Pairings over the host's database, so a pairing an
// administrator wrote outlives the process and sits beside the graph whose
// principals it names.
type Pairings struct {
	db     *sql.DB
	engine Engine
}

var _ auth.Pairings = (*Pairings)(nil)

// NewPairings returns the pairing store over an open handle.
func NewPairings(db *sql.DB, engine Engine) (*Pairings, error) {
	if db == nil {
		return nil, errors.New("dbstore: a pairing store needs an open database handle")
	}

	if err := checkEngine(engine); err != nil {
		return nil, err
	}

	return &Pairings{db: db, engine: engine}, nil
}

// Lookup returns the pairing for a subject, or auth.ErrNoPairing.
func (p *Pairings) Lookup(ctx context.Context, provider, subject string) (auth.Pairing, error) {
	var current, creatorCurrent, rootCurrent int64

	row, err := scanPairing(p.db.QueryRowContext(ctx, p.engine.Q(selectPairingSQL), provider, subject),
		&current, &creatorCurrent, &rootCurrent)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return auth.Pairing{}, auth.ErrNoPairing
	case err != nil:
		return auth.Pairing{}, fmt.Errorf("read the pairing of %s subject %q: %w", provider, subject, err)
	}

	row.CurrentGeneration = current
	row.CreatorCurrentGeneration = creatorCurrent
	row.RootCurrentGeneration = rootCurrent

	return row, nil
}

// Save writes a pairing, refusing a subject already paired with
// auth.ErrPairingExists.
//
// The existence check is a read before the insert, not a parse of the
// constraint violation, because the two engines word that violation
// differently. The read costs one indexed row.
func (p *Pairings) Save(ctx context.Context, pairing auth.Pairing) error {
	if err := pairing.Validate(); err != nil {
		return err
	}

	_, err := p.Lookup(ctx, pairing.Provider, pairing.Subject)

	switch {
	case err == nil:
		return auth.ErrPairingExists
	case !errors.Is(err, auth.ErrNoPairing):
		return err
	}

	_, err = p.db.ExecContext(ctx, p.engine.Q(insertPairingSQL),
		pairing.Provider, pairing.Subject, pairing.Principal, pairing.Label, pairing.CreatedAt.UTC(),
		pairing.Generation, pairing.CreatedBy, pairing.CreatorGeneration, pairing.RootCreator, pairing.RootGeneration)
	if err != nil {
		return fmt.Errorf("save the pairing of %s subject %q: %w", pairing.Provider, pairing.Subject, err)
	}

	return nil
}

// List returns every pairing, or one provider's, ordered by provider then
// subject.
func (p *Pairings) List(ctx context.Context, provider string) ([]auth.Pairing, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if provider == "" {
		rows, err = p.db.QueryContext(ctx, p.engine.Q(selectPairingsSQL))
	} else {
		rows, err = p.db.QueryContext(ctx, p.engine.Q(selectProviderPairingsSQL), provider)
	}

	if err != nil {
		return nil, fmt.Errorf("list the pairings: %w", err)
	}

	defer rows.Close()

	out := make([]auth.Pairing, 0)

	for rows.Next() {
		pairing, scanErr := scanPairing(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("read a pairing: %w", scanErr)
		}

		out = append(out, pairing)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list the pairings: %w", err)
	}

	return out, nil
}

// Delete removes one pairing, or reports auth.ErrNoPairing.
func (p *Pairings) Delete(ctx context.Context, provider, subject string) error {
	result, err := p.db.ExecContext(ctx, p.engine.Q(deletePairingSQL), provider, subject)
	if err != nil {
		return fmt.Errorf("delete the pairing of %s subject %q: %w", provider, subject, err)
	}

	removed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count the pairings deleted: %w", err)
	}

	if removed == 0 {
		return auth.ErrNoPairing
	}

	return nil
}

// scanPairing reads one row in the column order pairingColumns names.
func scanPairing(row scanner, extra ...any) (auth.Pairing, error) {
	var (
		pairing auth.Pairing
		created time.Time
	)

	dest := append([]any{&pairing.Provider, &pairing.Subject, &pairing.Principal, &pairing.Label, &created,
		&pairing.Generation, &pairing.CreatedBy, &pairing.CreatorGeneration, &pairing.RootCreator,
		&pairing.RootGeneration}, extra...)

	err := row.Scan(dest...)
	if err != nil {
		return auth.Pairing{}, err
	}

	pairing.CreatedAt = created.UTC()

	return pairing, nil
}

// DeletePrincipal removes every pairing to one principal below its current
// generation and reports how many. Removing none is not an error.
func (p *Pairings) DeletePrincipal(ctx context.Context, principal string) (int, error) {
	if err := auth.ValidSubject(principal); err != nil {
		return 0, err
	}

	result, err := p.db.ExecContext(ctx, p.engine.Q(deletePrincipalPairingsSQL), principal, principal)
	if err != nil {
		return 0, fmt.Errorf("delete the pairings to %q: %w", principal, err)
	}

	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count the pairings to %q deleted: %w", principal, err)
	}

	return int(removed), nil
}

// DeleteCreatedBy removes every pairing one person wrote below their current
// generation and reports how many. Removing none is not an error.
func (p *Pairings) DeleteCreatedBy(ctx context.Context, creator string) (int, error) {
	if err := auth.ValidSubject(creator); err != nil {
		return 0, err
	}

	result, err := p.db.ExecContext(ctx, p.engine.Q(deleteCreatedPairingsSQL), creator, creator, creator, creator)
	if err != nil {
		return 0, fmt.Errorf("delete the pairings %q wrote: %w", creator, err)
	}

	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count the pairings %q wrote that were deleted: %w", creator, err)
	}

	return int(removed), nil
}

// CountWithoutCreator counts the pairings that name nobody as their creator,
// which an older build wrote. A password change cannot find them, so the shell
// reset tells the operator.
func (p *Pairings) CountWithoutCreator(ctx context.Context) (int, error) {
	var count int

	if err := p.db.QueryRowContext(ctx, p.engine.Q(countPairingsWithoutCreatorSQL)).Scan(&count); err != nil {
		return 0, fmt.Errorf("count the pairings with no creator: %w", err)
	}

	return count, nil
}
