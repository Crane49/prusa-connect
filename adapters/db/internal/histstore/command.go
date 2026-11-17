package histstore

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// The three upserts of the command projection. Each writes only its own
// columns of the row, so the transitions of one command give the same row in
// any arrival order, and a replay changes nothing.
const (
	upsertRequestedSQL = `INSERT INTO history_command
		(command_id, ref, verb, key, requested_us, args, args_digest)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (command_id) DO UPDATE SET
			ref = EXCLUDED.ref, verb = EXCLUDED.verb, key = EXCLUDED.key,
			requested_us = EXCLUDED.requested_us, args = EXCLUDED.args,
			args_digest = EXCLUDED.args_digest`

	upsertAuthorizedSQL = `INSERT INTO history_command
		(command_id, ref, verb, authorized_us, auth_type, auth_id)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (command_id) DO UPDATE SET
			ref = EXCLUDED.ref, verb = EXCLUDED.verb,
			authorized_us = EXCLUDED.authorized_us,
			auth_type = EXCLUDED.auth_type, auth_id = EXCLUDED.auth_id`

	upsertSettledSQL = `INSERT INTO history_command
		(command_id, ref, verb, settled_us, status, ok, outcome_unknown,
		 machine_reason, fault_code, elapsed_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (command_id) DO UPDATE SET
			ref = EXCLUDED.ref, verb = EXCLUDED.verb,
			settled_us = EXCLUDED.settled_us, status = EXCLUDED.status,
			ok = EXCLUDED.ok, outcome_unknown = EXCLUDED.outcome_unknown,
			machine_reason = EXCLUDED.machine_reason,
			fault_code = EXCLUDED.fault_code, elapsed_ms = EXCLUDED.elapsed_ms`
)

// projectCommands folds the command transitions of one batch into
// history_command, one row per command. The row includes who authorized the
// command. The host decides which callers may read those columns.
func (s *Store) projectCommands(ctx context.Context, tx *sql.Tx, facts []historyapi.Fact) error {
	for _, fact := range facts {
		statement, args, folds := commandUpsert(fact)
		if !folds {
			continue
		}

		if _, err := tx.ExecContext(ctx, s.dialect.render(statement), args...); err != nil {
			return err
		}
	}

	return nil
}

// commandUpsert returns the statement that folds one transition. folds is
// false for any other fact, and for a payload that does not decode or has no
// command id.
func commandUpsert(fact historyapi.Fact) (statement string, args []any, folds bool) {
	switch fact.Type {
	case event.TypeDeviceCommandRequested:
		var payload event.CommandRequested
		if json.Unmarshal(fact.Payload, &payload) != nil || payload.CommandID == "" {
			return "", nil, false
		}

		rendered, err := json.Marshal(payload.Args)
		if err != nil {
			return "", nil, false
		}

		return upsertRequestedSQL, []any{
			payload.CommandID, fact.Ref.String(), payload.Verb, payload.Key,
			fact.At.UnixMicro(), string(rendered), payload.ArgsDigest,
		}, true
	case event.TypeDeviceCommandAuthorized:
		var payload event.CommandAuthorized
		if json.Unmarshal(fact.Payload, &payload) != nil || payload.CommandID == "" {
			return "", nil, false
		}

		return upsertAuthorizedSQL, []any{
			payload.CommandID, fact.Ref.String(), payload.Verb,
			fact.At.UnixMicro(), payload.AuthType, payload.AuthID,
		}, true
	case event.TypeDeviceCommandSettled:
		var payload event.CommandSettled
		if json.Unmarshal(fact.Payload, &payload) != nil || payload.CommandID == "" {
			return "", nil, false
		}

		return upsertSettledSQL, []any{
			payload.CommandID, fact.Ref.String(), payload.Verb,
			fact.At.UnixMicro(), payload.Status, payload.OK, payload.OutcomeUnknown,
			payload.MachineReason, payload.FaultCode, payload.ElapsedMS,
		}, true
	}

	return "", nil, false
}
