package xbuddy

// What /job/state holds while the printer volunteers nothing about the print,
// and how often the driver asks. Both are BUG-004: the firmware sends a
// JOB_INFO when a print starts and never again, so the tree's word for a print
// is whatever this driver puts there.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// TestTheTerminalStateSticksWhenTheMachineGoesBackToIdle is the blocker half of
// BUG-004 that survives the machine tidying up.
//
// A real MK4S reports STOPPED for a moment and then IDLE, and it volunteers no
// JOB_INFO for either. A reader deriving the print's state from /state alone
// therefore saw the print running again as soon as the machine idled, because
// /job/state still held the PRINTING the frame at the start of the print left
// there. The driver writes the terminal word instead, and holds it against the
// JOB_INFO that a refresh brings back saying IDLE.
func TestTheTerminalStateSticksWhenTheMachineGoesBackToIdle(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	// The frame the firmware really volunteers, at the start of the print and
	// nowhere else.
	printer.text(t, `{"event":"JOB_INFO","state":"PRINTING",`+
		`"data":{"state":"PRINTING","path":"/usb/bench-mk4s.gcode","job_id":233}}`)
	printer.awaitDialogTree(t, map[string]string{jobStatePath: `"PRINTING"`})

	// The stop, as the machine reports it: a state on a telemetry frame and
	// nothing about the print.
	printer.text(t, `{"state":"STOPPED","job_id":233}`)
	printer.awaitDialogTree(t, map[string]string{jobStatePath: `"STOPPED"`})

	// The machine tidying up, followed by a frame with something else to say so
	// that the reader has demonstrably passed the IDLE one.
	printer.text(t, `{"state":"IDLE"}`)
	printer.text(t, `{"state":"IDLE","temp_nozzle":215}`)
	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "215"})

	if held := printer.dialogValue(jobStatePath); held != `"STOPPED"` {
		t.Errorf("%s reads %s once the machine idled, want \"STOPPED\": a print that was aborted is "+
			"stored as how it ended", jobStatePath, held)
	}

	// And the answer to the refresh the stop asked for, which on a machine with
	// no job left is IDLE. It says nothing about how the print ended.
	printer.text(t, `{"event":"JOB_INFO","state":"IDLE",`+
		`"data":{"state":"IDLE","path":"/usb/bench-mk4s.gcode","job_id":233}}`)
	printer.text(t, `{"state":"IDLE","temp_nozzle":40}`)
	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "40"})

	if held := printer.dialogValue(jobStatePath); held != `"STOPPED"` {
		t.Errorf("%s reads %s after a JOB_INFO for a machine with no job, want \"STOPPED\"",
			jobStatePath, held)
	}

	t.Logf("observed: the print's own last word is %s with the machine at IDLE",
		printer.dialogValue(jobStatePath))
}

// TestAPrintCancelledAtThePrinterBeforeItStartedIsRecordedAsStopped is the end
// that has no STOPPED. Cancelled from the LCD at the filament check, a CORE One
// goes from ATTENTION straight to IDLE, and nothing else on the wire says the
// print ended.
func TestAPrintCancelledAtThePrinterBeforeItStartedIsRecordedAsStopped(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"event":"JOB_INFO","state":"PRINTING",`+
		`"data":{"state":"PRINTING","path":"/usb/bench-core.gcode","job_id":233}}`)
	printer.awaitDialogTree(t, map[string]string{jobStatePath: `"PRINTING"`})

	printer.text(t, `{"state":"ATTENTION","job_id":233}`)
	printer.text(t, `{"state":"IDLE"}`)
	printer.awaitDialogTree(t, map[string]string{
		jobStatePath:   `"STOPPED"`,
		jobOutcomePath: `"cancelled"`,
	})

	printer.text(t, `{"event":"JOB_INFO","state":"IDLE",`+
		`"data":{"state":"IDLE","path":"/usb/bench-core.gcode","job_id":233}}`)
	printer.text(t, `{"state":"IDLE","temp_nozzle":40}`)
	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "40"})

	if held := printer.dialogValue(jobStatePath); held != `"STOPPED"` {
		t.Errorf("%s reads %s after a JOB_INFO for a machine with no job, want \"STOPPED\"", jobStatePath, held)
	}

	if held := printer.dialogValue(jobOutcomePath); held != `"cancelled"` {
		t.Errorf("%s reads %s, want \"cancelled\"", jobOutcomePath, held)
	}
}

// TestADialogOnAMachineThatNamedNoPrintEndsNothing is the negative of the
// above: a session that has heard of no print has none to stop.
func TestADialogOnAMachineThatNamedNoPrintEndsNothing(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"state":"IDLE"}`)
	printer.text(t, `{"state":"ATTENTION"}`)
	printer.text(t, `{"state":"IDLE"}`)
	printer.text(t, `{"state":"IDLE","temp_nozzle":40}`)
	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "40"})

	if held := printer.dialogValue(jobStatePath); held != "" {
		t.Errorf("%s reads %s, want nothing: no print was ever named", jobStatePath, held)
	}
}

// TestAPrintThatFinishedOnItsOwnIsRecordedAsFinished is the other terminal
// word. A print reaching its own end is a state change like any other on this
// dialect, and the firmware volunteers nothing for it either.
func TestAPrintThatFinishedOnItsOwnIsRecordedAsFinished(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"event":"JOB_INFO","state":"PRINTING",`+
		`"data":{"state":"PRINTING","path":"/usb/bench-mk4s.gcode","job_id":234}}`)
	printer.awaitDialogTree(t, map[string]string{jobStatePath: `"PRINTING"`})

	printer.text(t, `{"state":"FINISHED","job_id":234}`)
	printer.awaitDialogTree(t, map[string]string{jobStatePath: `"FINISHED"`})

	printer.text(t, `{"state":"IDLE","temp_nozzle":215}`)
	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "215"})

	if held := printer.dialogValue(jobStatePath); held != `"FINISHED"` {
		t.Errorf("%s reads %s after a print that ran to its end, want \"FINISHED\"",
			jobStatePath, held)
	}
}

// TestAFinishedPrintReportsTheWholePrint is the last progress frame. The
// firmware stops sending progress when it leaves PRINTING, so the last value is
// taken a second before the end. The driver writes 100 for a print that
// finished, and leaves a stopped print at the figure it stopped at.
func TestAFinishedPrintReportsTheWholePrint(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		end      string
		progress string
		left     string
	}{
		"finished": {end: "FINISHED", progress: "100", left: "0"},
		"stopped":  {end: "STOPPED", progress: "99", left: "3"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			printer := standHostile(t)

			printer.text(t, `{"event":"JOB_INFO","state":"PRINTING",`+
				`"data":{"state":"PRINTING","path":"/usb/bench-mk4s.gcode","job_id":240}}`)
			printer.text(t, `{"state":"PRINTING","job_id":240,"progress":99,"time_remaining":3}`)
			printer.awaitDialogTree(t, map[string]string{"/job/progress": "99"})

			printer.text(t, `{"state":"`+tc.end+`","job_id":240}`)
			printer.text(t, `{"state":"`+tc.end+`","temp_nozzle":215}`)
			printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "215"})

			if held := printer.dialogValue("/job/progress"); held != tc.progress {
				t.Errorf("/job/progress reads %s after the print ended %s, want %s", held, name, tc.progress)
			}

			if held := printer.dialogValue("/job/time_remaining"); held != tc.left {
				t.Errorf("/job/time_remaining reads %s after the print ended %s, want %s", held, name, tc.left)
			}
		})
	}
}

// TestAMachineThatNeverNamedAPrintGetsNoJobState keeps the terminal word from
// inventing a print. A machine left STOPPED on the shelf reports that word on
// the first frame of every connection, and a host that wrote it to /job/state
// would put a print in the history of a device that has run none.
func TestAMachineThatNeverNamedAPrintGetsNoJobState(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"state":"STOPPED"}`)
	printer.text(t, `{"state":"STOPPED","temp_nozzle":215}`)
	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "215"})

	if held := printer.dialogValue(jobStatePath); held != "" {
		t.Errorf("%s reads %s on a machine that has named no print, want nothing", jobStatePath, held)
	}
}

// TestAFlapBetweenAttentionAndPrintingIsAskedAboutOnce bounds the refresh.
//
// ATTENTION is the state a machine reports while a dialog is on its screen, and
// an operator who leaves one up over a running print flaps between ATTENTION
// and PRINTING at the telemetry rate. A rule that asked on every change of
// value therefore spent a SEND_JOB_INFO and a SEND_STATE_INFO every second for
// as long as the dialog stood, on a link that carries one command at a time.
func TestAFlapBetweenAttentionAndPrintingIsAskedAboutOnce(t *testing.T) {
	t.Parallel()

	// Two hundred frames is fifty seconds of a real machine at 4 Hz.
	states := make([]string, 0, 200)
	for range 100 {
		states = append(states, stateAttention, statePrinting)
	}

	asked, held := 0, (*string)(nil)

	for _, state := range states {
		if worthAskingAbout(held, state) {
			asked++
		}

		held = &state
	}

	if asked != 1 {
		t.Errorf("a flap of %d frames between ATTENTION and PRINTING asks %d times, want 1",
			len(states), asked)
	}

	t.Logf("observed: %d frames of ATTENTION and PRINTING cost %d requests", len(states), asked)
}

// TestOnlyAMoveAcrossThePrintAsksThePrinter is the rest of the rule, as a
// table. The set a state belongs to is what decides, and not the word.
func TestOnlyAMoveAcrossThePrintAsksThePrinter(t *testing.T) {
	t.Parallel()

	for _, one := range []struct {
		name     string
		previous string
		first    bool
		state    string
		want     bool
	}{
		{name: "the first state of a connection", first: true, state: statePrinting, want: true},
		{name: "the same state again", previous: statePrinting, state: statePrinting, want: false},
		{name: "a print starting", previous: "IDLE", state: statePrinting, want: true},
		{name: "a dialog over a print", previous: statePrinting, state: stateAttention, want: false},
		{name: "the dialog answered", previous: stateAttention, state: statePrinting, want: false},
		{name: "a print paused", previous: statePrinting, state: statePaused, want: false},
		{name: "a print stopped", previous: statePaused, state: stateStopped, want: true},
		{name: "a print finished", previous: statePrinting, state: stateFinished, want: true},
		{name: "the machine tidying up", previous: stateStopped, state: "IDLE", want: false},
	} {
		t.Run(one.name, func(t *testing.T) {
			t.Parallel()

			var previous *string
			if !one.first {
				previous = &one.previous
			}

			if got := worthAskingAbout(previous, one.state); got != one.want {
				t.Errorf("a move from %q to %q asks %v, want %v", one.previous, one.state, got, one.want)
			}
		})
	}
}

// TestAVerbThePrinterRefusedIsNotAskedAbout keeps a refusal off the wire twice.
// A pause refused because there is no print moved nothing, and the refresh it
// asked for was a pair of frames spent to be told what the tree already holds.
func TestAVerbThePrinterRefusedIsNotAskedAbout(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	answered := printer.startVerb(t, VerbPausePrint)

	id := printer.takeFrame(t)
	printer.rejectFrame(t, id)

	result, err := printer.await(t, answered)
	if err != nil {
		t.Fatalf("the refused pause did not settle: %v", err)
	}

	if result.OK {
		t.Fatalf("the printer rejected the pause and the session reported it carried out: %s", result.Raw)
	}

	// The refresh waits jobRefreshDelay before it asks, so the wire is watched
	// for longer than that.
	time.Sleep(2 * jobRefreshDelay)
	printer.expectQuiet(t)

	t.Log("observed: a refused pause put nothing further on the wire")
}

// startVerb offers one named verb and hands back the channel its outcome
// arrives on. hostile.start is the same thing for BEEP, and a case about what a
// verb costs on the wire needs a verb that moves the print.
func (h *hostile) startVerb(t *testing.T, verb connectorapi.Capability) chan outcome {
	t.Helper()

	answered := make(chan outcome, 1)

	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), hostileBudget)
		defer cancel()

		result, err := h.session.Do(ctx, connectorapi.Command{Verb: verb})
		answered <- outcome{result: result, err: err}
	}()

	return answered
}

// rejectFrame refuses one command as the printer does. It carries no state, so
// what the case measures afterwards is the refusal and not a state change the
// frame happened to report.
func (h *hostile) rejectFrame(t *testing.T, id uint32) {
	t.Helper()

	frame := fmt.Sprintf(`{"event":"REJECTED","command_id":%d,"reason":"INVALID_STATE"}`, id)
	if err := h.client.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
		t.Fatalf("refuse command %d: %v", id, err)
	}
}

// TestAnEventsOwnStateReachesTheTreeBeforeTelemetryRepeatsIt is the race the
// e2e print gate found about one run in three. The firmware's JOB_INFO at the
// start of a print carries PRINTING on its envelope and arrives before the
// telemetry frame that says the same; a record flush landing between the two
// read a running job against a machine the tree still called IDLE and closed
// the print before it had started. The envelope's word is the machine's own,
// so it is written where telemetry would write it.
func TestAnEventsOwnStateReachesTheTreeBeforeTelemetryRepeatsIt(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"state":"IDLE","temp_nozzle":25}`)
	printer.awaitDialogTree(t, map[string]string{telemetryPaths["state"]: `"IDLE"`})

	// The one frame the firmware volunteers, and no telemetry after it yet.
	printer.text(t, `{"event":"JOB_INFO","state":"PRINTING",`+
		`"data":{"state":"PRINTING","path":"/usb/bench-mk4s.gcode","job_id":235}}`)
	printer.awaitDialogTree(t, map[string]string{
		telemetryPaths["state"]: `"PRINTING"`,
		jobStatePath:            `"PRINTING"`,
	})

	t.Logf("observed: %s reads %s off the JOB_INFO envelope with no telemetry frame after it",
		telemetryPaths["state"], printer.dialogValue(telemetryPaths["state"]))
}

// TestTheMachineStateWinsOverAStaleJobStateWhileThePrintRuns is BUG-013.
//
// The firmware renders JOB_INFO's `state` as PRINTING only while its own device
// state is exactly Printing, and as PAUSED for every other one, Paused,
// ATTENTION and Busy alike (Prusa-Firmware-Buddy, src/connect/render.cpp, the
// EventType::JobInfo arm). A print started through the command surface raises
// the print-preview dialog, so the machine is in ATTENTION when the refresh
// pump's SEND_JOB_INFO is answered, and the answer says PAUSED about a print
// that has not been paused. Nothing then contradicted it: the operator's answer
// moves the machine ATTENTION -> PRINTING, which is a flap inside
// machineIsHoldingAPrint and therefore not worth asking about, so /job/state
// held PAUSED for the whole visible middle of the print, thirteen minutes of
// continuous /state PRINTING on an MK4S, with progress climbing throughout.
//
// The machine's own /state is the strong witness: it arrives on every telemetry
// frame, and it is the field the firmware itself derives that PAUSED from.
func TestTheMachineStateWinsOverAStaleJobStateWhileThePrintRuns(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	// The print-preview dialog, raised by START_PRINT before the machine prints.
	printer.text(t, `{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":42,`+
		`"data":{"code":"13600","title":"Print preview","buttons":["PRINT","CANCEL"]}}`)
	printer.text(t, `{"state":"ATTENTION","job_id":233}`)
	printer.awaitDialogTree(t, map[string]string{telemetryPaths["state"]: `"ATTENTION"`})

	// The refresh pump's SEND_JOB_INFO, answered out of the state the machine is
	// in: ATTENTION renders PAUSED.
	printer.text(t, `{"event":"JOB_INFO","state":"ATTENTION",`+
		`"data":{"state":"PAUSED","path":"/usb/s07-long.gcode","job_id":233}}`)
	printer.awaitDialogTree(t, map[string]string{jobPathPath: `"/usb/s07-long.gcode"`})

	// Read before any PRINTING frame arrives, because a later frame writes the
	// right word for a reason of its own and would hide this one. The frame's
	// path has landed, and reportJobState runs before it in noteJob, so the
	// answer's own PAUSED has either been written by now or was refused.
	if held := printer.dialogValue(jobStatePath); held == `"PAUSED"` {
		t.Errorf("%s reads %s straight after a JOB_INFO answered in ATTENTION, want anything but "+
			"\"PAUSED\": the firmware renders that word for every state that is not exactly "+
			"Printing, so it is not evidence that the print was paused", jobStatePath, held)
	}

	t.Logf("observed: %s reads %q with the answer's own word refused",
		jobStatePath, printer.dialogValue(jobStatePath))

	// The dialog answered, and then the print running for a good while. The
	// machine volunteers no JOB_INFO for any of this.
	for frame := range 12 {
		printer.text(t, fmt.Sprintf(`{"state":"PRINTING","job_id":233,"progress":%d}`, frame))
	}

	printer.awaitDialogTree(t, map[string]string{jobStatePath: `"PRINTING"`})

	t.Logf("observed: %s reads %s while the machine says %s",
		jobStatePath, printer.dialogValue(jobStatePath),
		printer.dialogValue(telemetryPaths["state"]))
}

// TestAPauseAtTheMachineReachesTheJobState is the other direction of the same
// rule. A print paused at the printer's own screen is reported at /state and
// nowhere else, no JOB_INFO follows a pause, so the machine's word is what
// makes /job/state say the print is paused at all.
func TestAPauseAtTheMachineReachesTheJobState(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"event":"JOB_INFO","state":"PRINTING",`+
		`"data":{"state":"PRINTING","path":"/usb/s07-long.gcode","job_id":236}}`)
	printer.awaitDialogTree(t, map[string]string{jobStatePath: `"PRINTING"`})

	printer.text(t, `{"state":"PAUSED","job_id":236}`)
	printer.awaitDialogTree(t, map[string]string{jobStatePath: `"PAUSED"`})

	// And a dialog over the paused print says nothing about the print: the
	// firmware would render PAUSED for it, which is what it already holds.
	printer.text(t, `{"state":"ATTENTION","job_id":236}`)
	printer.text(t, `{"state":"ATTENTION","job_id":236,"temp_nozzle":215}`)
	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "215"})

	if held := printer.dialogValue(jobStatePath); held != `"PAUSED"` {
		t.Errorf("%s reads %s with a dialog over a paused print, want \"PAUSED\"", jobStatePath, held)
	}
}

// TestALongMoveOverAPrintDoesNotPauseTheJobState is the seam between BUG-012
// and BUG-013.
//
// BUSY is what a machine reports while a long move runs, held for 80 to 100
// seconds by an MK4 homing and levelling its bed (BUG-012). It is not one of
// the words a print is held in, so a print moving PRINTING -> BUSY crosses
// machineIsHoldingAPrint's edge and the pump asks what the print is. The answer
// is rendered out of the state the machine is in, and the firmware writes
// PAUSED for every device state that is not exactly Printing, Busy with it
// (Prusa-Firmware-Buddy, src/connect/render.cpp, the EventType::JobInfo arm),
// so believing it would report a long move as a pause: BUG-013's symptom in
// BUG-012's state.
//
// Whether Buddy reports BUSY over a running print is unmeasured, which is why
// the frame is put on the wire here rather than through the simulator: the wire
// can carry it, and the rule is what this holds.
// A file with a long name on a FAT stick is printed by its short name, and the
// long one arrives as display_name. The job names the file by its long name,
// so a person sees the name they gave it rather than the short one.
func TestAJobNamesTheFileByItsLongName(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"event":"JOB_INFO","state":"PRINTING",`+
		`"data":{"state":"PRINTING","path":"/usb/1A_PET~7.GCO","job_id":12,`+
		`"display_name":"1A_PETG_0.4n_0.2mm_XL_5h.bgcode"}}`)
	printer.awaitDialogTree(t, map[string]string{jobDisplayPath: `"1A_PETG_0.4n_0.2mm_XL_5h.bgcode"`})
}

func TestALongMoveOverAPrintDoesNotPauseTheJobState(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"event":"JOB_INFO","state":"PRINTING",`+
		`"data":{"state":"PRINTING","path":"/usb/s07-long.gcode","job_id":240}}`)
	printer.awaitDialogTree(t, map[string]string{jobStatePath: `"PRINTING"`})

	// The long move, and the refresh it asks for answered while it runs.
	printer.text(t, `{"state":"BUSY","job_id":240}`)
	printer.text(t, `{"event":"JOB_INFO","state":"BUSY",`+
		`"data":{"state":"PAUSED","path":"/usb/s07-long.gcode","job_id":240,"display":"the busy answer"}}`)
	printer.awaitDialogTree(t, map[string]string{jobDisplayPath: `"the busy answer"`})

	if held := printer.dialogValue(jobStatePath); held != `"PRINTING"` {
		t.Errorf("%s reads %s after a JOB_INFO answered while the machine was BUSY, want "+
			"\"PRINTING\": a long move says nothing about the print", jobStatePath, held)
	}

	// And the move ending puts the machine's own word back over the print.
	printer.text(t, `{"state":"PRINTING","job_id":240,"progress":12}`)
	printer.awaitDialogTree(t, map[string]string{"/job/progress": "12"})

	if held := printer.dialogValue(jobStatePath); held != `"PRINTING"` {
		t.Errorf("%s reads %s once the long move ended, want \"PRINTING\"", jobStatePath, held)
	}

	t.Logf("observed: %s held %s across a BUSY stretch and the JOB_INFO answered in it",
		jobStatePath, printer.dialogValue(jobStatePath))
}

// TestASecondPrintInOneSessionLeavesTheFirstPrintsWordBehind is BUG-004's
// second print, and the job number 0 that stands between the two.
//
// The terminal word is sticky on purpose and sticky for one print: only a new
// job number retires it. The number the firmware reports between prints is 0,
// the field is a uint16 and 0 is its way of saying there is no job, so a rule
// that retired the word on any number that is not jobIDUnknown gave the first
// idle frame after a print the power to clear it, and the refresh that same
// frame asks for then wrote IDLE over how the print ended.
func TestASecondPrintInOneSessionLeavesTheFirstPrintsWordBehind(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"event":"JOB_INFO","state":"PRINTING",`+
		`"data":{"state":"PRINTING","path":"/usb/first.gcode","job_id":241}}`)
	printer.awaitDialogTree(t, map[string]string{jobStatePath: `"PRINTING"`})

	printer.text(t, `{"state":"STOPPED","job_id":241}`)
	printer.awaitDialogTree(t, map[string]string{jobStatePath: `"STOPPED"`})

	// The machine idle between the two prints, naming no job. Several frames,
	// because it is a level and not an edge.
	for nozzle := range 3 {
		printer.text(t, fmt.Sprintf(`{"state":"IDLE","job_id":0,"temp_nozzle":%d}`, 200+nozzle))
	}

	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "202"})

	if held := printer.dialogValue(jobStatePath); held != `"STOPPED"` {
		t.Errorf("%s reads %s after frames naming job 0, want \"STOPPED\": 0 is the firmware "+
			"saying there is no job and not a print of its own", jobStatePath, held)
	}

	// And the answer to the refresh those frames asked for, which on a machine
	// with no job left says IDLE.
	printer.text(t, `{"event":"JOB_INFO","state":"IDLE",`+
		`"data":{"state":"IDLE","path":"/usb/first.gcode","job_id":0,"display":"the idle answer"}}`)
	printer.awaitDialogTree(t, map[string]string{jobDisplayPath: `"the idle answer"`})

	if held := printer.dialogValue(jobStatePath); held != `"STOPPED"` {
		t.Errorf("%s reads %s after a JOB_INFO for a machine with no job, want \"STOPPED\"",
			jobStatePath, held)
	}

	// The second print of the session, with a number of its own.
	printer.text(t, `{"event":"JOB_INFO","state":"PRINTING",`+
		`"data":{"state":"PRINTING","path":"/usb/second.gcode","job_id":242}}`)
	printer.text(t, `{"state":"PRINTING","job_id":242}`)
	printer.awaitDialogTree(t, map[string]string{jobStatePath: `"PRINTING"`})

	t.Logf("observed: %s reads %s for the second print of the session, and held %q through the "+
		"idle frames between the two", jobStatePath, printer.dialogValue(jobStatePath), "STOPPED")
}
