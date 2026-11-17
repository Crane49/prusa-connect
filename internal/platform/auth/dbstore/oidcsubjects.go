package dbstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/platform/auth"
)

// The statements over the subject table, written once with '?' for both
// engines.
const (
	oidcSubjectColumns = `principal, issuer, subject, display_name, email, first_seen, last_seen`

	// first_seen is written once. Every later sign-in replaces the rest.
	recordOIDCSubjectSQL = `INSERT INTO auth_oidc_subject (` + oidcSubjectColumns + `)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (principal) DO UPDATE SET issuer = excluded.issuer, subject = excluded.subject,
			display_name = excluded.display_name, email = excluded.email, last_seen = excluded.last_seen`

	selectOIDCSubjectSQL = `SELECT ` + oidcSubjectColumns + ` FROM auth_oidc_subject WHERE principal = ?`
)

// OIDCSubjects is auth.OIDCSubjects over the host's database.
type OIDCSubjects struct {
	db     *sql.DB
	engine Engine
}

var _ auth.OIDCSubjects = (*OIDCSubjects)(nil)

// NewOIDCSubjects returns the subject store over an open handle.
func NewOIDCSubjects(db *sql.DB, engine Engine) (*OIDCSubjects, error) {
	if db == nil {
		return nil, errors.New("dbstore: a subject store needs an open database handle")
	}

	if err := checkEngine(engine); err != nil {
		return nil, err
	}

	return &OIDCSubjects{db: db, engine: engine}, nil
}

// Record writes a person at a sign-in.
func (s *OIDCSubjects) Record(ctx context.Context, subject auth.OIDCSubject) error {
	if subject.Principal == "" || subject.Issuer == "" || subject.Subject == "" {
		return errors.New("dbstore: a subject record needs a principal, an issuer and a subject")
	}

	_, err := s.db.ExecContext(ctx, s.engine.Q(recordOIDCSubjectSQL),
		subject.Principal, subject.Issuer, subject.Subject, subject.DisplayName, subject.Email,
		subject.FirstSeen.UTC(), subject.LastSeen.UTC())
	if err != nil {
		return fmt.Errorf("record the sign-in of %q: %w", subject.Principal, err)
	}

	return nil
}

// Get returns the person under a bare user id, or auth.ErrNoOIDCSubject.
func (s *OIDCSubjects) Get(ctx context.Context, principal string) (auth.OIDCSubject, error) {
	var found auth.OIDCSubject

	err := s.db.QueryRowContext(ctx, s.engine.Q(selectOIDCSubjectSQL), principal).Scan(
		&found.Principal, &found.Issuer, &found.Subject, &found.DisplayName, &found.Email,
		&found.FirstSeen, &found.LastSeen)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return auth.OIDCSubject{}, auth.ErrNoOIDCSubject
	case err != nil:
		return auth.OIDCSubject{}, fmt.Errorf("read the sign-in of %q: %w", principal, err)
	}

	found.FirstSeen = found.FirstSeen.UTC()
	found.LastSeen = found.LastSeen.UTC()

	return found, nil
}
