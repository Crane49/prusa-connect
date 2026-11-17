package app

// Which print a reading belongs to, and the id that print is stored under.
//
// The machine's job number is not an identity. It is 16 bits on xBuddy and a
// firmware reset starts it again, so two prints can carry one number on one
// machine. A print is therefore named by a version 7 UUID minted once, and a
// reading is matched to a print by comparing it with the print the device
// began last. Older prints are never compared: a number that comes back names
// the newest print that carried it, or a new one.
//
// The one case the number alone cannot decide is the number coming back after
// the print it named has ended. A report that arrives right after the end is
// about that print; a print started after a reset is a new one. The difference
// this server can observe is an idle machine between the two: a machine that
// reboots and is given a new print passes through IDLE or READY, and a late
// report does not. A window of time would decide it worse, because it merges a
// reprint that starts soon after the end. The new print also has to be under
// way: a machine in ATTENTION under that number is showing a print preview,
// which is no print yet (see jobFacts.underWay).
//
// What this cannot see: a reset and a new print under the same number, both
// while this server cannot see the machine, continue the print that was open.

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

const (
	stateIdle  = "IDLE"
	stateReady = "READY"
)

// quietStates are the machine states that say no print is on the bed. FINISHED
// and STOPPED are not here: they are the print ending, and counting them would
// let a late report from that print open a new one.
//
//nolint:gochecknoglobals // a fixed set, read-only after init.
var quietStates = []string{stateIdle, stateReady}

// quietState reports whether a machine state says no print is on the bed.
func quietState(state string) bool { return slices.Contains(quietStates, state) }

// latestPrint is what the flush remembers about the print a device began last.
type latestPrint struct {
	// id is the print's UUID and number is the machine's job number for it.
	id     string
	number string
	// ended is whether the print has an end, and quiet whether the machine has
	// been seen idle or ready since it ended.
	ended bool
	quiet bool
	// seen is when the machine last reported this print. It is the end a print
	// the machine replaced without saying how it ended is given.
	seen time.Time
}

// printMatch is what one reading is, as matchPrint decides it.
type printMatch struct {
	// id is the print the reading belongs to.
	id string
	// opens is whether the reading begins a print of its own.
	opens bool
	// abandons is the id of an open print the machine replaced with this one
	// without saying how it ended, and is empty otherwise.
	abandons string
}

// matchPrint decides which print one reading belongs to. latest is the print
// the device began last and known whether there is one; quiet is whether the
// machine was seen idle or ready since the last take. mint names a new print.
//
// The caller has checked that the reading names a job number.
func matchPrint(latest latestPrint, known bool, facts jobFacts, quiet bool, mint func() string) printMatch {
	switch {
	case !known:
		return printMatch{id: mint(), opens: true}
	case facts.machineID != latest.number:
		out := printMatch{id: mint(), opens: true}
		if !latest.ended {
			out.abandons = latest.id
		}

		return out
	case latest.ended && (latest.quiet || quiet) && facts.underWay():
		return printMatch{id: mint(), opens: true}
	default:
		return printMatch{id: latest.id}
	}
}

// storedLatestPrint is a device's latest print as the store holds it, with the
// device's last record deciding whether it was seen idle after the print ended
// and when the machine was last heard from.
func storedLatestPrint(job devicestore.PrinterJob, record devicestore.DeviceRecord) latestPrint {
	ended := !job.Running()

	return latestPrint{
		id:     job.JobID,
		number: job.DeviceJobID,
		ended:  ended,
		quiet:  ended && quietState(record.MachineState),
		seen:   record.LastSeen,
	}
}

// printBook holds the latest print of every device. The flush writes it; the
// device listing reads it for the id of the print a machine is running, so it
// carries its own lock.
type printBook struct {
	mu     sync.Mutex
	latest map[string]latestPrint
}

func newPrintBook() *printBook { return &printBook{latest: map[string]latestPrint{}} }

// load replaces the book with what the store holds, at start.
func (b *printBook) load(latest map[string]latestPrint) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.latest = latest
}

func (b *printBook) get(device string) (latestPrint, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	held, known := b.latest[device]

	return held, known
}

func (b *printBook) put(device string, held latestPrint) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.latest[device] = held
}

// keptPrint is what the book held for one device before a flush matched its
// reading, and whether it held anything.
type keptPrint struct {
	latest latestPrint
	known  bool
}

// keep copies what the book holds for the devices a flush is about to match,
// so a flush whose write fails can put the book back as it was.
func (b *printBook) keep(changed []changedRecord) map[string]keptPrint {
	b.mu.Lock()
	defer b.mu.Unlock()

	kept := make(map[string]keptPrint, len(changed))
	for _, taken := range changed {
		held, known := b.latest[taken.record.DeviceID]
		kept[taken.record.DeviceID] = keptPrint{latest: held, known: known}
	}

	return kept
}

// restore puts back what keep copied. A print the failed flush minted is
// forgotten, and the next flush matches the same reading again.
func (b *printBook) restore(kept map[string]keptPrint) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for device, held := range kept {
		if held.known {
			b.latest[device] = held.latest
		} else {
			delete(b.latest, device)
		}
	}
}

// idOf is the id of the print a reading names, and empty when the reading is
// a print the flush has not matched yet. That is a print begun since the last
// flush, under a new number or under the number of a print that ended before
// the machine was seen idle, and it has no id until the flush mints one.
func (b *printBook) idOf(device string, facts jobFacts) string {
	held, known := b.get(device)
	if !known || held.number != facts.machineID {
		return ""
	}

	if held.ended && held.quiet && facts.underWay() {
		return ""
	}

	return held.id
}

// mintPrintID names a new print. A version 7 UUID leads with the time it was
// minted, so prints sort by when this server first recorded them.
func mintPrintID() string { return uuid.Must(uuid.NewV7()).String() }

// printIDAt names a print that is written after the fact, with the time it
// began in place of the time it was minted, so it sorts among the prints this
// server recorded as they ran. random fills the bits a version 7 UUID leaves
// to chance.
func printIDAt(began time.Time, random [10]byte) string {
	var (
		id    uuid.UUID
		stamp [8]byte
	)

	binary.BigEndian.PutUint64(stamp[:], uint64(max(began.UnixMilli(), 0)))

	// The first 48 bits are the milliseconds, the rest is the random bits with
	// the version and the variant written over theirs.
	copy(id[:6], stamp[2:])
	copy(id[6:], random[:])
	id[6] = id[6]&lowNibble | uuidVersion7
	id[8] = id[8]&variantBits | uuidVariant

	return id.String()
}

// The bits RFC 9562 fixes in a version 7 UUID: the version in the high nibble
// of byte 6, and the variant in the two high bits of byte 8.
const (
	lowNibble    = 0x0f
	uuidVersion7 = 0x70
	variantBits  = 0x3f
	uuidVariant  = 0x80
)

// abandonedPrint is an open print a machine replaced with another one without
// saying how it ended, and when the machine last reported it.
type abandonedPrint struct {
	id     string
	device string
	at     time.Time
}

// identifyPrints matches the reading each changed device carries to a print,
// before the records are written, so the record names the print by its id.
//
// It runs on the flush goroutine and nowhere else, so two flushes never decide
// one device's print at once. It returns the prints the machines replaced
// without saying how they ended.
func (d *devices) identifyPrints(changed []changedRecord) []abandonedPrint {
	var abandoned []abandonedPrint

	for index := range changed {
		taken := &changed[index]
		device := taken.record.DeviceID

		latest, known := d.prints.get(device)

		if !taken.job.held() {
			// A reading with no job number names no print. A take that saw the
			// machine idle or ready still counts for a print that has ended.
			if known && latest.ended && !latest.quiet && taken.quiet {
				latest.quiet = true
				d.prints.put(device, latest)
			}

			continue
		}

		match := matchPrint(latest, known, taken.job, taken.quiet, mintPrintID)
		if match.abandons != "" {
			abandoned = append(abandoned, abandonedPrint{
				id: match.abandons, device: device, at: lastReported(latest, taken.record.LastSeen),
			})
		}

		taken.printID = match.id
		taken.record.CurrentJobID = currentPrintID(latest, match, taken.job)

		// A print that opens is written even when the tree did not move
		// since the last take, which is a reused number read back unchanged.
		taken.jobMoved = taken.jobMoved || match.opens

		next := nextLatest(latest, match, taken.job, taken.quiet)
		next.seen = taken.record.LastSeen
		d.prints.put(device, next)
	}

	return abandoned
}

// lastReported is when the machine last reported a print it has now replaced.
// A print loaded from a store that never recorded it takes the moment the
// replacement was seen.
func lastReported(latest latestPrint, replacedAt time.Time) time.Time {
	if latest.seen.IsZero() {
		return replacedAt
	}

	return latest.seen
}

// currentPrintID is the print the device record names as the one the machine
// is running, and empty when it is running none. A late report about a print
// that has ended names that print but does not make it the running one.
func currentPrintID(latest latestPrint, match printMatch, facts jobFacts) string {
	if facts.running() && (match.opens || !latest.ended) {
		return match.id
	}

	return ""
}

// nextLatest is what the book holds for a device after one reading matched to
// a print.
func nextLatest(latest latestPrint, match printMatch, facts jobFacts, quiet bool) latestPrint {
	next := latestPrint{id: match.id, number: facts.machineID}

	if match.opens {
		next.ended = facts.ended()
		next.quiet = next.ended && quietState(facts.printerState)

		return next
	}

	// A print that had already ended stays ended whatever a late report says,
	// because its row is frozen at the reading that ended it.
	next.ended = latest.ended || facts.ended()
	next.quiet = next.ended &&
		(quietState(facts.printerState) || (latest.ended && (latest.quiet || quiet)))

	return next
}

// loadPrints fills the book from the store at start. stored is the fleet as
// restore read it, whose last record says whether a machine was seen idle
// after its print ended and when it was last heard from.
func (d *devices) loadPrints(ctx context.Context, stored []devicestore.DeviceRecord) error {
	jobs, err := d.records.LatestDeviceJobs(ctx)
	if err != nil {
		return fmt.Errorf("read the print every device began last: %w", err)
	}

	records := make(map[string]devicestore.DeviceRecord, len(stored))
	for _, record := range stored {
		records[record.DeviceID] = record
	}

	latest := make(map[string]latestPrint, len(jobs))
	for device, job := range jobs {
		latest[device] = storedLatestPrint(job, records[device])
	}

	d.prints.load(latest)

	return nil
}

// endAbandonedPrints stamps an end on every print a machine replaced without
// saying how it ended, and publishes how it ended, which is unknown.
//
// It runs after the records are written and before the rows of the prints
// that replaced them, in the same flush. A failure is logged
// and not retried, for the reason writeJobs gives: the print stays open and
// is no longer any device's current one.
func (d *devices) endAbandonedPrints(ctx context.Context, abandoned []abandonedPrint) {
	for _, gone := range abandoned {
		// The machine said nothing about how this print ended, so the row and
		// the settled frame below both read unknown.
		if err := d.records.EndPrinterJob(ctx, gone.id, gone.at, event.JobUnknown); err != nil {
			d.log.ErrorContext(ctx, "a print the machine replaced could not be ended",
				"device", gone.device, "job_id", gone.id, "error", err.Error())

			continue
		}

		row, held, err := d.records.PrinterJobByID(ctx, gone.id)
		if err != nil || !held {
			d.log.ErrorContext(ctx, "a print the machine replaced could not be read back",
				"device", gone.device, "job_id", gone.id, "held", held, "error", fmt.Sprint(err))

			continue
		}

		// The facts are empty: the machine said nothing about how this print
		// ended, so the outcome is unknown and the times are not known.
		d.publishJob(ctx, row.DeviceID, event.TypeDeviceJobSettled, row.JobID,
			frameOf(settledJobOf(row, jobFacts{})))
	}
}
