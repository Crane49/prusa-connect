package app

// The device plane's half of the record store: what it writes as a device
// changes, and what it reads back when the process starts.
//
// The in-memory entry stays the truth while this process is running. Nothing on
// the path from a printer's frame to a subscriber acquires a database
// connection: a frame marks its entry as changed and returns, and one goroutine
// writes every changed device in one transaction on a timer. A write per frame
// would cost about 1,600 statements a second for four hundred idle devices,
// and a write per patch about 24,000.
//
// What survives is the fleet: which devices this server has met, what they said
// they are, when each was last heard from, and the last state tree each
// reported. What does not survive is presence. A stored `connected` would be a
// claim about a socket that closed when the process holding it did, so the
// schema holds no presence column and a restored device is `away` with a
// last_seen.

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

const (
	// recordFlushInterval is how often the changed devices are written.
	//
	// Outbound frames are already coalesced to 5 to 10 Hz, and a stored row has
	// no reader who needs it sooner than a subscriber does. At the four hundred
	// devices this product claims it is one transaction every two seconds
	// however fast the machines report, which takes well under one per cent of
	// the SQLite writer. It is not the frame rate and must never become it.
	recordFlushInterval = 2 * time.Second

	// recordWriteTimeout bounds one flush. The flush runs on a goroutine of its
	// own, so a database that has stopped answering costs a log line and the
	// next tick rather than a plane that stops reading its printers.
	recordWriteTimeout = 10 * time.Second
)

// recordFields is the set of stored fields whose movement is worth a frame,
// held as a bit set so two flushes over one device merge without ordering.
//
// What is in it and what is not is the whole granularity decision. Every field
// here changes when somebody or something registers the device, when its link
// changes or when its session ends, which is the rate a machine changes state
// at and not the rate it reports at. The state document and last_seen are
// deliberately outside it: both move on every telemetry frame, so a fleet of
// four hundred reporting at 1 to 5 Hz would put one durable frame per device on
// the bus every flush, which is 200 a second into a replay window the hub bounds
// at 32 events and 32 KB per origin (`internal/transport/stream/hub.go`). Each
// device's window would then turn over every 64 seconds, and a reader resuming
// from before an arrival would be told its cursor is gone. Both are still
// published: the document one patch at a time as
// com.prusa.connect.device.telemetry.sampled, and last_seen on this frame,
// carried without ever causing one.
type recordFields uint16

const (
	fieldDriver recordFields = 1 << iota
	fieldVendor
	fieldModel
	fieldSerial
	fieldUnitUID
	fieldFirmware
	fieldDeparture
	fieldConnected
	fieldTags
	fieldRetired
	fieldName

	// everyRecordField is what the first frame about a device names. Nothing
	// was stated about it before, so every field the frame carries is new.
	everyRecordField = fieldDriver | fieldVendor | fieldModel | fieldSerial |
		fieldUnitUID | fieldFirmware | fieldDeparture | fieldConnected | fieldTags |
		fieldRetired | fieldName
)

// recordFieldNames maps the set onto the member names the frame carries. The
// order is fixed here rather than taken from the order things moved in, so two
// frames listing the same fields read the same.
var recordFieldNames = []struct {
	field recordFields
	name  string
}{
	{fieldDriver, "driver"},
	{fieldVendor, "vendor"},
	{fieldModel, "model"},
	{fieldSerial, "serial"},
	{fieldUnitUID, "unit_uid"},
	{fieldFirmware, "firmware"},
	{fieldDeparture, "departure"},
	{fieldConnected, "connected"},
	{fieldTags, "tags"},
	{fieldRetired, "retired"},
	{fieldName, "name"},
}

// names renders the set as the member names a subscriber reads.
func (f recordFields) names() []string {
	out := make([]string, 0, len(recordFieldNames))

	for _, known := range recordFieldNames {
		if f&known.field != 0 {
			out = append(out, known.name)
		}
	}

	return out
}

// recordFacts is what the frame reports about one device, kept on the entry so
// that the next write knows what moved. It holds no state tree, which is what
// keeps it a few words per device rather than a second copy of the fleet.
//
// connected is not a column of the stored row. It is read off the live entry,
// because a stored presence can only ever be read back as false. Presence is
// the field an operator watches most, so it stays a member of the frame and
// comes from the process that knows it.
type recordFacts struct {
	driver    string
	vendor    string
	model     string
	serial    string
	unitUID   string
	firmware  string
	departure string
	connected bool
	// tags is the list rendered as one comparable value rather than the slice
	// itself. This struct is compared field by field on every flush, and a
	// slice is not comparable; renderTags makes the list one word here.
	tags string
	// retired is why this device id stopped being its machine's, empty for one
	// that still is. The moment is not compared: it is written once, beside the
	// reason, so a frame that reports the reason has reported the change.
	retired string
	// name is what an operator called this device, empty for one nobody has
	// named. It is compared like every other field here, so a rename is one
	// frame and a write that changed nothing is none.
	name string
	// job is what the tree said about the print, kept here rather than in a map
	// on the plane so that a device's last-seen print lives exactly as long as
	// its entry does. movedFrom does not read it: see changedRecord.jobMoved
	// for why the job is not one of the fields a frame reports.
	job jobFacts
}

// factsOf reduces one record, plus the presence the entry answered with when it
// was built, to what a frame reports.
func factsOf(record devicestore.DeviceRecord, connected bool) recordFacts {
	return recordFacts{
		driver:    record.Driver,
		vendor:    record.Vendor,
		model:     record.Model,
		serial:    record.Serial,
		unitUID:   record.UnitUID,
		firmware:  record.Firmware,
		departure: record.Departure,
		connected: connected,
		tags:      renderTags(record.Tags),
		retired:   record.RetiredReason,
		name:      record.Name,
	}
}

// movedFrom is which fields differ from the ones a frame last stated. It runs
// once per device per flush, so it compares a few words and allocates nothing.
func (f recordFacts) movedFrom(stated recordFacts) recordFields {
	var moved recordFields

	moved |= movedBit(fieldDriver, f.driver != stated.driver)
	moved |= movedBit(fieldVendor, f.vendor != stated.vendor)
	moved |= movedBit(fieldModel, f.model != stated.model)
	moved |= movedBit(fieldSerial, f.serial != stated.serial)
	moved |= movedBit(fieldUnitUID, f.unitUID != stated.unitUID)
	moved |= movedBit(fieldFirmware, f.firmware != stated.firmware)
	moved |= movedBit(fieldDeparture, f.departure != stated.departure)
	moved |= movedBit(fieldConnected, f.connected != stated.connected)
	moved |= movedBit(fieldTags, f.tags != stated.tags)
	moved |= movedBit(fieldRetired, f.retired != stated.retired)
	moved |= movedBit(fieldName, f.name != stated.name)

	return moved
}

// movedBit is one bit, set when the comparison behind it says the value moved.
func movedBit(bit recordFields, moved bool) recordFields {
	if moved {
		return bit
	}

	return 0
}

// changedRecord is one device as a flush takes it: the row to write, the
// presence the entry answered with as it was taken, and which of the fields a
// frame reports moved since the last frame about this device.
//
// Presence rides here rather than on the record because the schema holds no
// column for it. The frame still reports it, so it is read once under the
// entry's lock beside everything else and carried to the publish.
type changedRecord struct {
	record    devicestore.DeviceRecord
	connected bool
	moved     recordFields
	// job is what the state tree said about the print on this machine as the
	// record was taken, and jobMoved is whether any of it differs from the last
	// time this entry was read.
	//
	// It is a flag of its own and not a bit in moved, because the two are about
	// different rates. moved names the fields a device.record.changed frame
	// reports, and every one of them moves when somebody registers the machine
	// or its link changes; progress moves on every telemetry frame, so a job bit
	// in that set would put one durable frame per printing device on the bus
	// every flush, which is the cost the comment on recordFields is about. The
	// job row is written on that rate deliberately: a stored progress that only
	// moved when the machine arrived would be no progress at all.
	job      jobFacts
	jobMoved bool
	// quiet is whether the machine reported IDLE or READY since the take
	// before this one, and printID is the print the flush matched the reading
	// to (printidentity.go).
	quiet   bool
	printID string
}

// records is the store the plane keeps the fleet in, internal/platform/
// devicestore over the host's database. device_record carries a foreign key
// onto authz_device, so the records live in the same database as the graph.
//
// It is an interface here so that this file names exactly what the plane uses
// and no more: nothing in the write path can reach for a grant.
type records interface {
	SaveDeviceRecords(ctx context.Context, records ...devicestore.DeviceRecord) error
	DeviceRecords(ctx context.Context) ([]devicestore.DeviceRecord, error)
	UnfinishedDeviceRecords(ctx context.Context) (int64, error)

	// The prints. They are on this interface and not on one of their own
	// because they are written by the same flush against the same handle:
	// printer_job carries the same foreign key device_record does, and a
	// second interface over the same store would be a second thing to wire.
	SaveDeviceJobs(ctx context.Context, jobs ...devicestore.PrinterJob) error
	DeviceJobs(ctx context.Context, deviceID string, limit int) ([]devicestore.PrinterJob, error)

	// The reads and the write that matching a reading to a print needs: the
	// print every device began last, one print by its id, and an end for a
	// print the machine replaced without saying how it ended.
	LatestDeviceJobs(ctx context.Context) (map[string]devicestore.PrinterJob, error)
	PrinterJobByID(ctx context.Context, jobID string) (devicestore.PrinterJob, bool, error)
	EndPrinterJob(ctx context.Context, jobID string, ended time.Time, outcome string) error

	// EndedJobs is the page a device page scrolls and the page a module reads:
	// the prints that have ended on one device, or on every device the subject
	// may observe, newest first, resumed from where the last page stopped. The
	// permission is resolved inside the query.
	EndedJobs(
		ctx context.Context, subject authz.Subject, window devicestore.JobWindow,
	) ([]devicestore.PrinterJob, error)

	// FleetJobs is the page the fleet's jobs route reads: the running and the
	// ended prints on every device the subject may observe, resolved inside
	// the query as EndedJobs is.
	FleetJobs(
		ctx context.Context, subject authz.Subject, filter devicestore.JobFilter,
	) ([]devicestore.PrinterJob, error)
}

// restore reads the fleet back and puts every device the store holds into the
// plane, then returns. It runs once, after the graph is migrated and before
// anything serves.
//
// Every device it loads is away, and no branch here decides that. The schema
// holds the rule: there is no presence column, so a restored device cannot
// claim a socket this process does not hold, and no start-up statement has to
// erase one. What the store does answer is how many
// rows the last run left with a session still open, which is a start after a
// crash and a line in the log.
//
// A restored device carries no session, so every operation that has to talk to
// the machine refuses it exactly as it refuses one whose session ended while
// this process watched. What a person gets is the fleet: the machine's id, its
// model and serial, when it was last heard from, and the last state it
// reported.
func (d *devices) restore(ctx context.Context) error {
	unfinished, err := d.records.UnfinishedDeviceRecords(ctx)
	if err != nil {
		return fmt.Errorf("read how the last run ended: %w", err)
	}

	if unfinished > 0 {
		// Every device whose session the last process never wrote an end for.
		// It is a warning rather than a fact, because a clean stop writes each
		// of them down as its session ends and a count above zero is what a
		// kill -9 or a power cut leaves.
		d.log.WarnContext(ctx, "the stored fleet still held open sessions, so the last run did not stop cleanly",
			"devices", unfinished)
	}

	stored, err := d.records.DeviceRecords(ctx)
	if err != nil {
		return fmt.Errorf("read the stored fleet: %w", err)
	}

	// Read before the plane's lock is taken, because it is a query.
	if err = d.loadPrints(ctx, stored); err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, record := range stored {
		// The identity binding comes back first and for every row, including a
		// row the departed table then drops. A device id is minted once per
		// machine, so what has to survive a restart is which id
		// a fingerprint already holds, and that outlives the last state tree
		// this process kept of it: departedLimit bounds the trees, and a fleet
		// larger than it must still hand every machine back its own id.
		//
		// A retired device is deliberately not bound. Its fingerprint belongs
		// to the device the transfer minted, and rebinding the retired id here
		// would hand the new owner the old owner's row at the next handshake.
		if record.UnitUID != "" && record.RetiredReason == "" {
			d.identities.bind(record.DeviceID, record.UnitUID)
			d.identities.meet(record.DeviceID)
		}

		if _, live := d.entries[record.DeviceID]; live {
			// A printer that dialled in while this was running is the printer
			// now, and its live entry is newer than anything on the disk.
			continue
		}

		d.evictDepartedLocked()

		d.departed[record.DeviceID] = restored(record, d.treeBudget())
	}

	d.log.InfoContext(ctx, "the fleet was read back from the record store",
		"devices", len(d.departed))

	return nil
}

// restored is one stored record as a plane entry. It holds no session, which is
// what makes every operation that needs the machine refuse it.
//
// It carries the retention budget and what the stored tree already spends of
// it. A device that comes back is filed as a new entry, so nothing patches
// this one. The budget is set anyway, because an entry with a zero budget
// would refuse every path if something did patch it.
func restored(record devicestore.DeviceRecord, budget int) *device {
	state := record.State
	if state == nil {
		state = map[string]json.RawMessage{}
	}

	spent := 0
	for path, value := range state {
		spent += len(path) + len(value)
	}

	return &device{
		budget:     budget,
		stateBytes: spent,
		tags:       record.Tags,
		name:       record.Name,
		id:         record.DeviceID,
		driver:     record.Driver,
		retired:    record.RetiredReason,
		retiredAt:  record.RetiredAt,
		identity: connectorapi.Identity{
			Vendor:   record.Vendor,
			Model:    record.Model,
			Serial:   record.Serial,
			UnitUID:  record.UnitUID,
			Firmware: connectorapi.NewFirmwareLabel(record.Firmware),
		},
		state:    state,
		gone:     restoredDeparture(record.Departure),
		goneAt:   record.LastSeen,
		lastSeen: record.LastSeen,
	}
}

// restoredDeparture is why a restored device says its session ended.
//
// The set on the wire is closed and holds two values, and this maps whatever
// the store holds onto it. A row written by a build that had a third word for
// it, or edited by hand, therefore cannot put a value on the API that no client
// can read. server_stopping is the fallback because it is the honest reading of
// a record whose session was still open: this server stopped, and the printer
// is not known to have gone anywhere.
func restoredDeparture(stored string) string {
	if stored == departureSessionEnded {
		return departureSessionEnded
	}

	return departureServerStopping
}

// flushRecords writes every changed device until the plane closes, then writes
// them one last time.
//
// One goroutine and one transaction. Four hundred upserts in one transaction
// cost between four and seven times less per row than one transaction each,
// and on SQLite every commit takes the single writer the whole process
// shares.
func (d *devices) flushRecords() {
	defer close(d.flushed)

	tick := time.NewTicker(recordFlushInterval)
	defer tick.Stop()

	// The fleet's gauges ride on this goroutine rather than one of their own.
	// Both are a walk of the same table on the same period, and a second
	// goroutine would be a second thing to stop.
	gauges := time.NewTicker(fleetGaugeInterval)
	defer gauges.Stop()

	sampler := newFleetSampler()

	for {
		select {
		case <-tick.C:
			d.flush()
		case <-gauges.C:
			d.sampleFleet(sampler)
		case <-d.flushNow:
			d.flush()
		case <-d.flushStop:
			// The last one. Every session has already been closed and every
			// pump has finished, so what is taken here is the departure each
			// of them recorded on the way out.
			d.flush()

			// The live table is empty by now, so this walk drops the series of
			// every printer rather than leaving a scrape of a stopping process
			// reporting a fleet that has gone.
			d.sampleFleet(sampler)

			return
		}
	}
}

// flush writes the devices that have changed since the last one, then says on
// the bus which of them moved a field a subscriber reads.
//
// A failed write is logged, and requeue puts its takes in front of the next
// flush. Nothing is published when the write fails, so a consumer never holds
// a record the store does not have.
//
// One flush runs at a time. A caller that returns from it knows that every
// change taken before it returned is in the store, whether this flush took
// it or the one it waited for did.
func (d *devices) flush() {
	d.flushing.Lock()
	defer d.flushing.Unlock()

	changed := d.takeChanged()
	if len(changed) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), recordWriteTimeout)
	defer cancel()

	// The print each reading belongs to is decided before the write, because
	// the record names the print it is running by the print's id. A write
	// that fails undoes the decision, so a print the machine replaced is not
	// forgotten while it is still open.
	kept := d.prints.keep(changed)
	abandoned := d.identifyPrints(changed)

	written := make([]devicestore.DeviceRecord, 0, len(changed))
	for _, taken := range changed {
		written = append(written, taken.record)
	}

	if err := d.records.SaveDeviceRecords(ctx, written...); err != nil {
		d.log.ErrorContext(ctx, "the fleet could not be written to the record store",
			"devices", len(changed), "error", err.Error())
		d.prints.restore(kept)
		d.requeue(changed)

		return
	}

	// A print the machine replaced is ended before its successor is written,
	// so its settled frame reaches the bus before the successor's started one.
	d.endAbandonedPrints(ctx, abandoned)
	d.writeJobs(ctx, changed)
	d.announce(ctx, changed)
}

// announce puts one frame on the bus per device whose stored record moved.
//
// It runs after the write and never before it, because the frame says the
// record store holds this row. Publishing first would tell a history provider
// about a record that the transaction then refused, and a provider that stores
// what the core stored would hold a row no second reader of that table can find.
//
// It is one frame per device and never one per flush. The hub routes on the
// origin and an origin is one device, so a frame naming eighty devices reaches
// a subscriber who may see one of them, or reaches nobody. That is not an
// efficiency argument: it is what makes the visibility answer possible at all.
func (d *devices) announce(ctx context.Context, written []changedRecord) {
	for _, taken := range written {
		if taken.moved == 0 {
			continue
		}

		d.publish(ctx, taken.record.DeviceID, event.TypeDeviceRecordChanged,
			event.ClassState, "", recordChangeOf(taken.record, taken.connected, taken.moved))
	}
}

// takeChanged returns one record per device that has something the store does
// not have yet, and clears the mark as it goes.
//
// It reads two places. The pending map holds records writeSoon captured as they
// happened, and the tables hold the devices that have been marked by a frame.
// A device is in both when it changed again after it was captured, and the one
// read out of the table is the newer of the two.
//
// The pending map exists for one case, and it is the case a restart is about.
// retire drops an entry from both tables during a shutdown, deliberately: a
// process that is stopping is not the moment to hold four hundred state trees
// for the length of the drain. A departure captured only as a mark on that
// entry would then reach no flush at all, and the last thing every machine
// reported would be lost exactly when the process is about to need it.
func (d *devices) takeChanged() []changedRecord {
	d.mu.Lock()

	captured := d.pending
	d.pending = map[string]changedRecord{}

	entries := make([]*device, 0, len(d.entries)+len(d.departed))
	for _, entry := range d.entries {
		entries = append(entries, entry)
	}

	for _, entry := range d.departed {
		entries = append(entries, entry)
	}

	d.mu.Unlock()

	for _, entry := range entries {
		taken, changed := entry.takeRecord()
		if !changed {
			continue
		}

		// The newer row wins and the two field sets are merged. A device
		// captured by writeSoon for a departure and then read out of the table
		// again is one row, and dropping the earlier set would lose exactly the
		// field an operator is watching for.
		if earlier, held := captured[taken.record.DeviceID]; held {
			taken.moved |= earlier.moved
			taken.jobMoved = taken.jobMoved || earlier.jobMoved
			taken.quiet = taken.quiet || earlier.quiet
		}

		captured[taken.record.DeviceID] = taken
	}

	out := make([]changedRecord, 0, len(captured))
	for _, taken := range captured {
		out = append(out, taken)
	}

	// One order, whatever order the maps above were walked in, so two flushes
	// take the write lock over the same rows in the same sequence and a failure
	// names the same device twice rather than a different one each time. The
	// frames the write is followed by leave in the same order for the same
	// reason.
	slices.SortFunc(out, func(a, b changedRecord) int {
		return strings.Compare(a.record.DeviceID, b.record.DeviceID)
	})

	return out
}

// takeRecord is this device as the store holds it, and reports false when
// nothing has changed since the last flush.
func (e *device) takeRecord() (changedRecord, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.changed {
		return changedRecord{}, false
	}

	return e.recordLocked(), true
}

// forceRecord is this device as the store holds it, whether or not anything
// changed, and it clears the mark.
func (e *device) forceRecord() changedRecord {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.recordLocked()
}

// recordLocked builds the record, works out which reported fields moved since
// the last frame about this device, and clears the mark. The caller holds e.mu.
//
// The comparison is against what a frame last stated and not against what was
// last written, because those are the same thing for every device that has one:
// a frame follows every successful write, and a write that failed left the
// store behind too.
func (e *device) recordLocked() changedRecord {
	e.changed = false

	// Presence is read here rather than through connected(), which takes this
	// same mutex. A departed entry is not connected whatever the closed session
	// under it answers, and a restored one holds no session at all.
	live := e.gone == "" && e.session != nil && e.session.Connected()

	// The print, read out of the same tree under the same lock, so the row and
	// the record it belongs to are one reading of the machine and not two.
	job := jobFactsOf(e.state)

	record := devicestore.DeviceRecord{
		DeviceID:      e.id,
		Driver:        e.driver,
		Vendor:        e.identity.Vendor,
		Model:         e.identity.Model,
		Serial:        e.identity.Serial,
		UnitUID:       e.identity.UnitUID,
		Firmware:      firmwareText(e.identity.Firmware),
		Departure:     e.gone,
		LastSeen:      e.lastSeen,
		RetiredReason: e.retired,
		RetiredAt:     e.retiredAt,
		Name:          e.name,
		MachineState:  job.printerState,
		State:         maps.Clone(e.state),
		Tags:          slices.Clone(e.tags),
	}

	// The first record about a device names every field it carries: nothing was
	// stated before it, so a consumer holds none of them.
	facts := factsOf(record, live)
	facts.job = job

	moved := everyRecordField
	if e.stated {
		moved = facts.movedFrom(e.announced)
	}

	// A device nothing has been stated about yet has a job to write if it has
	// one at all, which is the restored entry: its tree came back out of the
	// store and the row it names is the row that was already there.
	jobMoved := !e.stated || facts.job != e.announced.job

	e.announced, e.stated = facts, true

	quiet := e.quietSeen || quietState(job.printerState)
	e.quietSeen = false

	return changedRecord{
		record: record, connected: live, moved: moved, job: job, jobMoved: jobMoved, quiet: quiet,
	}
}

// requeue puts the takes of a flush whose write failed back in front of the
// next one. A take captured since is newer and its record wins, with the flags
// of both, so a reading that opened a print or saw the machine idle is not
// lost with the write.
func (d *devices) requeue(changed []changedRecord) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, taken := range changed {
		taken.printID = ""

		if newer, held := d.pending[taken.record.DeviceID]; held {
			newer.moved |= taken.moved
			newer.jobMoved = newer.jobMoved || taken.jobMoved
			newer.quiet = newer.quiet || taken.quiet
			taken = newer
		}

		d.pending[taken.record.DeviceID] = taken
	}
}

// writeSoon takes this device's record at once and asks the flush goroutine to
// run before its next tick.
//
// It is for the changes an operator acts on, such as an arrival, a
// reachability change and a departure. They happen at the rate a machine
// changes state and not at the frame rate, so taking them at once costs
// little, and holding them for two seconds would leave a fleet screen behind a
// person watching a cable come out.
//
// The record is taken here and not left as a mark on the entry, because the
// entry may not be reachable by the time the flush runs. See takeChanged.
func (d *devices) writeSoon(entry *device) {
	taken := entry.forceRecord()

	d.mu.Lock()

	if earlier, held := d.pending[taken.record.DeviceID]; held {
		taken.moved |= earlier.moved
		taken.jobMoved = taken.jobMoved || earlier.jobMoved
		taken.quiet = taken.quiet || earlier.quiet
	}

	d.pending[taken.record.DeviceID] = taken
	d.mu.Unlock()

	select {
	case d.flushNow <- struct{}{}:
	default:
		// A flush is already asked for, and one flush writes every device that
		// has anything waiting. A second signal would buy nothing and a
		// blocking send would put a printer's goroutine behind the database.
	}
}
