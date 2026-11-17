package app

import (
	"encoding/json"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// A machine sends its last progress while it is still printing and sends none
// once it says FINISHED, so the last number it sent is a second short of the
// end. A finished print is the whole print: the row, the settled frame and the
// device listing say 100. A print that was stopped keeps the figure it stopped
// at, because that figure is the answer to how far it got.
func TestAFinishedPrintReadsTheWholePrintAndAStoppedOneWhereItStopped(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		end      map[string]string
		progress float64
		outcome  string
	}{
		"finished with no last progress": {
			end:      map[string]string{"/state": `"FINISHED"`},
			progress: 100,
			outcome:  event.JobUnknown,
		},
		"finished with the driver's outcome": {
			end:      map[string]string{"/state": `"FINISHED"`, "/job/outcome": `"finished"`},
			progress: 100,
			outcome:  event.JobFinished,
		},
		"stopped part way": {
			end:      map[string]string{"/state": `"STOPPED"`, "/job/outcome": `"cancelled"`},
			progress: 99,
			outcome:  event.JobCancelled,
		},
		// The machine's word says FINISHED and the driver says the print was
		// cancelled. The outcome decides, so the figure it stopped at stays.
		"cancelled under a FINISHED word": {
			end: map[string]string{
				"/state": `"FINISHED"`, "/job/state": `"FINISHED"`, "/job/outcome": `"cancelled"`,
			},
			progress: 99,
			outcome:  event.JobCancelled,
		},
		"failed under a FINISHED word": {
			end: map[string]string{
				"/state": `"FINISHED"`, "/job/state": `"FINISHED"`, "/job/outcome": `"failed"`,
			},
			progress: 99,
			outcome:  event.JobFailed,
		},
		// A driver with no outcome whose print word says FINISHED while the
		// machine says STOPPED was stopped; it keeps where it stopped.
		"stopped under a stale FINISHED with no outcome": {
			end:      map[string]string{"/state": `"STOPPED"`, "/job/state": `"FINISHED"`},
			progress: 99,
			outcome:  event.JobUnknown,
		},
		"failed with no outcome": {
			end:      map[string]string{"/state": `"ERROR"`, "/job/state": `"FINISHED"`},
			progress: 99,
			outcome:  event.JobUnknown,
		},
		"failed part way": {
			end:      map[string]string{"/state": `"IDLE"`, "/job/outcome": `"failed"`},
			progress: 99,
			outcome:  event.JobFailed,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				plane, bus, graph := planeBusAndGraph(t)
				id, session := arriveWithSession(t, plane, "SIM-JOB-PROGRESS")

				reportPrint(t, plane, session, map[string]string{
					"/state":                `"PRINTING"`,
					"/job/id":               `"5"`,
					"/job/path":             `"/usb/tangle.bgcode"`,
					"/job/progress":         `99`,
					"/job/time_remaining":   `1`,
					"/job/time_printing":    `239`,
					"/job/filament_used_mm": `1200`,
				})
				bus.awaitJobFrames(t, 1)

				reportPrint(t, plane, session, tc.end)
				frames := bus.awaitJobFrames(t, 2)

				var settled event.JobSettled
				require.NoError(t, json.Unmarshal(frames[1].Data, &settled))
				require.InDelta(t, tc.progress, settled.Progress, 0.001,
					"the settled frame carries the wrong progress")
				require.Equal(t, tc.outcome, settled.Outcome)

				rows, err := recordsOf(t, graph).DeviceJobs(t.Context(), id, 1)
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.InDelta(t, tc.progress, rows[0].Progress, 0.001,
					"the stored row carries the wrong progress")

				require.NoError(t, graph.Write(t.Context(),
					tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

				device, err := plane.Get(t.Context(), manager, id)
				require.NoError(t, err)
				require.NotNil(t, device.Job)
				require.InDelta(t, tc.progress, device.Job.Progress, 0.001,
					"the device listing carries the wrong progress")
			})
		})
	}
}
