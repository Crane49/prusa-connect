package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	pfiles "github.com/prusa3d/fractal-link/files"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	sdkfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// The outcomes FetchCounts counts a print of a fractal URL under.
const (
	OutcomeStarted  = "started"
	OutcomeNotFound = "not_found"
	OutcomeFailed   = "failed"
	OutcomeDropped  = "dropped"
)

// fetchJob is one print of a fractal URL after the call was answered.
type fetchJob struct {
	device    string
	url       sdkfiles.URL
	caller    fractal.NodePath
	principal string
}

// fractalURL returns the fractal URL a call names, when the method takes one.
// It reports false for any other value, which arguments then handles.
func fractalURL(mapped *MethodMap, params []json.RawMessage) (sdkfiles.URL, bool, error) {
	for _, arg := range mapped.Args {
		if !arg.Fractal {
			continue
		}

		text, ok := argumentOf(arg, params)
		if !ok || !strings.EqualFold(schemeOf(text), sdkfiles.Scheme) {
			continue
		}

		u, err := sdkfiles.ParseURL(text)
		if err != nil {
			return sdkfiles.URL{}, true, answerWith(CodeURLMalformed)
		}

		return u, true, nil
	}

	return sdkfiles.URL{}, false, nil
}

// printFractal answers before the fetch ends. Invoke has already checked the
// state of the printer.
func (o *owner) printFractal(ctx context.Context, u sdkfiles.URL, caller fractal.NodePath) error {
	if !o.bridge.files.Reachable(ctx, u.ID) {
		return answerWith(CodeObjectUnreachable)
	}

	if !o.bridge.mayPrint(ctx, o.device) {
		return answerWith(CodeCallNotAllowed)
	}

	job := fetchJob{device: o.device, url: u, caller: caller, principal: o.bridge.principal()}

	o.bridge.startFetch(job)

	return nil
}

// The questions device.print answers for a print from a storage: operate on
// the printer, and read_storage on the storages.
const (
	actionOperate     = "operate"
	actionReadStorage = "read_storage"
	objectDevice      = "device"
	objectKind        = "resource_kind"
	kindStorage       = "storage"
)

// mayPrint asks the host what device.print asks, to refuse before the fetch.
// On any error from the host it allows, and device.print decides afterwards.
func (b *Bridge) mayPrint(ctx context.Context, device string) bool {
	if b.access == nil {
		return true
	}

	principal := b.principal()

	for _, question := range []accessapi.CheckParams{
		{Subject: principal, Action: actionOperate, ObjectType: objectDevice, ObjectID: device},
		{Subject: principal, Action: actionReadStorage, ObjectType: objectKind, ObjectID: kindStorage},
	} {
		decision, err := b.access.Check(ctx, question)
		if err != nil {
			b.log.WarnContext(ctx, "the host could not say whether a print is allowed, so device.print decides",
				"device", device, "principal", principal, "action", question.Action, "error", err.Error())

			return true
		}

		if !decision.Allowed {
			return false
		}
	}

	return true
}

// startFetch runs the fetch and the print in the background, bounded by
// files.start_deadline and Run. A print after Run returned is dropped at once.
func (b *Bridge) startFetch(job fetchJob) {
	// The check of life and the Add are one step under mu, so Run's Wait never
	// meets an Add.
	b.mu.Lock()
	if b.life.Err() != nil {
		b.mu.Unlock()
		b.fetchEnded(b.life, job, b.life.Err())

		return
	}

	b.jobs.Add(1)
	b.mu.Unlock()

	running, cancel := context.WithTimeout(b.life, b.startDeadline)

	go func() {
		defer b.jobs.Done()
		defer cancel()

		b.fetchAndPrint(running, job)
	}()
}

func (b *Bridge) fetchAndPrint(ctx context.Context, job fetchJob) {
	storage, source, release, err := b.files.Obtain(ctx, job.url)
	if err != nil {
		b.fetchEnded(ctx, job, err)

		return
	}

	b.mu.Lock()
	b.printing++
	b.mu.Unlock()

	started, err := b.print(ctx, job, storage, source)
	if err != nil {
		b.holdUntilSettled("", release, nil)
		b.fetchEnded(ctx, job, err)

		return
	}

	if settled, failed := b.holdUntilSettled(started.TransferID, release, &job); failed {
		b.transferFailed(ctx, job, settled)
	}
}

// print sends the file to the first free path on the printer. A path taken
// by another file is never printed, because nothing checked that file.
func (b *Bridge) print(ctx context.Context, job fetchJob, storage, source string) (deviceapi.Started, error) {
	params := deviceapi.PrintParams{Principal: job.principal, ID: job.device, Storage: storage, Source: source}
	paths := b.files.PrinterPaths(ctx, job.url, storage, source)

	var taken error

	for _, at := range paths {
		params.Path = at

		started, err := b.fleet.Print(ctx, params)
		if !errors.Is(err, deviceapi.ErrNameTaken) {
			return started, err
		}

		taken = err
	}

	return deviceapi.Started{}, fmt.Errorf("the printer holds a file at %s: %w", strings.Join(paths, " and at "), taken)
}

// fetchEnded counts and logs a print that did not start.
func (b *Bridge) fetchEnded(ctx context.Context, job fetchJob, err error) {
	outcome := OutcomeFailed

	switch {
	case ctx.Err() != nil:
		outcome = OutcomeDropped
	case errors.Is(err, pfiles.ErrNotAvailable):
		outcome = OutcomeNotFound
	}

	b.notStarted(ctx, job, outcome, err.Error())
	// Counted after the line, so a reader of the count also sees the line.
	b.count(outcome)
}

// transferFailed moves a print from started to failed. The core sends the
// start command only after the whole file arrived, so the print did not start.
func (b *Bridge) transferFailed(ctx context.Context, job fetchJob, settled event.TransferSettled) {
	b.notStarted(ctx, job, OutcomeFailed, "the transfer "+settled.State+": "+settled.Reason)

	b.mu.Lock()
	defer b.mu.Unlock()

	b.fetched[OutcomeStarted]--
	if b.fetched[OutcomeStarted] == 0 {
		delete(b.fetched, OutcomeStarted)
	}

	b.fetched[OutcomeFailed]++
}

func (b *Bridge) notStarted(ctx context.Context, job fetchJob, outcome, reason string) {
	b.log.WarnContext(ctx, "a print of a file from the network did not start",
		"identity", job.url.ID.Text(), "printer", job.device, "caller", string(job.caller),
		"outcome", outcome, "reason", reason)
}

// holdUntilSettled keeps the copy until the transfer settles, at most maxHold.
// It returns the settled event and true when the transfer already failed.
func (b *Bridge) holdUntilSettled(
	transfer string, release func(), job *fetchJob,
) (event.TransferSettled, bool) {
	b.mu.Lock()
	b.printing--

	settled, early := b.settledEarly[transfer]
	delete(b.settledEarly, transfer)

	if b.printing == 0 {
		clear(b.settledEarly)
	}

	if job != nil {
		b.fetched[OutcomeStarted]++
	}

	if transfer != "" && !early {
		timer := time.AfterFunc(b.maxHold, func() { b.endHold(transfer) })
		b.releases[transfer] = func() {
			timer.Stop()
			release()
		}

		if job != nil {
			b.started[transfer] = *job
		}
	}
	b.mu.Unlock()

	if transfer == "" || early {
		release()
	}

	return settled, job != nil && early && settled.State != event.TransferDone
}

// settle releases the copy of a settled transfer. While a print waits for
// Fleet.Print, it remembers a transfer that has no copy registered yet.
func (b *Bridge) settle(ctx context.Context, data json.RawMessage) {
	var settled event.TransferSettled
	if err := json.Unmarshal(data, &settled); err != nil || settled.TransferID == "" {
		return
	}

	// The job is taken under the same lock that finds it, so a transfer the
	// core reports as ended twice at once is counted failed once.
	b.mu.Lock()
	job, printed := b.started[settled.TransferID]
	delete(b.started, settled.TransferID)

	if _, held := b.releases[settled.TransferID]; !held && b.printing > 0 {
		b.settledEarly[settled.TransferID] = settled
	}
	b.mu.Unlock()

	b.endHold(settled.TransferID)

	if printed && settled.State != event.TransferDone {
		b.transferFailed(ctx, job, settled)
	}
}

func (b *Bridge) endHold(transfer string) {
	b.mu.Lock()
	release, ok := b.releases[transfer]
	delete(b.releases, transfer)
	delete(b.started, transfer)
	b.mu.Unlock()

	if ok {
		release()
	}
}

func (b *Bridge) count(outcome string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.fetched[outcome]++
}

// FetchCounts returns how many prints of a fractal URL ended in each outcome.
// The caller owns the map.
func (b *Bridge) FetchCounts() map[string]uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	return maps.Clone(b.fetched)
}
