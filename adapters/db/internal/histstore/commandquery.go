package histstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// commandColumns is the projection's columns, in the order scanCommand reads
// them.
const commandColumns = `command_id, ref, verb, key, requested_us, authorized_us,
	settled_us, status, ok, outcome_unknown, machine_reason, fault_code, elapsed_ms,
	auth_type, auth_id`

// QueryCommands returns command rows, newest requested first. The fact and
// sample queries go oldest first. Empty Refs means every origin, see
// historyapi.CommandQuery. The host narrows Refs to what the caller may read.
func (s *Store) QueryCommands(
	ctx context.Context, query historyapi.CommandQuery,
) (historyapi.CommandResult, error) {
	statement, args, err := commandStatement(query)
	if err != nil {
		return historyapi.CommandResult{}, err
	}

	limit := limitOf(query.Limit)

	rows, err := s.db.QueryContext(ctx, s.dialect.render(statement), args...)
	if err != nil {
		return historyapi.CommandResult{}, fmt.Errorf("histstore: read commands: %w", err)
	}

	defer func() { _ = rows.Close() }()

	commands := make([]historyapi.Command, 0, limit)

	var (
		lastAt int64
		lastID string
	)

	for rows.Next() {
		one, at, scanErr := scanCommand(rows)
		if scanErr != nil {
			return historyapi.CommandResult{}, scanErr
		}

		commands = append(commands, one)
		lastAt, lastID = at, one.ID
	}

	if err = rows.Err(); err != nil {
		return historyapi.CommandResult{}, fmt.Errorf("histstore: read commands: %w", err)
	}

	return historyapi.CommandResult{
		Commands: commands,
		Cursor:   cursorAfter(len(commands), limit, lastAt, lastID),
	}, nil
}

// commandStatement builds the page's statement and its arguments.
func commandStatement(query historyapi.CommandQuery) (string, []any, error) {
	var (
		text strings.Builder
		args []any
	)

	text.WriteString(`SELECT ` + commandColumns + ` FROM history_command WHERE requested_us >= ? AND requested_us < ?`)
	args = append(args, boundOf(query.From, minMicro), boundOf(query.To, maxMicro))

	if len(query.Refs) > 0 {
		text.WriteString(` AND ref IN (` + placeholders(len(query.Refs)) + `)`)

		for _, ref := range query.Refs {
			args = append(args, ref.String())
		}
	}

	if len(query.IDs) > 0 {
		text.WriteString(` AND command_id IN (` + placeholders(len(query.IDs)) + `)`)

		for _, id := range query.IDs {
			args = append(args, id)
		}
	}

	if query.OpenOnly {
		text.WriteString(` AND settled_us IS NULL`)
	}

	// The cursor is the (requested_us, command_id) pair, so two commands
	// requested in the same microsecond are not lost at a page boundary.
	if query.Cursor != "" {
		at, id, err := readCursor(query.Cursor)
		if err != nil {
			return "", nil, err
		}

		text.WriteString(` AND (requested_us < ? OR (requested_us = ? AND command_id < ?))`)
		args = append(args, at, at, id)
	}

	text.WriteString(` ORDER BY requested_us DESC, command_id DESC LIMIT ?`)
	args = append(args, limitOf(query.Limit))

	return text.String(), args, nil
}

// scanCommand reads one projection row and returns its requested instant for
// the cursor. Most columns are nullable, because each upsert fills only its
// own columns and the transitions can arrive in any order.
func scanCommand(rows *sql.Rows) (historyapi.Command, int64, error) {
	var (
		id         string
		reference  string
		verb       sql.NullString
		key        sql.NullString
		requested  sql.NullInt64
		authorized sql.NullInt64
		settled    sql.NullInt64
		status     sql.NullString
		ok         sql.NullBool
		unknown    sql.NullBool
		reason     sql.NullString
		faultCode  sql.NullString
		elapsedMS  sql.NullInt64
		authType   sql.NullString
		authID     sql.NullString
	)

	if err := rows.Scan(&id, &reference, &verb, &key, &requested, &authorized, &settled,
		&status, &ok, &unknown, &reason, &faultCode, &elapsedMS, &authType, &authID,
	); err != nil {
		return historyapi.Command{}, 0, fmt.Errorf("histstore: read a command row: %w", err)
	}

	origin, err := event.ParseOrigin(reference)
	if err != nil {
		return historyapi.Command{}, 0, fmt.Errorf(
			"histstore: a stored command names %q, which is no origin: %w", reference, err)
	}

	return historyapi.Command{
		ID:             id,
		Ref:            origin,
		Verb:           verb.String,
		Key:            key.String,
		Requested:      instantOf(requested),
		Authorized:     instantOf(authorized),
		Settled:        instantOf(settled),
		Status:         status.String,
		OK:             ok.Bool,
		OutcomeUnknown: unknown.Bool,
		MachineReason:  reason.String,
		FaultCode:      faultCode.String,
		ElapsedMS:      elapsedMS.Int64,
		AuthType:       authType.String,
		AuthID:         authID.String,
	}, requested.Int64, nil
}

// instantOf converts a nullable microsecond column. NULL gives the zero time,
// which historyapi defines as a transition the store never saw.
func instantOf(at sql.NullInt64) time.Time {
	if !at.Valid {
		return time.Time{}
	}

	return time.UnixMicro(at.Int64).UTC()
}
