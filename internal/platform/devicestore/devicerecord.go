package devicestore

// The device record: what a restart remembers about a device that has dialled
// this server.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	// A record write is not a grant write. It bumps no epoch and recompiles no
	// projection, because nothing it touches decides who may see what. A fleet
	// flushes every two seconds, and an epoch bump on each flush would move
	// the generation the stream hub compares on every subscriber's tick.
	//
	// The VALUES list is built for as many devices as one statement carries.
	// On Postgres the cost of a write is the round trip and not the commit.
	// One statement per device inside one transaction would save the commits
	// and still pay every round trip.
	upsertDeviceRecordPrefix = `INSERT INTO device_record
		(device_id, driver, vendor, model, serial, unit_uid,
		 firmware, departure, last_seen, state, current_job_id,
		 state_document, tags, retired_reason, retired_at, name)
		VALUES `

	upsertDeviceRecordSuffix = `
		ON CONFLICT (device_id) DO UPDATE SET
			driver = excluded.driver,
			vendor = excluded.vendor,
			model = excluded.model,
			serial = excluded.serial,
			unit_uid = excluded.unit_uid,
			firmware = excluded.firmware,
			departure = excluded.departure,
			last_seen = excluded.last_seen,
			state = excluded.state,
			current_job_id = excluded.current_job_id,
			state_document = excluded.state_document,
			tags = excluded.tags,
			retired_reason = excluded.retired_reason,
			retired_at = excluded.retired_at,
			name = excluded.name`

	selectDeviceRecordsSQL = `SELECT device_id, driver, vendor, model, serial, unit_uid,
			firmware, departure, last_seen, state, current_job_id,
			state_document, tags, retired_reason, retired_at, name
		FROM device_record
		ORDER BY device_id`

	// The unclean-stop count. departure is written as a session ends, so an
	// empty one is a row whose writer still held the machine, and a clean stop
	// leaves none. It is a full scan of a table this product bounds at four
	// hundred rows, run once before anything serves.
	countUnfinishedDeviceRecordsSQL = `SELECT COUNT(*) FROM device_record WHERE departure = ''`
)

const (
	// recordColumns is how many parameters one device costs.
	recordColumns = 16

	// recordBatchRows is how many devices one statement carries. Sixty-two rows
	// of sixteen columns is 992 parameters. That is inside the smallest limit
	// either engine has shipped: SQLite's SQLITE_MAX_VARIABLE_NUMBER was 999
	// before 3.32, and Postgres takes 65535. A fleet of four hundred in one
	// statement would be 6,400 parameters and would depend on which limit this
	// build's SQLite was compiled with. Adding a column to the row means
	// lowering this number so the product stays under 999.
	recordBatchRows = 62
)

// emptyDocument is what a device with no state tree stores. A nil Go map
// marshals to null, which is not an object and which the CHECK on both engines
// refuses, so the empty case is written here and not left to encoding/json.
const emptyDocument = "{}"

// emptyList is what a device with no tags stores, for the same reason. A nil
// slice marshals to null, which is not an array.
const emptyList = "[]"

// ErrDeviceRecordUnknownDevice reports a record written for a device the
// directory does not hold. The foreign key refuses it: authz_device is the
// only list of the devices that exist, and a record is state about a row in
// it.
var ErrDeviceRecordUnknownDevice = errors.New("devicestore: no device is registered under that id")

// DeviceRecord is one device as a restart finds it: what admission learned
// about the machine, the latest state the plane observed, and the state tree
// the driver's patches built up.
//
// The two halves follow Kubernetes' spec and status. A record column changes
// when somebody or something registers the device. A latest-state column is
// overwritten by the machine, and its previous value is an event a history
// provider may have caught.
type DeviceRecord struct {
	// DeviceID is the directory row this record is about.
	DeviceID string

	// Driver is the connector holding the device, as its ref renders.
	Driver string

	// What the machine said it is, at the last admission.
	Vendor  string
	Model   string
	Serial  string
	UnitUID string

	// Firmware is latest state and not identity: it changes on a machine that
	// is otherwise the same one.
	Firmware string

	// Departure is why the last session ended, and is empty while it was open.
	// It is stored so that a restored device reports the reason the API's
	// closed set already carries instead of a new one.
	Departure string

	// LastSeen is when the device was last heard from. It is the fact a person
	// wants about a machine that is away, and the only one no live session can
	// supply.
	LastSeen time.Time

	// MachineState is the machine's own word for what it is doing, stored in
	// the column named state. It is spelled differently here because State
	// below is the whole tree, and one struct cannot hold two fields of that
	// name.
	//
	// /state in the tree is what the driver reported, and this is what the
	// plane made of it. A listing that filters a fleet by state filters on
	// this column, because the two engines index a JSON document differently.
	MachineState string

	// CurrentJobID is the printer_job row this device is working on, and is
	// empty when it is working on nothing. A device that comes back from a
	// restart mid-print carries the id of the job it was on, so the print
	// outlives the process.
	CurrentJobID string

	// State is the coalesced state tree, keyed by the paths the driver names.
	// It is open on purpose: a driver reporting a spindle load adds paths and
	// needs no migration to do it.
	State map[string]json.RawMessage

	// Tags are the plain words an operator puts on the device, in the order the
	// operator gave them. Nothing in internal/authz reads one, so a tag never
	// decides who may see what.
	Tags []string

	// RetiredReason is why this device id stopped being its machine's, and is
	// empty for every device that still is one. RetiredAt is when, and is zero
	// alongside an empty reason.
	//
	// It is identity and not state, so it is a pair of its own and not a value
	// of Departure. Departure says why one session ended, and the next session
	// overwrites it. This pair is written once, when a second registration
	// for the same fingerprint mints a second id. The retired row keeps every
	// history row, command record and grant filed under the old id.
	RetiredReason string
	RetiredAt     time.Time

	// Name is what an operator called this device, and is empty for one nobody
	// has named. It is the display name a screen puts at the top of a card,
	// beside the model and the serial the machine calls itself.
	//
	// It is on the record and not on the directory row, although that row
	// carries a name too. The directory's is rewritten from the reported model
	// and serial on every arrival, so a name a person typed would not survive
	// the machine coming back. Nothing in internal/authz reads this column: a
	// name decides no grant, and neither does a tag.
	Name string
}

// SaveDeviceRecords writes one record per device, creating the row or
// overwriting it, all in one transaction and in as few statements as the
// parameter limit allows.
//
// One transaction for the fleet matters on SQLite, which has one writer and
// pays for every commit. Few statements matter on Postgres, where the cost is
// the round trip. So the rows go in as a multi-row VALUES list, the shape the
// projection recompile uses.
//
// The flush that calls this hands it every device that changed since the last
// one. Writing nothing is not an error and opens no transaction.
func (s *Store) SaveDeviceRecords(ctx context.Context, records ...DeviceRecord) error {
	if len(records) == 0 {
		return nil
	}

	// One row per device, last write winning. ON CONFLICT DO UPDATE refuses to
	// touch one row twice inside a single statement on Postgres. A caller that
	// coalesced by device already holds one record each, so this only guards
	// against a caller that did not.
	deduplicated, err := deduplicateRecords(records)
	if err != nil {
		return err
	}

	return saveBatched(ctx, s, upsertBatch[storedRecord]{
		what:         "device record",
		rows:         deduplicated,
		prefix:       upsertDeviceRecordPrefix,
		suffix:       upsertDeviceRecordSuffix,
		columns:      recordColumns,
		perStatement: recordBatchRows,
		args:         recordArgs,
		classify:     s.recordWriteFailure,
	})
}

// upsertBatch is one multi-row upsert: the statement it is spelled with, the
// rows it carries, and how a refusal of it is read.
//
// It is a struct because eight positional parameters, six of them strings and
// integers, cannot be read at the call site.
type upsertBatch[T any] struct {
	// what names the thing being written, for the two failures that can only
	// say which write it was: "device record", "printer job".
	what string

	rows   []T
	prefix string
	suffix string
	// columns is how many parameters one row costs, and perStatement is how
	// many rows one statement carries. Their product is the parameter count,
	// and it has to stay under the smallest limit either engine ships.
	columns      int
	perStatement int

	// args flattens one batch into the parameter list its VALUES rows take.
	args func([]T) []any
	// classify turns the engine's refusal into the one a caller can act on. It
	// runs after the transaction is over, because Postgres refuses every
	// further statement on an aborted one and a classification reads the
	// directory.
	classify func(context.Context, []T, error) error
}

// saveBatched writes one upsert batch inside one transaction, in as few
// statements as the parameter limit allows.
//
// One transaction matters on SQLite, which has one writer and pays for every
// commit. Few statements matter on Postgres, where the cost is the round trip.
func saveBatched[T any](ctx context.Context, s *Store, write upsertBatch[T]) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return failure("begin the "+write.what+" write", err)
	}

	defer func() { _ = tx.Rollback() }() // a rollback after a commit is a no-op

	for batch := range slices.Chunk(write.rows, write.perStatement) {
		statement := s.q(write.prefix +
			valuesRows(len(batch), write.columns) + write.suffix)

		if _, err = tx.ExecContext(ctx, statement, write.args(batch)...); err != nil {
			_ = tx.Rollback()

			return write.classify(ctx, batch, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return failure("commit the "+write.what+" write", err)
	}

	return nil
}

// storedRecord is one record with its two documents already encoded, which is
// done before the transaction opens: a value that will not marshal is the
// caller's mistake and must not be found with the write lock held.
type storedRecord struct {
	record   DeviceRecord
	document string
	tags     string
}

// deduplicateRecords returns one entry per device in the order the devices
// first appeared, carrying the last record given for each.
func deduplicateRecords(records []DeviceRecord) ([]storedRecord, error) {
	at := make(map[string]int, len(records))
	out := make([]storedRecord, 0, len(records))

	for _, record := range records {
		if record.DeviceID == "" {
			return nil, errors.New("devicestore: a device record names no device")
		}

		document, err := record.document()
		if err != nil {
			return nil, err
		}

		tags, err := record.tagDocument()
		if err != nil {
			return nil, err
		}

		stored := storedRecord{record: record, document: document, tags: tags}

		if index, seen := at[record.DeviceID]; seen {
			out[index] = stored

			continue
		}

		at[record.DeviceID] = len(out)
		out = append(out, stored)
	}

	return out, nil
}

// recordArgs is one batch flattened into the parameter list its VALUES rows
// take, in column order.
func recordArgs(batch []storedRecord) []any {
	out := make([]any, 0, len(batch)*recordColumns)

	for _, stored := range batch {
		out = append(out,
			stored.record.DeviceID, stored.record.Driver, stored.record.Vendor,
			stored.record.Model, stored.record.Serial, stored.record.UnitUID,
			stored.record.Firmware, stored.record.Departure,
			stored.record.LastSeen.UTC(), stored.record.MachineState,
			stored.record.CurrentJobID, stored.document, stored.tags,
			stored.record.RetiredReason, retiredAt(stored.record.RetiredAt),
			stored.record.Name)
	}

	return out
}

// valuesRows builds the VALUES list for rows rows of columns columns. Only
// placeholders are concatenated; no value ever reaches the query text.
func valuesRows(rows, columns int) string {
	var b strings.Builder

	for row := range rows {
		if row > 0 {
			b.WriteString(", ")
		}

		b.WriteByte('(')

		for column := range columns {
			if column > 0 {
				b.WriteString(", ")
			}

			b.WriteByte('?')
		}

		b.WriteByte(')')
	}

	return b.String()
}

// DeviceRecords reads every record back, in the directory's id order.
//
// It returns what the store holds and joins nothing: a caller that wants to
// know which devices exist asks the directory. No row it returns says anything
// about presence, because no column holds it: a device read back is away, and
// last_seen is the fact a person wants about it.
func (s *Store) DeviceRecords(ctx context.Context) ([]DeviceRecord, error) {
	rows, err := s.db.QueryContext(ctx, s.q(selectDeviceRecordsSQL))
	if err != nil {
		return nil, failure("read the device records", err)
	}

	defer func() { _ = rows.Close() }() // the scan error below is the one worth reporting

	var out []DeviceRecord

	for rows.Next() {
		record, scanErr := scanDeviceRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}

		out = append(out, record)
	}

	if err = rows.Err(); err != nil {
		return nil, failure("read the device records", err)
	}

	return out, nil
}

// UnfinishedDeviceRecords counts the rows whose last write happened while a
// session was still open, which is what a start after a crash finds.
//
// No column stores presence. A stored connected flag would be wrong after
// every crash, and a start-up statement would have to clear it. Departure
// carries the same fact. It is written as the session ends, so a row with an
// empty departure is a row the last process wrote while it still held the
// machine, and a clean stop leaves none.
//
// The count goes to a log line. It runs after Migrate and before anything
// serves.
func (s *Store) UnfinishedDeviceRecords(ctx context.Context) (int64, error) {
	var open int64

	err := s.db.QueryRowContext(ctx, s.q(countUnfinishedDeviceRecordsSQL)).Scan(&open)
	if err != nil {
		return 0, failure("count the devices whose session never ended", err)
	}

	return open, nil
}

// document is the state tree as the column stores it.
func (r DeviceRecord) document() (string, error) {
	if len(r.State) == 0 {
		return emptyDocument, nil
	}

	encoded, err := json.Marshal(r.State)
	if err != nil {
		return "", fmt.Errorf("encode the state tree of %s: %w", r.DeviceID, err)
	}

	return string(encoded), nil
}

// tagDocument is the tag list as the column stores it. A device carrying none
// stores an empty array and not null, as the CHECK on both engines requires.
func (r DeviceRecord) tagDocument() (string, error) {
	if len(r.Tags) == 0 {
		return emptyList, nil
	}

	encoded, err := json.Marshal(r.Tags)
	if err != nil {
		return "", fmt.Errorf("encode the tags of %s: %w", r.DeviceID, err)
	}

	return string(encoded), nil
}

// scanDeviceRecord reads one row of the record query.
func scanDeviceRecord(rows interface{ Scan(...any) error }) (DeviceRecord, error) {
	var (
		record   DeviceRecord
		document []byte
		tags     []byte
		retired  sql.NullTime
	)

	err := rows.Scan(
		&record.DeviceID, &record.Driver, &record.Vendor, &record.Model, &record.Serial,
		&record.UnitUID, &record.Firmware, &record.Departure,
		&record.LastSeen, &record.MachineState, &record.CurrentJobID,
		&document, &tags, &record.RetiredReason, &retired, &record.Name,
	)
	if err != nil {
		return DeviceRecord{}, failure("scan a device record", err)
	}

	// The two engines do not hand back the same bytes for one value: Postgres
	// normalises jsonb on the way in, so the document is decoded and never
	// compared or passed through as bytes.
	if err = json.Unmarshal(document, &record.State); err != nil {
		return DeviceRecord{}, failure("read the state tree of "+record.DeviceID, err)
	}

	if err = json.Unmarshal(tags, &record.Tags); err != nil {
		return DeviceRecord{}, failure("read the tags of "+record.DeviceID, err)
	}

	record.LastSeen = record.LastSeen.UTC()

	if retired.Valid {
		record.RetiredAt = retired.Time.UTC()
	}

	return record, nil
}

// retiredAt is the retirement moment as the column takes it: NULL for a device
// that was never retired, because "never" is not a moment and a zero timestamp
// is one. printer_job.ended is stored the same way.
func retiredAt(at time.Time) any {
	if at.IsZero() {
		return nil
	}

	return at.UTC()
}

// recordWriteFailure names the one refusal a caller can act on.
//
// A record for a device the directory does not hold breaks the foreign key to
// authz_device. Both drivers report that as a constraint violation, with
// different text and types. So the directory is asked about every device in
// the refused batch, and a device missing from it is the cause, whatever the
// driver called it. The statement carried up to recordBatchRows devices, and
// the engine's error does not say which of them failed.
func (s *Store) recordWriteFailure(ctx context.Context, batch []storedRecord, cause error) error {
	var unregistered []string

	for _, stored := range batch {
		var held string

		err := s.db.QueryRowContext(ctx, s.q(selectDeviceSQL), stored.record.DeviceID).Scan(&held)
		if errors.Is(err, sql.ErrNoRows) {
			unregistered = append(unregistered, stored.record.DeviceID)
		}
	}

	failure := failure("write the device records", cause)

	if len(unregistered) == 0 {
		return failure
	}

	return fmt.Errorf("%w: %s. Register the device before writing its record: authz_device is what "+
		"says which devices exist, and this table is state about a row in it: %w",
		ErrDeviceRecordUnknownDevice, strings.Join(unregistered, ", "), failure)
}
