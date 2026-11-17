package devicestore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
)

// recordedAt is the instant every case in this file writes, chosen so that a
// timestamp that came back with the wrong location or with a truncated
// fractional part is a failure rather than a value close enough to pass.
var recordedAt = time.Date(2026, 8, 28, 9, 14, 22, 0, time.UTC)

// aRegisteredDevice puts one row in the directory, which is what makes a record
// for it writable at all.
func aRegisteredDevice(tb testing.TB, store fixture, id, name string) {
	tb.Helper()

	require.NoError(tb, store.UpsertDevices(tb.Context(), authz.DeviceRow{ID: id, Name: name}))
}

// aFullRecord is one device with every column carrying something, so a column
// dropped from the write or from the read is a difference rather than two
// zero values agreeing.
func aFullRecord(id string) DeviceRecord {
	return DeviceRecord{
		DeviceID:  id,
		Driver:    "connector:xbuddy",
		Vendor:    "Prusa",
		Model:     "MK4",
		Serial:    "SN-" + id,
		UnitUID:   "uid-" + id,
		Firmware:  "6.2.4",
		Departure: "",
		LastSeen:  recordedAt,
		State: map[string]json.RawMessage{
			"/state":                json.RawMessage(`"PRINTING"`),
			"/tools/0/nozzle_temp":  json.RawMessage(`215.4`),
			"/job/progress":         json.RawMessage(`41`),
			"/dialog/id":            json.RawMessage(`null`),
			"/storage/usb/readonly": json.RawMessage(`false`),
		},
		Tags: []string{"workshop", "PETG only", "night shift"},
	}
}

// TestADeviceRecordSurvivesAFreshHandle is the store half of the restart. The
// database is written through one handle, that handle is closed, and the record
// is read back through another one, which is what a process that has stopped
// and started leaves behind.
//
// Writing and reading through one open Store would prove nothing about a
// restart: every value would still be in this process's memory and in the
// engine's page cache, and a store that held its records in a map would pass.
func TestADeviceRecordSurvivesAFreshHandle(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		handles := e.OpenShared(t, 2)

		first := openFixture(t, e, migrated(t, e, handles[0]))

		aRegisteredDevice(t, first, "p-alpha", "Alpha")
		require.NoError(t, first.SaveDeviceRecords(t.Context(), aFullRecord("p-alpha")))
		require.NoError(t, handles[0].Close())

		// The start a second process makes against the same database.
		second := openFixture(t, e, migrated(t, e, handles[1]))

		// The device was written while the first process still held it, which is
		// what the count of unfinished sessions reports to the next start.
		unfinished, err := second.UnfinishedDeviceRecords(t.Context())
		require.NoError(t, err)
		require.EqualValues(t, 1, unfinished,
			"the start did not notice that the last run left a session open")

		records, err := second.DeviceRecords(t.Context())
		require.NoError(t, err)
		require.Len(t, records, 1)

		found := records[0]

		want := aFullRecord("p-alpha")

		require.Equal(t, want.DeviceID, found.DeviceID)
		require.Equal(t, want.Driver, found.Driver)
		require.Equal(t, want.Vendor, found.Vendor)
		require.Equal(t, want.Model, found.Model)
		require.Equal(t, want.Serial, found.Serial)
		require.Equal(t, want.UnitUID, found.UnitUID)
		require.Equal(t, want.Firmware, found.Firmware)
		require.True(t, recordedAt.Equal(found.LastSeen), "last_seen came back as %s", found.LastSeen)

		// The document is compared decoded and never as bytes: Postgres
		// normalises jsonb on the way in, so the two engines hand back
		// different spellings of one value.
		require.Len(t, found.State, len(want.State))

		for path, value := range want.State {
			require.JSONEqf(t, string(value), string(found.State[path]), "the state tree lost %s", path)
		}

		// The tags are what an operator typed, so a restart that loses them
		// loses somebody's work rather than a reading the machine will send
		// again. Their order is the operator's and has to survive too.
		require.Equal(t, want.Tags, found.Tags,
			"the tags did not survive the restart")
	})
}

// TestTheDeviceRecordHasNoPresenceColumn is a design requirement read off the schema
// rather than off a start-up statement.
//
// Earlier research refused the column when the table was built. A stored presence has
// one legal value on the way back out, because the socket it described closed
// with the process that wrote it, and a column with one legal value is not a
// column. Storing it and clearing it at boot leaves the wrong answer reachable
// by anything that reads the table before the clearing statement runs, or by a
// build that forgets to run it. Not holding it makes restoring presence
// impossible rather than merely forbidden.
//
// It asks the engine and not the migration text, so a later migration that adds
// the column back fails here whatever the file it was added in says.
func TestTheDeviceRecordHasNoPresenceColumn(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		var presence bool

		err := store.db.QueryRowContext(t.Context(),
			`SELECT connected FROM device_record LIMIT 1`).Scan(&presence)

		require.Error(t, err, "device_record holds a presence column, which can only ever read back false")
		require.NotErrorIs(t, err, sql.ErrNoRows,
			"the column exists and the table is merely empty, which is not the property under test")
	})
}

// TestAnUnfinishedSessionIsCountedAtTheNextStart is what replaced the presence
// column. departure is written as a session ends, so a row with an empty one is
// a row the last process wrote while it still held the machine. A clean stop
// leaves none, and a count above zero is a kill -9 or a power cut.
func TestAnUnfinishedSessionIsCountedAtTheNextStart(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		aRegisteredDevice(t, store, "p-alpha", "Alpha")
		aRegisteredDevice(t, store, "p-beta", "Beta")

		// One machine the process was still holding, and one it wrote down.
		open := aFullRecord("p-alpha")
		closed := aFullRecord("p-beta")
		closed.Departure = "session_ended"

		require.NoError(t, store.SaveDeviceRecords(t.Context(), open, closed))

		unfinished, err := store.UnfinishedDeviceRecords(t.Context())
		require.NoError(t, err)
		require.EqualValues(t, 1, unfinished,
			"the count did not tell the machine that was still held from the one written down")

		// The same fleet after a clean stop.
		open.Departure = "server_stopping"
		require.NoError(t, store.SaveDeviceRecords(t.Context(), open))

		unfinished, err = store.UnfinishedDeviceRecords(t.Context())
		require.NoError(t, err)
		require.Zero(t, unfinished, "a clean stop was reported as a crash")
	})
}

// TestARecordCannotNameADeviceTheDirectoryDoesNotHold is the whole answer to
// which store says what exists. authz_device is the directory; this table is
// state about a row in it, and the database refuses a record with no row above
// it rather than leaving two lists that can disagree.
func TestARecordCannotNameADeviceTheDirectoryDoesNotHold(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		err := store.SaveDeviceRecords(t.Context(), aFullRecord("p-nobody"))
		require.ErrorIs(t, err, ErrDeviceRecordUnknownDevice)
		require.ErrorContains(t, err, "p-nobody")

		records, readErr := store.DeviceRecords(t.Context())
		require.NoError(t, readErr)
		require.Empty(t, records, "the refused record was written anyway")
	})
}

// TestABatchIsRefusedWholeIsTheTransactionHalf holds the flush to one
// transaction. The flush hands this call every device that changed, and a batch
// that committed the rows before the bad one would leave the store holding half
// a flush with nothing recording which half.
func TestABatchIsRefusedWholeIsTheTransactionHalf(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		aRegisteredDevice(t, store, "p-alpha", "Alpha")

		err := store.SaveDeviceRecords(t.Context(),
			aFullRecord("p-alpha"), aFullRecord("p-nobody"))
		require.ErrorIs(t, err, ErrDeviceRecordUnknownDevice)

		records, readErr := store.DeviceRecords(t.Context())
		require.NoError(t, readErr)
		require.Empty(t, records, "the row before the refused one was committed on its own")
	})
}

// TestUnregisteringADeviceTakesItsRecord is the cascade. An operator-chosen id
// is reusable after a replacement, and RemoveDevice already deletes every tuple
// written on it for that reason. A record left behind would hand the machine
// that replaced it the model, the serial and the last state of the one that
// went.
func TestUnregisteringADeviceTakesItsRecord(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		aRegisteredDevice(t, store, "p-alpha", "Alpha")
		aRegisteredDevice(t, store, "p-beta", "Beta")
		require.NoError(t, store.SaveDeviceRecords(t.Context(),
			aFullRecord("p-alpha"), aFullRecord("p-beta")))

		require.NoError(t, store.RemoveDevice(t.Context(), "p-alpha"))

		records, err := store.DeviceRecords(t.Context())
		require.NoError(t, err)
		require.Len(t, records, 1, "the record outlived the directory row it describes")
		require.Equal(t, "p-beta", records[0].DeviceID)
	})
}

// TestARecordIsOverwrittenAndNeverDuplicated is the upsert. The plane writes
// the same device again on every flush, so a second row per device would be a
// store that grows without bound at the frame rate.
func TestARecordIsOverwrittenAndNeverDuplicated(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		aRegisteredDevice(t, store, "p-alpha", "Alpha")
		require.NoError(t, store.SaveDeviceRecords(t.Context(), aFullRecord("p-alpha")))

		later := aFullRecord("p-alpha")
		later.Firmware = "6.3.0"
		later.Departure = "session_ended"
		later.LastSeen = recordedAt.Add(90 * time.Second)
		later.State = map[string]json.RawMessage{"/state": json.RawMessage(`"IDLE"`)}

		require.NoError(t, store.SaveDeviceRecords(t.Context(), later))

		records, err := store.DeviceRecords(t.Context())
		require.NoError(t, err)
		require.Len(t, records, 1)

		found := records[0]
		require.Equal(t, "6.3.0", found.Firmware)
		require.Equal(t, "session_ended", found.Departure)
		require.True(t, later.LastSeen.Equal(found.LastSeen))
		require.Len(t, found.State, 1, "the document was merged into the old one rather than replaced")
		require.JSONEq(t, `"IDLE"`, string(found.State["/state"]))
	})
}

// TestADeviceWithNoStateTreeStoresAnObject is the empty case, and it is here
// because a nil Go map marshals to null. The column's CHECK refuses that on
// both engines, so a device admitted before its first telemetry frame would
// otherwise be the one device that cannot be recorded.
func TestADeviceWithNoStateTreeStoresAnObject(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		aRegisteredDevice(t, store, "p-alpha", "Alpha")

		fresh := aFullRecord("p-alpha")
		fresh.State = nil

		require.NoError(t, store.SaveDeviceRecords(t.Context(), fresh))

		records, err := store.DeviceRecords(t.Context())
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Empty(t, records[0].State)
	})
}

// TestWritingNoRecordsOpensNoTransaction is the flush's quiet tick. A
// deployment whose printers are all idle flushes nothing every two seconds, and
// on SQLite an empty transaction is still the one write lock the whole process
// shares.
func TestWritingNoRecordsOpensNoTransaction(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		before := store.db.Stats()
		require.NoError(t, store.SaveDeviceRecords(t.Context()))
		require.Equal(t, before.WaitCount, store.db.Stats().WaitCount)

		records, err := store.DeviceRecords(t.Context())
		require.NoError(t, err)
		require.Empty(t, records)
	})
}

// TestAFleetIsWrittenInOnePassOverSeveralStatements is the batching, held to
// the boundary rather than to a fleet that happens to fit in one statement. The
// VALUES list carries recordBatchRows devices, so a fleet is several statements
// inside one transaction, and the case that breaks is the one where the last
// batch is short or a device sits exactly on the seam.
func TestAFleetIsWrittenInOnePassOverSeveralStatements(t *testing.T) {
	t.Parallel()

	// Two full statements and one short one, so both seams are crossed.
	const fleet = 2*recordBatchRows + 3

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		directory := make([]authz.DeviceRow, 0, fleet)
		batch := make([]DeviceRecord, 0, fleet)

		for i := range fleet {
			id := fmt.Sprintf("p-%03d", i)

			directory = append(directory, authz.DeviceRow{ID: id, Name: "Printer " + id})

			record := aFullRecord(id)
			record.Model = fmt.Sprintf("MK%d", i)
			batch = append(batch, record)
		}

		require.NoError(t, store.UpsertDevices(t.Context(), directory...))
		require.NoError(t, store.SaveDeviceRecords(t.Context(), batch...))

		records, err := store.DeviceRecords(t.Context())
		require.NoError(t, err)
		require.Len(t, records, fleet)

		// Every row carries its own values rather than the last batch's,
		// which is what a parameter list assembled off by one would lose.
		for _, record := range records {
			var index int

			_, err = fmt.Sscanf(record.DeviceID, "p-%03d", &index)
			require.NoError(t, err)
			require.Equalf(t, fmt.Sprintf("MK%d", index), record.Model,
				"%s carries another device's row", record.DeviceID)
			require.Equalf(t, "SN-"+record.DeviceID, record.Serial,
				"%s carries another device's row", record.DeviceID)
		}
	})
}

// TestOneDeviceNamedTwiceInOneBatchIsOneRow is the guard on the caller. A
// multi-row ON CONFLICT DO UPDATE refuses to touch one row twice inside one
// statement on Postgres, so a batch naming a device twice would be an error on
// one engine and the second row silently winning on the other.
func TestOneDeviceNamedTwiceInOneBatchIsOneRow(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		aRegisteredDevice(t, store, "p-alpha", "Alpha")

		first := aFullRecord("p-alpha")
		first.Firmware = "6.1.0"

		last := aFullRecord("p-alpha")
		last.Firmware = "6.3.0"

		require.NoError(t, store.SaveDeviceRecords(t.Context(), first, last))

		records, err := store.DeviceRecords(t.Context())
		require.NoError(t, err)
		require.Len(t, records, 1)
		require.Equal(t, "6.3.0", records[0].Firmware, "the earlier of the two writes won")
	})
}

// TestARecordWriteMovesNoEpoch is what keeps the flush off the authorization
// path. Every grant write bumps authz_epoch and recompiles the projection,
// which the stream hub reads on every subscriber's tick; a record write that
// went through the same path would move the generation of the graph four
// hundred times every two seconds and make every subscriber re-read its routing
// table for a temperature reading.
func TestARecordWriteMovesNoEpoch(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := newStore(t, e)

		aRegisteredDevice(t, store, "p-alpha", "Alpha")

		before := store.Epoch()

		stored := readEpoch(t, store.db)
		require.Equal(t, before, stored, "the cached epoch and the row disagree before anything ran")

		require.NoError(t, store.SaveDeviceRecords(t.Context(), aFullRecord("p-alpha")))

		_, err := store.UnfinishedDeviceRecords(t.Context())
		require.NoError(t, err)

		after := readEpoch(t, store.db)
		require.Equal(t, stored, after, "a record write moved the generation of the graph")
		require.Equal(t, before, store.Epoch())
	})
}
