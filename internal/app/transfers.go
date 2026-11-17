package app

// The record of a file moving from one of this deployment's storages onto a
// device.
//
// A transfer takes minutes and moves bytes, so it has to be observable while
// it runs. Three things carry it:
//
//   - this register, which holds the running transfers and is the only place a
//     live byte count exists
//   - the bus, which carries two durable frames and a stream of telemetry so a
//     subscriber sees it move
//   - the history adapter's projection, which folds the durable pair into one
//     row so a transfer is still there after a restart
//
// The register is memory and the projection is derived from the facts. So the
// record of a transfer has one write path: the frames this file publishes.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/transferapi"
)

// ErrNoSuchTransfer reports a transfer id this process is not running. A
// transfer that has settled and a transfer that never existed both reach a
// caller as this. A caller that could tell them apart could learn which
// transfers a device it may not read has run.
var ErrNoSuchTransfer = errors.New("app: no transfer of that id is running")

// progressInterval is the shortest time between two progress frames of a
// running transfer. It is time and not bytes, because a person watching cares
// how stale the figure is. A rule in bytes would publish hundreds of times a
// second on a fast link and once a minute on a slow one.
const progressInterval = 250 * time.Millisecond

// transferRegister is the transfers this process is running.
//
// It holds the cancel function of each. The transfer runs on the goroutine of
// the request that began it, and another request can end it only by
// cancelling the context that request is blocked on.
type transferRegister struct {
	mu   sync.Mutex
	live map[string]*liveTransfer
	// detached counts the prints running with nobody waiting for them. See
	// maxDetachedPrints.
	detached int
}

// liveTransfer is one running transfer: the record as it stands and the way to
// end it.
type liveTransfer struct {
	// mu guards record alone. It is separate from the register's lock because
	// the progress callback takes it on every range, and a transfer's own
	// reporting must not queue behind a listing of every transfer.
	mu     sync.Mutex
	record transferapi.Record
	cancel context.CancelFunc
	// cancelled says somebody stopped this transfer, so the failure the driver
	// reports afterwards is recorded as a cancellation and not as a fault.
	cancelled bool
	// reason is what the person who stopped it said, kept for the settled frame.
	reason string
	// startedBy is the principal whose request began the transfer. It is kept
	// here and not on the record: who asked is audit, so it never goes into a
	// frame or a stored row, and the fleet listing hands it only to a caller
	// who holds view_audit on the device.
	startedBy string
}

// newTransferRegister returns an empty register, ready to use.
func newTransferRegister() *transferRegister {
	return &transferRegister{live: map[string]*liveTransfer{}}
}

// begin files a transfer that is about to start, and refuses a second one to a
// machine that is already fetching a file.
//
// A printer fetches one file at a time, so a second transfer would abandon the
// first. A driver may refuse it as well, but that refusal would arrive after
// this server has opened a record and published the started frame. This
// refusal comes first, and it holds for a driver that keeps no such rule.
func (r *transferRegister) begin(
	record transferapi.Record, startedBy string, cancel context.CancelFunc,
) (*liveTransfer, error) {
	entry := &liveTransfer{record: record, cancel: cancel, startedBy: startedBy}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, running := range r.live {
		if running.snapshot().Device != record.Device {
			continue
		}

		return nil, connectorapi.TransferBusy.
			New("the printer is already fetching a file").
			With(fault.StringParam("path", record.Destination)).
			Wrap(connectorapi.ErrTransferBusy)
	}

	r.live[record.TransferID] = entry

	return entry, nil
}

// end drops a transfer that has settled, so the register holds what is running
// and the store holds what has run.
func (r *transferRegister) end(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.live, id)
}

// find returns one running transfer of one device.
func (r *transferRegister) find(device, id string) (*liveTransfer, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, found := r.live[id]
	if !found || entry.snapshot().Device != device {
		return nil, false
	}

	return entry, true
}

// running is every transfer of one device that has not settled, newest first.
func (r *transferRegister) running(device string) []transferapi.Record {
	r.mu.Lock()

	out := make([]transferapi.Record, 0, len(r.live))

	for _, entry := range r.live {
		record := entry.snapshot()
		if record.Device == device {
			out = append(out, record)
		}
	}

	r.mu.Unlock()

	sort.Slice(out, func(i, j int) bool { return out[i].StartedMS > out[j].StartedMS })

	return out
}

// runningTransfer is one transfer that has not settled, with the principal who
// began it.
type runningTransfer struct {
	record    transferapi.Record
	startedBy string
}

// byDevice is every transfer that has not settled, grouped by the device it
// goes to, each group newest first.
func (r *transferRegister) byDevice() map[string][]runningTransfer {
	r.mu.Lock()

	out := make(map[string][]runningTransfer, len(r.live))

	for _, entry := range r.live {
		record := entry.snapshot()
		out[record.Device] = append(out[record.Device], runningTransfer{record: record, startedBy: entry.startedBy})
	}

	r.mu.Unlock()

	for _, group := range out {
		sort.Slice(group, func(i, j int) bool { return group[i].record.StartedMS > group[j].record.StartedMS })
	}

	return out
}

// snapshot is the record as it stands, copied so a reader holds no lock.
func (t *liveTransfer) snapshot() transferapi.Record {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.record
}

// advance records what the driver reported and returns the updated record.
// It reports false and changes nothing once the transfer is settled.
// A driver reports from goroutines of its own, which may still be finishing a
// range after Send has returned, and a report that late must not turn a
// settled record back into a running one.
func (t *liveTransfer) advance(progress connectorapi.Progress) (transferapi.Record, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.record.EndedMS != 0 {
		return t.record, false
	}

	t.record.BytesSent = progress.Sent
	if progress.Total > 0 {
		t.record.BytesTotal = progress.Total
	}

	// A driver reports a warning once, on its last report. A later report
	// with no warning does not clear it: the warning says the transfer
	// finished unconfirmed, and an empty report confirms nothing.
	if progress.Warning != "" {
		t.record.Warning = progress.Warning
	}

	t.record.State = transferapi.StateSending

	return t.record, true
}

// stop asks the transfer to end and records why, so the settled frame says a
// person stopped it and not that the driver failed.
func (t *liveTransfer) stop(reason string) {
	t.mu.Lock()
	t.cancelled = true
	t.reason = reason
	cancel := t.cancel
	t.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// settle writes the outcome onto the record and returns it, so the answer a
// caller gets from Send says how the transfer ended.
func (t *liveTransfer) settle(state, reason string, endedMS int64) transferapi.Record {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.record.State = state
	t.record.Reason = reason
	t.record.EndedMS = endedMS

	return t.record
}

// stopped reports whether somebody stopped this transfer, and what they said.
func (t *liveTransfer) stopped() (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.reason, t.cancelled
}

// Transfers is what this device has moved and is moving, newest first.
//
// It answers from two places. The running transfers come from this process's
// own register, because a live byte count exists nowhere else. The settled
// ones come from the history adapter's projection, because a transfer that
// ended before this process started is still a transfer this device ran. A
// build whose history adapter keeps no projection answers with the running
// transfers only and does not refuse.
func (d *devices) Transfers(
	ctx context.Context, principal string, id string,
) ([]httpapi.DeviceTransfer, error) {
	if _, err := d.reach(ctx, principal, id, authz.ActionObserve); err != nil {
		return nil, err
	}

	origin, err := deviceOrigin(id)
	if err != nil {
		return nil, err
	}

	live := d.transfers.running(origin.String())

	stored, err := d.storedTransfers(ctx, origin.String())
	if err != nil {
		return nil, err
	}

	out := make([]httpapi.DeviceTransfer, 0, len(live)+len(stored))
	for _, record := range live {
		out = append(out, transferOf(record))
	}

	// A transfer can be in both lists. A stored row whose transfer is still in
	// the register is skipped, so each transfer appears once.
	for _, record := range stored {
		if _, running := d.transfers.find(origin.String(), record.TransferID); running {
			continue
		}

		out = append(out, transferOf(record))
	}

	return out, nil
}

// RunningTransfers is every transfer still running on a printer the caller may
// observe, newest first, whoever began it.
//
// It reads only the register, because only running transfers are asked for.
// The printers come from the same authorized walk the fleet listing takes, so
// the permission is resolved inside the graph's query and a printer the caller
// cannot observe never reaches this code. The answer carries no count, so it
// says nothing about the transfers it left out.
func (d *devices) RunningTransfers(ctx context.Context, principal string) ([]httpapi.RunningTransfer, error) {
	who, err := authz.ParseSubject(principal)
	if err != nil {
		return nil, fmt.Errorf("read the caller's principal: %w", err)
	}

	running := d.transfers.byDevice()
	if len(running) == 0 {
		return []httpapi.RunningTransfer{}, nil
	}

	out := []httpapi.RunningTransfer{}

	err = d.walkVisible(ctx, who, func(row authz.DeviceRow, entry *device) {
		origin, originErr := deviceOrigin(row.ID)
		if originErr != nil {
			return
		}

		group := running[origin.String()]
		if len(group) == 0 {
			return
		}

		name := entry.displayedName(row.Name)
		canStop := slices.Contains(row.Actions, authz.ActionOperate)
		audit := slices.Contains(row.Actions, authz.ActionViewAudit)

		for _, one := range group {
			item := httpapi.RunningTransfer{
				DeviceTransfer: transferOf(one.record),
				DeviceID:       row.ID,
				PrinterName:    name,
				Mine:           one.startedBy != "" && one.startedBy == principal,
				CanStop:        canStop,
			}

			if audit {
				item.StartedBy = one.startedBy
			}

			out = append(out, item)
		}
	})
	if err != nil {
		return nil, err
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })

	return out, nil
}

// storedTransfers reads the projection, and reports none for a build that keeps
// none.
func (d *devices) storedTransfers(ctx context.Context, device string) ([]transferapi.Record, error) {
	if d.past == nil {
		return nil, nil
	}

	records, kept := d.past()
	if !kept {
		return nil, nil
	}

	stored, err := records.Transfers(ctx, transferapi.Query{Device: device})
	if err != nil {
		return nil, fmt.Errorf("read the stored transfers: %w", err)
	}

	return stored, nil
}

// StopTransfer ends one transfer of one device.
//
// It takes operate on the printer and not observe: stopping a transfer changes
// what the machine is doing, and a caller who may only watch may not end
// somebody else's upload. It returns as soon as the transfer has been told to
// stop. The goroutine that was running the transfer publishes the settled
// frame.
//
// A transfer this process is not running is looked up in the store. See
// stopStoredTransfer.
func (d *devices) StopTransfer(ctx context.Context, principal string, id string, transfer string) error {
	if _, err := d.reach(ctx, principal, id, authz.ActionOperate); err != nil {
		return err
	}

	origin, err := deviceOrigin(id)
	if err != nil {
		return err
	}

	entry, found := d.transfers.find(origin.String(), transfer)
	if !found {
		return d.stopStoredTransfer(ctx, principal, id, origin, transfer)
	}

	entry.stop("a caller stopped the transfer")

	return nil
}

// stopStoredTransfer ends a transfer this process is not running and the store
// still holds open.
//
// The register is this process's memory, so a transfer another run began is
// not in it. After a server is killed during a transfer, the stored row stays
// open and the printer may go on holding its transfer slot. So the store is
// the second place to look, and stopping such a row also tells the printer to
// let go.
//
// The row is closed as cancelled whether or not the printer answered. Nothing
// here will ever finish that transfer, and the row must not stay open for
// ever.
func (d *devices) stopStoredTransfer(
	ctx context.Context, principal, id string, origin event.Origin, transfer string,
) error {
	record, found, err := d.openStoredTransfer(ctx, origin.String(), transfer)
	if err != nil {
		return err
	}

	if !found {
		// A transfer of another device, one no row names, and one a settled
		// frame has closed all reach a caller as this. The sweep at start
		// closes what the last run left open, so the row asked for is most
		// often one the sweep has ended.
		return fault.PrinterTransferNotFound.
			New("no transfer of that id is running").
			With(fault.StringParam("transfer_id", transfer)).
			Wrap(ErrNoSuchTransfer)
	}

	// The printer is told only while this process holds no transfer of its own
	// on that machine. STOP_TRANSFER names no transfer: a board abandons
	// whatever it is fetching. Sent for a stale row while a live transfer of
	// the same printer is in flight, it would abort the wrong one. The stale
	// row is closed either way, and the live transfer has its own stop route.
	if len(d.transfers.running(origin.String())) == 0 {
		d.tellPrinterToStop(ctx, principal, id, transfer)
	}

	record.EndedMS = time.Now().UnixMilli()

	// On a context the caller cannot end, as in settleTransferRecord: the row
	// has to close even for a caller that gave up while the printer was being
	// told.
	d.publishTransferSettled(context.WithoutCancel(ctx), record,
		event.TransferCancelled, "a caller stopped a transfer this process was not running", "")

	d.log.InfoContext(ctx, "a transfer only the store held open was stopped by a caller",
		"device", origin.String(), "transfer_id", transfer,
		"bytes_sent", record.BytesSent, "bytes_total", record.BytesTotal)

	return nil
}

// openStoredTransfer reads the one open row of one device by id, and reports no
// row for a build that keeps no projection.
//
// The query asks for the device's open rows and not for a page of the device's
// history. A page comes back newest first under a page size, so a full listing
// would drop an old open row of a busy printer.
func (d *devices) openStoredTransfer(
	ctx context.Context, device, transfer string,
) (transferapi.Record, bool, error) {
	if d.past == nil {
		return transferapi.Record{}, false, nil
	}

	records, kept := d.past()
	if !kept {
		return transferapi.Record{}, false, nil
	}

	open, err := records.Transfers(ctx, transferapi.Query{Device: device, RunningOnly: true})
	if err != nil {
		return transferapi.Record{}, false, fmt.Errorf("read the stored transfers: %w", err)
	}

	for _, record := range open {
		if record.TransferID == transfer {
			return record, true, nil
		}
	}

	return transferapi.Record{}, false, nil
}

// tellPrinterToStop asks the machine to let go of the transfer slot it may
// still be holding.
//
// It goes down the ordinary command path, so the attempt is authorized, audited
// and stored as the same verb sent by hand would be, and it waits as long. A
// failure is only logged: a printer that is away is the ordinary case here,
// and the record is closed either way.
func (d *devices) tellPrinterToStop(ctx context.Context, principal, id, transfer string) {
	_, err := d.Command(ctx, principal, id, httpapi.PrinterCommand{Verb: verbStopTransfer})
	if err == nil {
		return
	}

	d.log.InfoContext(ctx, "a printer was not told to release a transfer slot it may still hold",
		"device", id, "transfer_id", transfer, "error", err.Error())
}

// verbStopTransfer is the verb that tells a machine to abandon what it is
// fetching. It is written here as the string the verb table carries and not
// as a dialect's constant, because the device plane speaks to every driver and
// imports none of them.
const verbStopTransfer = "core.command.stop_transfer"

// sweepInterruptedTransfers records every transfer the last run left open.
//
// It runs once, when a store that keeps facts first serves after the start. A
// transfer that was in flight when the process stopped is over: the device has
// forgotten it and holds only part of the file. So the row is closed as
// interrupted and nothing is resumed. It publishes a settled frame and does
// not write the row, so the record keeps one write path: the fold that wrote
// the row also closes it.
func (d *devices) sweepInterruptedTransfers(ctx context.Context) {
	if d.past == nil {
		return
	}

	records, kept := d.past()
	if !kept {
		return
	}

	open, err := records.Transfers(ctx, transferapi.Query{RunningOnly: true})
	if err != nil {
		d.log.WarnContext(ctx, "the transfers the last run left open could not be read",
			"error", err.Error())

		return
	}

	for _, record := range open {
		d.publishTransferSettled(ctx, record, event.TransferInterrupted, "server restarted", "")
	}

	if len(open) > 0 {
		d.log.InfoContext(ctx, "transfers left open by the last run are recorded interrupted",
			"transfers", len(open))
	}
}

// beginTransferRecord opens the record of a transfer that is about to start and
// publishes the frame that says so.
func (d *devices) beginTransferRecord(
	ctx context.Context, principal, device string, upload httpapi.DeviceUpload, size int64,
	cancel context.CancelFunc,
) (*liveTransfer, error) {
	id, err := d.commands.Mint(time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("name the transfer: %w", err)
	}

	origin, err := deviceOrigin(device)
	if err != nil {
		return nil, err
	}

	record := transferapi.Record{
		TransferID:  id.String(),
		Device:      origin.String(),
		Storage:     upload.Storage,
		Source:      upload.Source,
		Destination: upload.Path,
		BytesTotal:  size,
		State:       transferapi.StateQueued,
		StartedMS:   time.Now().UnixMilli(),
	}

	entry, err := d.transfers.begin(record, principal, cancel)
	if err != nil {
		return nil, err
	}

	d.publishTransfer(ctx, record.Device, event.TypeDeviceTransferStarted, event.ClassState,
		record.TransferID, frameOf(event.TransferStarted{
			TransferID:  record.TransferID,
			Storage:     record.Storage,
			Source:      record.Source,
			Destination: record.Destination,
			BytesTotal:  record.BytesTotal,
		}))

	d.logTransferStart(ctx, record)

	return entry, nil
}

// deviceOrigin renders one device id as the origin its frames are published
// on, which is also the reference the stored projection keys a row by.
func deviceOrigin(id string) (event.Origin, error) {
	origin, err := event.NewOrigin(event.KindDevice, id)
	if err != nil {
		return event.Origin{}, fmt.Errorf("name the device's origin: %w", err)
	}

	return origin, nil
}

// reportProgress is the callback the driver calls as the transfer moves.
//
// The record is updated on every report, because a listing reads it. A frame
// is published at most once per progressInterval, because a subscriber does
// not need one per range. The driver calls it from one goroutine at a time, so
// the last published time needs no lock.
func (d *devices) reportProgress(ctx context.Context, entry *liveTransfer) func(connectorapi.Progress) {
	var published time.Time

	return func(progress connectorapi.Progress) {
		record, running := entry.advance(progress)
		if !running {
			return
		}

		now := time.Now()
		if now.Sub(published) < progressInterval {
			return
		}

		published = now

		d.publishTransfer(ctx, record.Device, event.TypeDeviceTransferProgressed, event.ClassTelemetry,
			record.TransferID, frameOf(event.TransferProgressed{
				TransferID:         record.TransferID,
				BytesTotal:         record.BytesTotal,
				BytesSent:          progress.Sent,
				BytesReceived:      progress.Received,
				BytesReceivedKnown: progress.Known,
			}))
	}
}

// reasonOf is the sentence a person reads for a failed transfer.
//
// A driver's refusal carries the machine's own words as a param, inside an
// error that says only that the machine refused. `File already exists` tells a
// person what to do, and `the device refused the transfer` does not.
func reasonOf(failure error) string {
	if carried, ok := errors.AsType[*fault.Fault](failure); ok {
		for _, param := range carried.Params() {
			if param.Key == "reason" && param.Value.String() != "" {
				return param.Value.String()
			}
		}
	}

	return failure.Error()
}

// settleTransferRecord closes the record and publishes the frame that says how
// it ended.
//
// The failure the driver reported is read together with what the register
// knows. A transfer somebody stopped fails with a cancelled context, and that
// is recorded as a cancellation and not as a fault.
func (d *devices) settleTransferRecord(
	ctx context.Context, entry *liveTransfer, failure error, elapsed time.Duration,
) {
	state, reason := event.TransferDone, ""
	code := ""

	if failure != nil {
		state, reason, code = event.TransferFailed, reasonOf(failure), codeOf(failure)

		if stopped, was := entry.stopped(); was {
			// A person pressed stop, so there is no fault and the code is
			// empty. The failure here is the cancelled context the stop
			// caused. It carries no registered code, and codeOf would answer
			// "unregistered" for it.
			state, reason, code = event.TransferCancelled, stopped, ""
		} else if errors.Is(failure, context.Canceled) {
			// Nobody stopped it and the context ended anyway. Send relays on
			// a context the caller cannot end, so this is a driver reporting
			// a cancellation of its own. It is not a failure of the printer,
			// the storage or this server. Recording a failure would say the
			// file never arrived, and the machine may hold it.
			state, reason, code = event.TransferCancelled, "the printer's driver ended the transfer", ""
		}
	}

	record := entry.settle(state, reason, time.Now().UnixMilli())

	d.transfers.end(record.TransferID)

	// The publication runs on a context the caller cannot end, so the frame
	// that closes the row is written even for a caller that has gone.
	detached := context.WithoutCancel(ctx)

	d.publishTransferSettled(detached, record, state, reason, code)

	d.log.InfoContext(detached, "a transfer ended",
		"device", record.Device, "transfer_id", record.TransferID, "state", state,
		"bytes_sent", record.BytesSent, "bytes_total", record.BytesTotal,
		"elapsed_ms", elapsed.Milliseconds())
}

// publishTransferSettled writes the frame that closes one transfer's row.
func (d *devices) publishTransferSettled(
	ctx context.Context, record transferapi.Record, state, reason, code string,
) {
	elapsed := int64(0)
	if record.EndedMS > 0 && record.StartedMS > 0 {
		elapsed = record.EndedMS - record.StartedMS
	}

	d.publishTransfer(ctx, record.Device, event.TypeDeviceTransferSettled, event.ClassState,
		record.TransferID, frameOf(event.TransferSettled{
			TransferID: record.TransferID,
			State:      state,
			BytesSent:  record.BytesSent,
			BytesTotal: record.BytesTotal,
			Reason:     reason,
			FaultCode:  code,
			ElapsedMS:  elapsed,
		}))
}

// publishTransfer puts one transfer frame on the device's origin, with the
// transfer id as the causation that joins the three.
//
// A frame that could not be rendered is not published, as with a command
// frame. A reader can see that a record is missing, but cannot see that a
// record carries half a payload.
func (d *devices) publishTransfer(
	ctx context.Context, device, kind string, class event.Class, transfer string, data json.RawMessage,
) {
	if len(data) == 0 {
		d.log.ErrorContext(ctx, "a transfer frame could not be rendered and is unpublished",
			"device", device, "type", kind, "transfer_id", transfer)

		return
	}

	origin, err := event.ParseOrigin(device)
	if err != nil {
		d.log.ErrorContext(ctx, "a transfer names an origin this host cannot render",
			"device", device, "error", err.Error())

		return
	}

	env, _, err := d.hub.PublishStored(stream.Publication{
		Origin:      origin,
		Type:        kind,
		Class:       class,
		Data:        data,
		CausationID: transfer,
	})
	if err != nil {
		d.log.DebugContext(ctx, "a transfer frame reached no hub",
			"device", device, "type", kind, "error", err.Error())

		return
	}

	// The frame is also written to the store on this goroutine, as a command
	// frame is. Without that, a transfer that ends just before a shutdown
	// leaves a row that reads as still sending for ever.
	d.durable.write(ctx, factNow(origin, env))

	d.meters.published(kind)
}

// frameOf renders a payload, and nothing at all when it cannot be rendered.
func frameOf(payload any) json.RawMessage {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	return data
}

// transferOf renders one record as the transport publishes it. The record keeps
// Unix milliseconds and the wire takes RFC 3339, so the two instants are
// converted here. A transfer that is still running has no end, and the member
// is absent.
func transferOf(record transferapi.Record) httpapi.DeviceTransfer {
	out := httpapi.DeviceTransfer{
		TransferID: record.TransferID,
		Storage:    record.Storage,
		Source:     record.Source,
		Path:       record.Destination,
		BytesTotal: record.BytesTotal,
		BytesSent:  record.BytesSent,
		State:      record.State,
		Reason:     record.Reason,
		Warning:    record.Warning,
		StartedAt:  time.UnixMilli(record.StartedMS).UTC(),
	}

	if record.EndedMS > 0 {
		ended := time.UnixMilli(record.EndedMS).UTC()
		out.EndedAt = &ended
	}

	return out
}

// transferRecordsOf is how the device plane reaches the projection: it resolves
// the provider answering fact queries and asks whether it keeps transfer rows.
//
// It is a function and not a provider captured at boot, as with the history
// read surface: several instances may store the same writes, and a read picks
// one by capability on each call.
func transferRecordsOf(feed historyReaders) func() (transferapi.Records, bool) {
	return func() (transferapi.Records, bool) {
		provider, err := feed.reader(verbFacts)
		if err != nil {
			return nil, false
		}

		// The provider is a supervised wrapper around the adapter. The wrapper
		// implements the history contract and not this one, so the probe asks
		// the wrapper for the store underneath.
		if wrapped, ok := provider.(supervisedRecords); ok {
			return wrapped.TransferRecords()
		}

		return transferapi.Of(provider)
	}
}

// supervisedRecords is the wrapper's own probe: it asks the driver it holds
// whether the driver keeps transfer rows, and hands back a reader that runs
// every query under supervision.
type supervisedRecords interface {
	TransferRecords() (transferapi.Records, bool)
}

// logTransferStart is the one line that says a transfer began, so a log with no
// event stream still shows what moved.
func (d *devices) logTransferStart(ctx context.Context, record transferapi.Record) {
	d.log.InfoContext(ctx, "a transfer began",
		slog.String("device", record.Device),
		slog.String("transfer_id", record.TransferID),
		slog.String("storage", record.Storage),
		slog.String("source", record.Source),
		slog.String("path", record.Destination),
		slog.Int64("bytes_total", record.BytesTotal))
}
