package app

// The two frames a print puts on the bus: one when this server first sees the
// print, one when it sees it stop.
//
// They are published from the job write and not from the state tree, because
// the row is the thing that happens once. A tree restates a running print on
// every flush; the row opens once and takes an end once, and the store is what
// holds both across a restart.
//
// Exactly once per print, and that is what the store read below is for. A
// process that restarts mid-print matches the returning machine to the print it
// was running, and that row is already there: the print started before this
// process did, so nothing is published for its start. The same read tells a
// print that had already ended from one this flush is ending.

import (
	"context"
	"encoding/json"
	"time"

	"sync"

	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// printStartVerb is the instruction that starts a print. It is a key this host
// mints and every driver declares, and not the name of a dialect: a driver that
// prints implements it whatever wire it speaks.
const printStartVerb = "core.command.start_print"

// printStarts is the instruction that started the print each device is about to
// begin, waiting for the row that print opens.
//
// It exists because the two are known in different places. A command is issued
// on the caller's goroutine and a print opens on the flush, and nothing the
// machine reports about the print names the instruction, so the join is made
// here or not at all.
//
// One id per device, and the newest wins. A second start sent to a machine that
// has not begun the first one replaces it, which is what the machine will do
// with the file as well.
type printStarts struct {
	mu       sync.Mutex
	byDevice map[string]string
}

func newPrintStarts() *printStarts { return &printStarts{byDevice: map[string]string{}} }

// note records the instruction sent to one device.
func (p *printStarts) note(device, commandID string) {
	if device == "" || commandID == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.byDevice[device] = commandID
}

// take reads the instruction waiting for this device's next print and forgets
// it, so one command names one print.
func (p *printStarts) take(device string) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	held := p.byDevice[device]
	delete(p.byDevice, device)

	return held
}

// forget drops the instruction waiting for a device, for a start the machine
// never carried out. A command that failed must not name the next print
// somebody makes at the machine's own screen.
func (p *printStarts) forget(device string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.byDevice, device)
}

// notePrintStart holds a start the machine accepted, so the print it begins can
// name it, and drops one the machine refused.
//
// It is read off the answer and not off the request, because a start the
// machine said no to begins no print: leaving it here would put it on whatever
// print somebody next made at the machine's own screen.
func (d *devices) notePrintStart(
	device, commandID, verb string, result connectorapi.Result, failure error,
) {
	if verb != printStartVerb {
		return
	}

	if failure != nil || !result.OK {
		d.printStarts.forget(device)

		return
	}

	d.printStarts.note(device, commandID)
}

// jobAnnouncement is what this process has already published about one device's
// print. It is owned by the flush goroutine, which is the only thing that reads
// or writes it.
type jobAnnouncement struct {
	// jobID is the print the other two fields are about. A device printing
	// something else has moved on, and the new print owes both frames again.
	jobID string
	// started is the instant the started frame carried, which is the instant
	// the row holds. The settled frame repeats it, so a print whose start this
	// process never saw still says when it began.
	started time.Time
	// settled says the end has been published, so a machine that reports a
	// second terminal reading publishes nothing more.
	settled bool
	// startPending says the started frame is still owed, because the flush
	// that first saw this print found a reading only half applied. The next
	// flush publishes it from whatever the tree says by then.
	startPending bool
}

// jobFrames is what one row still owes the bus.
type jobFrames struct {
	row     devicestore.PrinterJob
	facts   jobFacts
	started bool
	settled bool
	// holdStart says the started frame is owed and is not published yet. See
	// halfApplied below.
	holdStart bool
	// settledBefore says an earlier flush published this print's end. The row
	// in the store keeps that end for ever, and a late report about the print,
	// such as a print preview under its number, carries none.
	settledBefore bool
}

// halfApplied reports whether this row comes from a reading the machine has
// only partly delivered.
//
// One reading reaches this server as a path at a time, and the flush timer can
// fire between two of them. A row taken then carries the job id and not yet the
// file the print is for, so a started frame built from it names an empty file
// and a consumer has nothing to show. The machine's own word for what it is
// doing is the last part of a reading to arrive, so a row that has none is a
// reading still on its way.
//
// A print that has already ended is never held: the two frames of one print are
// published in order and never split by a wait.
func halfApplied(row devicestore.PrinterJob) bool {
	return row.State == "" && row.Ended.IsZero()
}

// jobEventsOwed works out which frames each row in this flush still owes,
// before the rows are written.
//
// It is asked before the write because the write is an upsert: afterwards a row
// this flush created and a row it restated read the same. A device whose print
// this process has already announced is answered out of the announcement and
// asks the store nothing, so a fleet reporting progress five times a second
// costs no read at all. The store is asked once per print, for the print this
// process has not seen before, which is a machine starting one and a machine
// that was already printing when this process came back.
func (d *devices) jobEventsOwed(
	ctx context.Context, rows []devicestore.PrinterJob, facts map[string]jobFacts,
) []jobFrames {
	owed := make([]jobFrames, 0, len(rows))

	for _, row := range rows {
		frames := jobFrames{row: row, facts: facts[row.DeviceID]}

		held, known := d.announcedJobs[row.DeviceID]
		if !known || held.jobID != row.JobID {
			held = d.readAnnouncement(ctx, row)
		}

		fresh := held.jobID != row.JobID

		frames.started = fresh || held.startPending
		frames.settled = !held.settled && !row.Ended.IsZero()
		frames.settledBefore = !fresh && held.settled

		// A print seen for the first time out of a reading that is still
		// arriving waits one flush, so its frame names the file. A print
		// already waiting is published from whatever this flush holds, because
		// a machine that says no more must not leave the frame owed for ever.
		frames.holdStart = fresh && halfApplied(row)

		// The start the row holds, which is the first write's and not this
		// reading's. A print this process found already open keeps the moment
		// the machine was heard from before the restart.
		if !fresh {
			frames.row.Started = held.started
		}

		owed = append(owed, frames)
	}

	return owed
}

// readAnnouncement asks the store what it already holds about this print, for a
// print this process has not announced.
//
// A row that is there was opened by a write that published the started frame,
// so its start is not published again. A row that is there with an end was
// closed the same way. A store this server cannot read answers nothing, and the
// caller then publishes as though the print were new: a frame too many is a
// reading a consumer folds onto the row it already has, and a frame too few is
// a print that never appears.
func (d *devices) readAnnouncement(ctx context.Context, row devicestore.PrinterJob) jobAnnouncement {
	stored, err := d.records.DeviceJobs(ctx, row.DeviceID, currentAndLastJobs)
	if err != nil {
		d.log.ErrorContext(ctx, "the prints of a device could not be read before announcing one",
			"device", row.DeviceID, "job_id", row.JobID, "error", err.Error())

		return jobAnnouncement{}
	}

	for _, job := range stored {
		if job.JobID != row.JobID {
			continue
		}

		return jobAnnouncement{jobID: job.JobID, started: job.Started, settled: !job.Ended.IsZero()}
	}

	return jobAnnouncement{}
}

// publishJobEvents puts the frames the written rows owe on each device's
// origin, and records what it published.
//
// It runs after the write, because both frames say the row is in the store. A
// frame published first would tell a history provider about a print the
// transaction then refused.
func (d *devices) publishJobEvents(ctx context.Context, owed []jobFrames) {
	for _, frames := range owed {
		if frames.started && !frames.holdStart {
			d.publishJob(ctx, frames.row.DeviceID, event.TypeDeviceJobStarted, frames.row.JobID,
				frameOf(event.JobStarted{
					JobID:       frames.row.JobID,
					DeviceJobID: frames.row.DeviceJobID,
					Path:        frames.row.Path,
					DisplayName: frames.row.DisplayName,
					CommandID:   d.printStarts.take(frames.row.DeviceID),
					Started:     frames.row.Started,
				}))
		}

		if frames.settled {
			d.publishJob(ctx, frames.row.DeviceID, event.TypeDeviceJobSettled, frames.row.JobID,
				frameOf(settledJobOf(frames.row, frames.facts)))
		}

		d.announcedJobs[frames.row.DeviceID] = jobAnnouncement{
			jobID:        frames.row.JobID,
			started:      frames.row.Started,
			settled:      frames.settled || frames.settledBefore || !frames.row.Ended.IsZero(),
			startPending: frames.holdStart,
		}
	}

	d.boundAnnouncedJobs()
}

// settledJobOf is the payload that closes one print.
//
// The outcome is the driver's word and never this server's reading of a machine
// state: the set of terminal words belongs to the dialect, and a print that
// ended with none is unknown. machine_state carries the machine's own last word
// beside it, so a consumer that knows the dialect loses nothing to the closed
// set.
func settledJobOf(row devicestore.PrinterJob, facts jobFacts) event.JobSettled {
	return event.JobSettled{
		JobID:             row.JobID,
		DeviceJobID:       row.DeviceJobID,
		Path:              row.Path,
		DisplayName:       row.DisplayName,
		Started:           row.Started,
		Ended:             row.Ended,
		Outcome:           jobOutcomeOf(facts.outcome),
		MachineState:      row.State,
		Progress:          row.Progress,
		TimePrintingS:     facts.printing,
		TimePrintingKnown: facts.printingKnown,
		FilamentUsedMM:    facts.filament,
		FilamentUsedKnown: facts.filamentKnown,
	}
}

// jobOutcomeOf holds the driver's word to the closed set. A driver that
// reported nothing, and one that reported a word this build does not carry,
// both read as unknown: a value outside the set would reach a consumer that
// branches on four.
func jobOutcomeOf(reported string) string {
	switch reported {
	case event.JobFinished, event.JobFailed, event.JobCancelled:
		return reported
	default:
		return event.JobUnknown
	}
}

// publishJob puts one print frame on the device's origin, with the job id as
// the causation that joins the two.
//
// It writes the frame into the durable store on this goroutine for the reason a
// transfer frame does: a print that ends in the last window before a shutdown
// must not lose the record of how it ended.
//
// A frame that could not be rendered is not published. A record that is missing
// is one a reader can see is missing, and one carrying half a payload is not.
func (d *devices) publishJob(ctx context.Context, device, kind, jobID string, data json.RawMessage) {
	if len(data) == 0 {
		d.log.ErrorContext(ctx, "a print frame could not be rendered and is unpublished",
			"device", device, "type", kind, "job_id", jobID)

		return
	}

	origin, err := event.NewOrigin(event.KindDevice, device)
	if err != nil {
		d.log.ErrorContext(ctx, "a print names an origin this host cannot render",
			"device", device, "error", err.Error())

		return
	}

	env, _, err := d.hub.PublishStored(stream.Publication{
		Origin:      origin,
		Type:        kind,
		Class:       event.ClassState,
		Data:        data,
		CausationID: jobID,
	})
	if err != nil {
		d.log.DebugContext(ctx, "a print frame reached no hub",
			"device", device, "type", kind, "error", err.Error())

		return
	}

	d.durable.write(ctx, factNow(origin, env))

	d.meters.published(kind)
}

// announcedJobsLimit bounds what the flush remembers about prints. It sits well
// above a site's fleet, because the map holds one small entry per device that
// has printed and nothing else.
const announcedJobsLimit = 4096

// boundAnnouncedJobs keeps the map from growing with every device id this
// process has ever seen. It is emptied rather than trimmed, because nothing
// here knows which device will print next, and emptying it costs a read of the
// job store for the next print of each device and nothing else: the store is
// what the answer comes from either way.
func (d *devices) boundAnnouncedJobs() {
	if len(d.announcedJobs) <= announcedJobsLimit {
		return
	}

	clear(d.announcedJobs)
}
