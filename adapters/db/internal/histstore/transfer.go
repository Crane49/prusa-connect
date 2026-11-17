package histstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
	"github.com/prusa3d/prusa-connect/sdk/transferapi"
)

var _ transferapi.Records = (*Store)(nil)

// The two upserts of the transfer projection. Each writes only its own
// columns, so the two frames of one transfer give the same row in any arrival
// order, and a replay changes nothing.
const (
	upsertTransferStartedSQL = `INSERT INTO history_transfer
		(transfer_id, ref, storage, source, destination, bytes_total, started_ms, state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (transfer_id) DO UPDATE SET
			ref = EXCLUDED.ref, storage = EXCLUDED.storage, source = EXCLUDED.source,
			destination = EXCLUDED.destination, bytes_total = EXCLUDED.bytes_total,
			started_ms = EXCLUDED.started_ms,
			state = CASE WHEN history_transfer.ended_ms IS NULL
				THEN EXCLUDED.state ELSE history_transfer.state END`

	upsertTransferSettledSQL = `INSERT INTO history_transfer
		(transfer_id, ref, bytes_sent, bytes_total, state, reason, fault_code,
		 ended_ms, elapsed_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (transfer_id) DO UPDATE SET
			ref = EXCLUDED.ref, bytes_sent = EXCLUDED.bytes_sent,
			bytes_total = EXCLUDED.bytes_total, state = EXCLUDED.state,
			reason = EXCLUDED.reason, fault_code = EXCLUDED.fault_code,
			ended_ms = EXCLUDED.ended_ms, elapsed_ms = EXCLUDED.elapsed_ms`
)

// projectTransfers folds the started and settled transitions of one batch
// into history_transfer, one row per transfer. Progress is not folded: it is
// telemetry, replaced by the next progress event.
func (s *Store) projectTransfers(ctx context.Context, tx *sql.Tx, facts []historyapi.Fact) error {
	for _, fact := range facts {
		switch fact.Type {
		case event.TypeDeviceTransferStarted:
			var payload event.TransferStarted
			if json.Unmarshal(fact.Payload, &payload) != nil || payload.TransferID == "" {
				continue
			}

			if _, err := tx.ExecContext(ctx, s.dialect.render(upsertTransferStartedSQL),
				payload.TransferID, fact.Ref.String(), payload.Storage, payload.Source,
				payload.Destination, payload.BytesTotal, fact.At.UnixMilli(),
				transferapi.StateSending,
			); err != nil {
				return err
			}
		case event.TypeDeviceTransferSettled:
			var payload event.TransferSettled
			if json.Unmarshal(fact.Payload, &payload) != nil || payload.TransferID == "" {
				continue
			}

			if _, err := tx.ExecContext(ctx, s.dialect.render(upsertTransferSettledSQL),
				payload.TransferID, fact.Ref.String(), payload.BytesSent, payload.BytesTotal,
				payload.State, payload.Reason, payload.FaultCode,
				fact.At.UnixMilli(), payload.ElapsedMS,
			); err != nil {
				return err
			}
		}
	}

	return nil
}

// defaultTransferLimit is the page size when the caller gives none. The
// host's restart sweep reads every open transfer in one page, so it must stay
// above the number of transfers that can run at once.
const defaultTransferLimit = 1000

// selectTransfersSQL reads the projection. Most columns are nullable, because
// each frame writes only its own columns. COALESCE turns a NULL into the zero
// value transferapi.Record documents.
const selectTransfersSQL = `SELECT transfer_id, ref,
	COALESCE(storage, ''), COALESCE(source, ''), COALESCE(destination, ''),
	COALESCE(bytes_total, 0), COALESCE(bytes_sent, 0), COALESCE(state, ''),
	COALESCE(reason, ''), COALESCE(fault_code, ''),
	COALESCE(started_ms, 0), COALESCE(ended_ms, 0)
	FROM history_transfer`

// Transfers returns a page of the transfer projection, newest first. The
// transfer id breaks a tie between two transfers that started in the same
// millisecond, so the order is total.
func (s *Store) Transfers(ctx context.Context, query transferapi.Query) ([]transferapi.Record, error) {
	var (
		conditions []string
		args       []any
	)

	if query.Device != "" {
		conditions = append(conditions, `ref = ?`)
		args = append(args, query.Device)
	}

	// Running means no settled frame arrived. Test ended_ms, not the state:
	// a settled frame always writes ended_ms.
	if query.RunningOnly {
		conditions = append(conditions, `ended_ms IS NULL`)
	}

	text := selectTransfersSQL
	if len(conditions) > 0 {
		text += ` WHERE ` + strings.Join(conditions, ` AND `)
	}

	limit := transferLimitOf(query.Limit)

	text += ` ORDER BY started_ms DESC, transfer_id DESC LIMIT ?`
	args = append(args, limit)

	return s.readTransfers(ctx, text, args, limit)
}

// readTransfers runs one page of the transfer query.
func (s *Store) readTransfers(
	ctx context.Context, statement string, args []any, limit int,
) ([]transferapi.Record, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.render(statement), args...)
	if err != nil {
		return nil, fmt.Errorf("histstore: read transfers: %w", err)
	}

	defer func() { _ = rows.Close() }()

	records := make([]transferapi.Record, 0, limit)

	for rows.Next() {
		var record transferapi.Record

		if err = rows.Scan(
			&record.TransferID, &record.Device, &record.Storage, &record.Source,
			&record.Destination, &record.BytesTotal, &record.BytesSent, &record.State,
			&record.Reason, &record.FaultCode, &record.StartedMS, &record.EndedMS,
		); err != nil {
			return nil, fmt.Errorf("histstore: read a transfer row: %w", err)
		}

		records = append(records, record)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("histstore: read transfers: %w", err)
	}

	return records, nil
}

// transferLimitOf clamps a caller's page size, as limitOf does.
func transferLimitOf(asked int) int {
	switch {
	case asked <= 0:
		return defaultTransferLimit
	case asked > maxLimit:
		return maxLimit
	default:
		return asked
	}
}
