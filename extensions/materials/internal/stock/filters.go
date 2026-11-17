package stock

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// MaxFilters bounds the filters one person keeps, in bytes of JSON.
const MaxFilters = 2048

// Filters answers the list filters principal kept last, or an empty object
// for a person who kept none.
func (s *Store) Filters(ctx context.Context, principal string) (json.RawMessage, error) {
	var kept string

	err := s.db.QueryRowContext(ctx, `SELECT filters FROM list_filters WHERE principal = $1`, principal).Scan(&kept)
	if errors.Is(err, sql.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}

	if err != nil {
		return nil, fmt.Errorf("read filters: %w", err)
	}

	return json.RawMessage(kept), nil
}

// KeepFilters keeps principal's list filters in place of the last ones. The
// page owns what is in them; the module keeps any JSON object of at most
// MaxFilters bytes and refuses the rest with ErrInvalid.
func (s *Store) KeepFilters(ctx context.Context, principal string, filters json.RawMessage) error {
	trimmed := bytes.TrimSpace(filters)
	if len(trimmed) > MaxFilters || !json.Valid(trimmed) || len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("filters are not a JSON object of at most %d bytes: %w", MaxFilters, ErrInvalid)
	}

	_, err := s.db.ExecContext(ctx, `INSERT INTO list_filters (principal, filters, updated_at) VALUES ($1, $2, $3)
		ON CONFLICT (principal) DO UPDATE SET filters = excluded.filters, updated_at = excluded.updated_at`,
		principal, string(trimmed), s.at(time.Now()))
	if err != nil {
		return fmt.Errorf("keep filters: %w", err)
	}

	return nil
}
