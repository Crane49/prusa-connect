package app

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// arriveWithSession puts one printer in the plane and hands back the session a
// case pushes readings through.
func arriveWithSession(t *testing.T, plane *devices, serial string) (string, *countedSession) {
	t.Helper()

	id, held := arriveHeld(t, plane, serial, connectorapi.Set{})
	close(held.release)

	return id, &held.countedSession
}

// The frames a print puts on the bus, and the rule that each of them is
// published once.
//
// Every case here drives the plane the way a driver does: patches into the
// session's channel, and a flush. The flush is what writes the row and
// publishes, so calling it is the same thing the flush goroutine does on its
// tick.

// jobFrames is the print frames the bus carried, in arrival order.
func (r *busReader) jobFrames() []event.Envelope {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]event.Envelope, 0, len(r.frames))

	for _, envelope := range r.frames {
		if strings.HasPrefix(envelope.Type, event.CoreNamespace+"device.job.") {
			out = append(out, envelope)
		}
	}

	return out
}

// awaitJobFrames waits until the bus has carried at least want print frames,
// and returns every one it had carried by then.
func (r *busReader) awaitJobFrames(t *testing.T, want int) []event.Envelope {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for {
		if got := r.jobFrames(); len(got) >= want {
			return got
		}

		if time.Now().After(deadline) {
			t.Fatalf("the bus carried %d print frames and %d were expected", len(r.jobFrames()), want)
		}

		time.Sleep(5 * time.Millisecond)
	}
}

// reportPrint puts one reading of a print into the plane and flushes it, which
// is what a telemetry frame followed by the flush tick does.
func reportPrint(t *testing.T, plane *devices, session *countedSession, paths map[string]string) {
	t.Helper()

	for _, path := range sortedPaths(paths) {
		session.patches <- connectorapi.StatePatch{
			Path:  path,
			Value: json.RawMessage(paths[path]),
		}
	}

	synctest.Wait()
	plane.flush()
	synctest.Wait()
}

// sortedPaths is the paths of one reading in a fixed order, so two runs of a
// case send the same sequence.
func sortedPaths(paths map[string]string) []string {
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}

	slices.Sort(out)

	return out
}

// TestAPrintPublishesOneStartedFrameAndOneSettledFrame is the shape of the
// pair: the print opens once, it ends once, and the second frame says how.
func TestAPrintPublishesOneStartedFrameAndOneSettledFrame(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)
		_, session := arriveWithSession(t, plane, "SIM-JOB-EVENTS")

		reportPrint(t, plane, session, map[string]string{
			"/state":        `"PRINTING"`,
			"/job/id":       `"7"`,
			"/job/path":     `"/usb/tangle.bgcode"`,
			"/job/display":  `"tangle.bgcode"`,
			"/job/progress": `12`,
		})

		frames := bus.awaitJobFrames(t, 1)
		require.Len(t, frames, 1, "the start of a print published something other than one frame")
		require.Equal(t, event.TypeDeviceJobStarted, frames[0].Type)

		var started event.JobStarted
		require.NoError(t, json.Unmarshal(frames[0].Data, &started))

		require.Regexp(t, printIDPattern, started.JobID)
		require.Equal(t, "7", started.DeviceJobID)
		require.Equal(t, "/usb/tangle.bgcode", started.Path)
		require.Equal(t, "tangle.bgcode", started.DisplayName)
		require.False(t, started.Started.IsZero())
		require.Empty(t, started.CommandID,
			"a print nobody commanded named an instruction")

		// Progress moves. The row is rewritten on every reading and the bus is
		// told nothing: a print starts once.
		reportPrint(t, plane, session, map[string]string{"/job/progress": `48`})
		require.Len(t, bus.jobFrames(), 1, "a print that moved published a second start")

		reportPrint(t, plane, session, map[string]string{
			"/state":        `"FINISHED"`,
			"/job/state":    `"FINISHED"`,
			"/job/outcome":  `"finished"`,
			"/job/progress": `100`,
		})

		frames = bus.awaitJobFrames(t, 2)
		require.Len(t, frames, 2, "the end of a print published something other than one frame")
		require.Equal(t, event.TypeDeviceJobSettled, frames[1].Type)
		require.Equal(t, started.JobID, frames[1].CausationID,
			"the two frames of one print are not joined by the job id")

		var settled event.JobSettled
		require.NoError(t, json.Unmarshal(frames[1].Data, &settled))

		require.Equal(t, started.JobID, settled.JobID)
		require.Equal(t, event.JobFinished, settled.Outcome)
		require.Equal(t, "FINISHED", settled.MachineState)
		require.InDelta(t, 100.0, settled.Progress, 0.001)
		require.True(t, started.Started.Equal(settled.Started),
			"the settled frame says the print began at a different moment from the started one")
		require.False(t, settled.Ended.IsZero())
		require.False(t, settled.FilamentUsedKnown,
			"a machine that reported no filament was published as having used none")
	})
}

// TestAStartedFrameNamesTheFileHoweverTheReadingArrives is the flush landing
// in the middle of one reading.
//
// A machine's reading reaches this server a path at a time, so the flush timer
// can fire after the job id and before the file the print is for. The frame
// published then named an empty file and a consumer had nothing to show. Both
// orders are run, because a driver sends the file before the id as readily as
// after it, and there is a flush between every pair of paths, which is the
// worst the timer can do.
func TestAStartedFrameNamesTheFileHoweverTheReadingArrives(t *testing.T) {
	t.Parallel()

	reading := map[string]string{
		"/job/id":      `"21"`,
		"/job/path":    `"/usb/tangle.bgcode"`,
		"/job/display": `"tangle.bgcode"`,
		"/state":       `"PRINTING"`,
	}

	orders := map[string][]string{
		"the id first":   {"/job/id", "/job/path", "/job/display", "/state"},
		"the file first": {"/job/path", "/job/display", "/job/id", "/state"},
	}

	for name, order := range orders {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				plane, bus, _ := planeBusAndGraph(t)
				_, session := arriveWithSession(t, plane, "SIM-JOB-ORDER")

				for _, path := range order {
					session.patches <- connectorapi.StatePatch{
						Path:  path,
						Value: json.RawMessage(reading[path]),
					}

					synctest.Wait()
					plane.flush()
					synctest.Wait()
				}

				frames := bus.awaitJobFrames(t, 1)
				require.Len(t, frames, 1,
					"one print published something other than one started frame")

				var started event.JobStarted
				require.NoError(t, json.Unmarshal(frames[0].Data, &started))

				require.Regexp(t, printIDPattern, started.JobID)
				require.Equal(t, "/usb/tangle.bgcode", started.Path,
					"the frame names no file, so the flush cut the reading in half")
				require.Equal(t, "tangle.bgcode", started.DisplayName)
			})
		})
	}
}

// TestTwoTerminalReadingsSettleAPrintOnce is the machine saying it twice. The
// row takes the first end it is given, and so does the bus.
func TestTwoTerminalReadingsSettleAPrintOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)
		_, session := arriveWithSession(t, plane, "SIM-JOB-TWICE")

		reportPrint(t, plane, session, map[string]string{
			"/state":  `"PRINTING"`,
			"/job/id": `"11"`,
		})
		bus.awaitJobFrames(t, 1)

		reportPrint(t, plane, session, map[string]string{
			"/state":       `"STOPPED"`,
			"/job/state":   `"STOPPED"`,
			"/job/outcome": `"cancelled"`,
		})
		frames := bus.awaitJobFrames(t, 2)

		// The machine tidies up and says so again.
		reportPrint(t, plane, session, map[string]string{"/state": `"IDLE"`})

		require.Len(t, bus.jobFrames(), 2, "a second terminal reading settled the print again")

		var settled event.JobSettled
		require.NoError(t, json.Unmarshal(frames[1].Data, &settled))
		require.Equal(t, event.JobCancelled, settled.Outcome)
	})
}

// TestAPrintThatEndsWithNoWordForItIsUnknown is the power cut. The machine goes
// back to idle over a print that was running and says nothing about it, and the
// core does not guess: it knows no machine's vocabulary.
func TestAPrintThatEndsWithNoWordForItIsUnknown(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)
		_, session := arriveWithSession(t, plane, "SIM-JOB-UNKNOWN")

		reportPrint(t, plane, session, map[string]string{
			"/state":  `"PRINTING"`,
			"/job/id": `"3"`,
		})
		bus.awaitJobFrames(t, 1)

		reportPrint(t, plane, session, map[string]string{"/state": `"READY"`})
		frames := bus.awaitJobFrames(t, 2)

		var settled event.JobSettled
		require.NoError(t, json.Unmarshal(frames[1].Data, &settled))
		require.Equal(t, event.JobUnknown, settled.Outcome)
		require.Equal(t, "READY", settled.MachineState,
			"the machine's own last word is not on the frame")
	})
}

// TestAPrinterThatGoesAwayMidPrintSettlesNothing is the printer that loses its
// connection. The print is still on its bed and nothing here knows how it went,
// so the row stays open and the bus is told nothing until the machine comes
// back and says.
func TestAPrinterThatGoesAwayMidPrintSettlesNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)
		_, session := arriveWithSession(t, plane, "SIM-JOB-AWAY")

		reportPrint(t, plane, session, map[string]string{
			"/state":  `"PRINTING"`,
			"/job/id": `"5"`,
		})
		bus.awaitJobFrames(t, 1)

		// The printer is away. It reports nothing, so every flush while it is
		// gone has nothing to write and nothing to say about the print.
		for range 3 {
			synctest.Wait()
			plane.flush()
			synctest.Wait()
		}

		require.Len(t, bus.jobFrames(), 1,
			"a printer that went away mid-print had its print settled for it")

		reportPrint(t, plane, session, map[string]string{
			"/state":       `"FINISHED"`,
			"/job/state":   `"FINISHED"`,
			"/job/outcome": `"finished"`,
		})

		frames := bus.awaitJobFrames(t, 2)

		var settled event.JobSettled
		require.NoError(t, json.Unmarshal(frames[1].Data, &settled))
		require.Equal(t, event.JobFinished, settled.Outcome)
	})
}

// TestAPrintTheStoreAlreadyHoldsIsNotStartedAgain is the restart, and the
// machine that was printing when this process began.
//
// The row is in the store because a run before this one opened it and published
// the start. This process matches the machine to that print again and must
// publish nothing for it, or every restart during a long print would tell
// a queue that the print had begun again.
func TestAPrintTheStoreAlreadyHoldsIsNotStartedAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)
		_, session := arriveWithSession(t, plane, "SIM-JOB-RESTART")

		reportPrint(t, plane, session, map[string]string{
			"/state":  `"PRINTING"`,
			"/job/id": `"9"`,
		})
		started := bus.awaitJobFrames(t, 1)

		var opened event.JobStarted
		require.NoError(t, json.Unmarshal(started[0].Data, &opened))

		// The restart: this process has published the frames of no print, and
		// the row is in the store. Everything else is as it was, which is what a
		// fleet restored out of device_record gives a new process.
		clear(plane.announcedJobs)

		reportPrint(t, plane, session, map[string]string{"/job/progress": `61`})

		require.Len(t, bus.jobFrames(), 1,
			"a print the store already held was started a second time")

		reportPrint(t, plane, session, map[string]string{
			"/state":       `"FINISHED"`,
			"/job/state":   `"FINISHED"`,
			"/job/outcome": `"finished"`,
		})

		frames := bus.awaitJobFrames(t, 2)

		var settled event.JobSettled
		require.NoError(t, json.Unmarshal(frames[1].Data, &settled))

		require.True(t, opened.Started.Equal(settled.Started),
			"the restart moved the start of a print that began before it")
	})
}

// TestAPrintThatEndedBeforeTheRestartIsNotSettledAgain is the other half of the
// restart. The row in the store has an end, so this process publishes nothing
// about it however many readings restate it.
func TestAPrintThatEndedBeforeTheRestartIsNotSettledAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)
		_, session := arriveWithSession(t, plane, "SIM-JOB-ENDED")

		reportPrint(t, plane, session, map[string]string{
			"/state":  `"PRINTING"`,
			"/job/id": `"13"`,
		})
		bus.awaitJobFrames(t, 1)

		reportPrint(t, plane, session, map[string]string{
			"/state":     `"STOPPED"`,
			"/job/state": `"STOPPED"`,
		})
		bus.awaitJobFrames(t, 2)

		clear(plane.announcedJobs)

		reportPrint(t, plane, session, map[string]string{"/job/progress": `77`})

		require.Len(t, bus.jobFrames(), 2,
			"a print that had already ended was settled again after a restart")
	})
}

// TestAFilamentReadingReachesTheSettledFrame is the optional path. A driver
// that reports it says how much the print used; one that does not leaves the
// field absent, which is what the case above holds.
func TestAFilamentReadingReachesTheSettledFrame(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, bus, _ := planeBusAndGraph(t)
		_, session := arriveWithSession(t, plane, "SIM-JOB-FILAMENT")

		reportPrint(t, plane, session, map[string]string{
			"/state":  `"PRINTING"`,
			"/job/id": `"17"`,
		})
		bus.awaitJobFrames(t, 1)

		reportPrint(t, plane, session, map[string]string{
			"/state":                `"FINISHED"`,
			"/job/state":            `"FINISHED"`,
			"/job/outcome":          `"finished"`,
			"/job/time_printing":    `1840`,
			"/job/filament_used_mm": `0`,
		})

		frames := bus.awaitJobFrames(t, 2)

		var settled event.JobSettled
		require.NoError(t, json.Unmarshal(frames[1].Data, &settled))

		require.True(t, settled.FilamentUsedKnown,
			"a machine that reported no filament used reads as one that reported nothing")
		require.InDelta(t, 0.0, settled.FilamentUsedMM, 0.001)
		require.True(t, settled.TimePrintingKnown)
		require.Equal(t, int64(1840), settled.TimePrintingS)
	})
}
