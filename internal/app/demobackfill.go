package app

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/prusa3d/prusa-connect/adapters/db/sqlite"
	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/sqlstore"
	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

const (
	demoBackfillDays  = 21
	demoBackfillEpoch = "demo-backfill"
	// demoBackfillSeed fixes the past's shape, so two demos tell the same kind of story.
	demoBackfillSeed = 20261001
	// demoBackfillQuiet keeps the newest backfilled print clear of the live
	// loop's first one.
	demoBackfillQuiet = 10 * time.Minute
	// demoFilamentPerSecond is roughly what a 0.4 mm nozzle feeds at speed.
	demoFilamentPerSecond = 0.8
	// demoProblemFailShare is the share of prints the second printer loses, so
	// a statistics page has one machine worth looking at.
	demoProblemFailShare = 0.25

	demoBackfillFirstJob   = 1000
	demoBackfillBusyFrom   = 8
	demoBackfillBusyTo     = 20
	demoBackfillBusyGap    = 10 * time.Minute
	demoBackfillBusySpread = 30 * time.Minute
	demoBackfillIdleGap    = 2 * time.Hour
	demoBackfillIdleSpread = 6 * time.Hour
	demoBackfillShortest   = 20 * time.Minute
	demoBackfillLongestX   = 30
	demoBackfillCutFrom    = 5
	demoBackfillCutSpan    = 90
	demoPercent            = 100
)

//nolint:gochecknoglobals // a fixed list, read-only.
var demoBackfillFiles = []string{
	"benchy.bgcode", "bracket.bgcode", "gear-housing.bgcode",
	"phone-stand.bgcode", "cable-clip.bgcode", "lamp-shade.bgcode",
}

// demoBackfillPrinter is one printer the past is written for, and the share
// of its prints that failed.
type demoBackfillPrinter struct {
	ID        string
	FailShare float64
}

// demoBackfill writes three weeks of settled prints: busy on weekdays from
// eight to eight, sparse at night and at weekends, never two prints at once on
// one printer.
//
// The prints come back twice, as the settled facts the history keeps and as the
// rows the core keeps, with the same ids and outcomes: the history page reads
// the one and a report over the fleet's prints reads the other.
func demoBackfill(
	now time.Time, printers []demoBackfillPrinter,
) ([]historyapi.Fact, []devicestore.PrinterJob, error) {
	rng := rand.New(rand.NewPCG(demoBackfillSeed, 0)) //nolint:gosec // a fixed seed is the point
	stop := now.Add(-demoBackfillQuiet)

	var (
		facts    []historyapi.Fact
		jobs     []devicestore.PrinterJob
		sequence uint64
	)

	for _, printer := range printers {
		origin, err := event.NewOrigin(event.KindDevice, printer.ID)
		if err != nil {
			return nil, nil, err
		}

		at := now.AddDate(0, 0, -demoBackfillDays)
		number := demoBackfillFirstJob

		for {
			at = at.Add(demoBackfillGap(rng, at))
			length := demoBackfillLength(rng)

			if !at.Add(length).Before(stop) {
				break
			}

			number++
			settled := demoBackfillJob(rng, printer, number, at, length)

			payload, marshalErr := json.Marshal(settled)
			if marshalErr != nil {
				return nil, nil, marshalErr
			}

			sequence++
			facts = append(facts, historyapi.Fact{
				At:       settled.Ended,
				Ref:      origin,
				Type:     event.TypeDeviceJobSettled,
				Sequence: sequence,
				Epoch:    demoBackfillEpoch,
				Payload:  payload,
				Class:    event.ClassState,
			})
			jobs = append(jobs, devicestore.PrinterJob{
				JobID:       settled.JobID,
				DeviceID:    printer.ID,
				DeviceJobID: settled.DeviceJobID,
				Path:        settled.Path,
				DisplayName: settled.DisplayName,
				State:       settled.MachineState,
				Progress:    settled.Progress,
				Started:     settled.Started,
				Ended:       settled.Ended,
				Outcome:     settled.Outcome,
			})
			at = settled.Ended
		}
	}

	return facts, jobs, nil
}

// demoBackfillGap is the idle time before the next print, by the clock on the
// wall where the demo runs.
func demoBackfillGap(rng *rand.Rand, at time.Time) time.Duration {
	local := at.Local()
	busy := local.Weekday() != time.Saturday && local.Weekday() != time.Sunday &&
		local.Hour() >= demoBackfillBusyFrom && local.Hour() < demoBackfillBusyTo

	if busy {
		return demoBackfillBusyGap + time.Duration(rng.Int64N(int64(demoBackfillBusySpread)))
	}

	return demoBackfillIdleGap + time.Duration(rng.Int64N(int64(demoBackfillIdleSpread)))
}

// demoBackfillLength is 20 minutes to 10 hours, short prints more common.
func demoBackfillLength(rng *rand.Rand) time.Duration {
	return time.Duration(float64(demoBackfillShortest) * math.Exp(rng.Float64()*math.Log(demoBackfillLongestX)))
}

func demoBackfillJob(
	rng *rand.Rand, printer demoBackfillPrinter, number int, started time.Time, length time.Duration,
) event.JobSettled {
	name := demoBackfillFiles[rng.IntN(len(demoBackfillFiles))]
	outcome, machine, progress := event.JobFinished, "FINISHED", 100.0

	switch fate := rng.Float64(); {
	case fate < printer.FailShare:
		outcome, machine, progress = event.JobFailed, "ERROR", demoBackfillCutFrom+rng.Float64()*demoBackfillCutSpan
	case fate < printer.FailShare+demoCancelShare:
		outcome, machine, progress = event.JobCancelled, "STOPPED", demoBackfillCutFrom+rng.Float64()*demoBackfillCutSpan
	}

	printed := time.Duration(float64(length) * progress / demoPercent)

	var (
		bits   [16]byte
		random [10]byte
	)

	binary.LittleEndian.PutUint64(bits[:8], rng.Uint64())
	binary.LittleEndian.PutUint64(bits[8:], rng.Uint64())
	copy(random[:], bits[:])

	return event.JobSettled{
		JobID:             printIDAt(started, random),
		DeviceJobID:       strconv.Itoa(number),
		Path:              "usb/" + name,
		DisplayName:       name,
		Started:           historyapi.Truncate(started),
		Ended:             historyapi.Truncate(started.Add(printed)),
		Outcome:           outcome,
		MachineState:      machine,
		Progress:          progress,
		TimePrintingS:     int64(printed.Seconds()),
		TimePrintingKnown: true,
		FilamentUsedMM:    printed.Seconds() * demoFilamentPerSecond,
		FilamentUsedKnown: true,
	}
}

// backfill writes the past into the demo's history file. The provider the
// core supervises has no handle the demo can reach, so this opens a second
// one on the same SQLite file, writes under its own epoch so no key meets a
// live fact, and closes it.
func (f *demoFleet) backfill(ctx context.Context, opts DemoOptions, dir string, page demoListing) string {
	var existing struct {
		Items []json.RawMessage `json:"items"`
	}

	url := opts.baseURL() + "/api/v1/history/facts?limit=1&types=" + event.TypeDeviceJobSettled +
		"&to=" + time.Now().Add(-demoBackfillQuiet).UTC().Format(time.RFC3339)

	// The history instance can still be starting once the fleet has arrived, and
	// until it serves, this read answers 501.
	if err := f.await(ctx, url, demoStartBudget, func(demoListing) bool { return true }); err != nil {
		return "no past was written: history never answered: " + err.Error()
	}

	if status, err := f.call(ctx, http.MethodGet, url, "", &existing); err != nil || status != http.StatusOK {
		return "no past was written: could not tell whether history already holds one"
	}

	if len(existing.Items) > 0 {
		return "history already holds the past"
	}

	ids := map[string]string{}
	for _, device := range page.Items {
		ids[device.Serial] = device.ID
	}

	var printers []demoBackfillPrinter

	for i, printer := range f.printers {
		id, ok := ids[printer.Serial()]
		if !ok {
			continue
		}

		share := demoFailShare
		if i == 1 {
			share = demoProblemFailShare
		}

		printers = append(printers, demoBackfillPrinter{ID: id, FailShare: share})
	}

	facts, jobs, err := demoBackfill(time.Now(), printers)
	if err != nil {
		return "no past was written: " + err.Error()
	}

	config, err := json.Marshal(map[string]any{
		"path":      filepath.Join(dir, "history.db"),
		"retention": map[string]string{"raw": "0"},
	})
	if err != nil {
		return "no past was written: " + err.Error()
	}

	provider, err := sqlite.New(historyapi.ProviderSpec{ID: "past", Type: sqlite.Type, Config: config})
	if err != nil {
		return "no past was written: " + err.Error()
	}

	if closer, ok := provider.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}

	if err = provider.AppendFacts(ctx, facts); err != nil {
		return "no past was written: " + err.Error()
	}

	if err = demoBackfillJobs(ctx, dir, jobs); err != nil {
		return "the past is in history and not among the prints: " + err.Error()
	}

	return fmt.Sprintf("history and the prints hold %d prints from the last %d days", len(facts), demoBackfillDays)
}

// demoBackfillJobs writes the same prints into the core's own table of prints,
// on a second handle to the host database, the way backfill writes history.
// The devices are there already, because the fleet arrived before this runs.
func demoBackfillJobs(ctx context.Context, dir string, jobs []devicestore.PrinterJob) error {
	db, err := authz.OpenSQLite(ctx, config.DefaultGraph(filepath.Join(dir, "config.yaml")), sqlstore.Pool{})
	if err != nil {
		return err
	}

	defer func() { _ = db.Close() }()

	store, err := devicestore.New(db, engineOf(authz.DialectSQLite))
	if err != nil {
		return err
	}

	return store.SaveDeviceJobs(ctx, jobs...)
}
