package devicestore

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
)

// What one page of the fleet's prints costs at the size this product claims:
// four hundred printers, a thousand ended prints each and one running.
//
//	go test -run '^$' -bench BenchmarkFleetJobs -benchtime 20x ./internal/platform/devicestore/

// benchPrintsPerDevice is how many ended prints each printer holds.
const benchPrintsPerDevice = 1000

// benchPage is the page the Jobs page asks for, plus the row that says whether
// another page follows.
const benchPage = 201

// seedPrints writes the prints onto the measured fleet and gives one person a
// viewer grant on every printer. Each printer's newest print is running.
func seedPrints(b *testing.B, store fixture) {
	b.Helper()

	batch := make([]PrinterJob, 0, jobBatchRows)
	flush := func() {
		require.NoError(b, store.SaveDeviceJobs(b.Context(), batch...))
		batch = batch[:0]
	}

	grants := make([]authz.Tuple, 0, fleetDevices)

	for device := range fleetDevices {
		id := fleetDeviceID(device)
		grants = append(grants, mustTuple(b, "device:"+id+"#viewer@user:bench"))

		for number := range benchPrintsPerDevice + 1 {
			started := startedAt.Add(time.Duration(number*fleetDevices+device) * time.Minute)
			job := PrinterJob{
				JobID: fmt.Sprintf("%s:%d", id, number), DeviceID: id, DeviceJobID: strconv.Itoa(number),
				Path: "/usb/part.bgcode", DisplayName: "part.bgcode",
				State: "FINISHED", Progress: 100, Started: started,
			}

			if number < benchPrintsPerDevice {
				job.Ended = started.Add(time.Hour)
			} else {
				job.State = "PRINTING"
			}

			batch = append(batch, job)
			if len(batch) == jobBatchRows {
				flush()
			}
		}
	}

	if len(batch) > 0 {
		flush()
	}

	require.NoError(b, store.Write(b.Context(), grants...))
}

func BenchmarkFleetJobs(b *testing.B) {
	for _, e := range benchEngines(b) {
		b.Run(e.Name, func(b *testing.B) {
			store := seedFleet(b, e)
			seedPrints(b, store)

			who := subjectOf(b, "user:bench")

			for name, filter := range map[string]JobFilter{
				"every":      {},
				"running":    {Phase: JobPhaseRunning},
				"ended":      {Phase: JobPhaseEnded},
				"one-device": {DeviceID: fleetDeviceID(200)},
			} {
				b.Run(name, func(b *testing.B) {
					filter.Limit = benchPage

					for b.Loop() {
						rows, err := store.FleetJobs(b.Context(), who, filter)
						if err != nil || len(rows) == 0 {
							require.NoError(b, err)
							require.NotEmpty(b, rows)
						}
					}
				})
			}
		})
	}
}
