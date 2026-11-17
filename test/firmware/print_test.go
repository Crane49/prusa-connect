package firmware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Where a print is addressed in the state tree
// (internal/connector/xbuddy/inbound.go). They are spelled out rather than
// imported for the reason nozzlePath is: this reads the tree the way a client
// outside the process does, and a constant shared with the publisher would hide
// a rename from exactly the readers it breaks.
const (
	statePath         = "/state"
	jobStatePath      = "/job/state"
	jobProgressPath   = "/job/progress"
	jobPrintingPath   = "/job/time_printing"
	dialogIDPath      = "/dialog/id"
	dialogTitlePath   = "/dialog/title"
	dialogTextPath    = "/dialog/text"
	dialogButtonsPath = "/dialog/buttons"
)

// The verbs a print is driven by.
const (
	startPrintVerb   = "core.command.start_print"
	stopPrintVerb    = "core.command.stop_print"
	dialogActionVerb = "core.command.dialog_action"
)

// The machine states this lane reads by name.
const (
	statePrinting  = "PRINTING"
	statePaused    = "PAUSED"
	stateAttention = "ATTENTION"
	stateStopped   = "STOPPED"
	stateFinished  = "FINISHED"
	stateIdle      = "IDLE"
	stateReady     = "READY"
)

// The phase names for the waits this file adds.
const (
	phaseTransferred = "file-lands-on-the-stick"
	phasePrintStart  = "start-print-to-printing"
	phaseJobFollows  = "job-state-follows-the-machine"
	phasePrintEnds   = "stop-print-to-stopped"
)

// answeredButtons are the answers to a dialog that mean "carry on", in the
// order this lane prefers them. PRINT is the print-preview's own answer; the
// rest are what a board raising a different dialog over a print offers, and
// pressing one of those is still the operator saying yes to the print.
//
//nolint:gochecknoglobals // a fixed preference list, read-only after init.
var answeredButtons = []string{"PRINT", "YES", "CONTINUE", "OK", "RETRY"}

// travelPrintSource is the file the print scenario relays, in the storage it is
// read from and under the name it lands on the printer.
const (
	travelPrintSource = "s08-travel.gcode"
	travelPrintPath   = printerStorage + "/" + travelPrintSource
)

// TestAPrintOnRealFirmwareKeepsTheJobStateWithTheMachine is BUG-013 measured
// against firmware rather than against this project's reading of it.
//
// The bug was a `/job/state` that stood at the pre-print `PAUSED` the firmware
// renders for every device state that is not exactly Printing, for the whole
// visible middle of a print. Nothing in this repository could catch it: the
// simulator rendered JOB_INFO the way the driver expected rather than the way
// the firmware does, which is why the fix had to change both. So the assertion
// that matters is made here, on a machine whose JOB_INFO is the firmware's own.
//
// The print is travel-only on purpose. It heats nothing, extrudes nothing and
// needs no filament, so what it exercises is the state machine and the motion
// the emulator does model, and it cannot be confused with a print that failed
// for a reason of the machine's.
func TestAPrintOnRealFirmwareKeepsTheJobStateWithTheMachine(t *testing.T) {
	requireNotBlocked(t)

	if err := aPrintKeepsTheJobStateWithTheMachine(t); err != nil {
		t.Fatalf("%v", err)
	}
}

// aPrintKeepsTheJobStateWithTheMachine reports whether a print on real firmware
// keeps its job state with the machine.
//
// It is written as a probe rather than as a test body because everything after
// the transfer depends on the transfer: a file that does not reach the stick
// cannot be printed, and a scenario that cannot get a print started has nothing
// to say about the print's state. See blocked.json.
func aPrintKeepsTheJobStateWithTheMachine(t *testing.T) error {
	t.Helper()

	rg := startRigWithStick(t)

	view, err := awaitConnected(t, rg)
	if err != nil {
		return err
	}

	// The transfer is the first half of the scenario, and it is BUG-014's
	// readback against a real FatFs: a file with a name of its own must be
	// confirmed on the stick and answered done.
	body := travelPrint()

	if err = os.WriteFile(filepath.Join(rg.server.storage, travelPrintSource), body, 0o600); err != nil {
		return fmt.Errorf("put the print file in the storage: %w", err)
	}

	sending, cancel := context.WithTimeout(t.Context(), transferBudget)
	defer cancel()

	status, sent, fault, err := rg.server.sendFile(sending, view.ID, travelPrintSource, travelPrintPath)
	if err != nil {
		return fmt.Errorf("send %s: %w", travelPrintPath, err)
	}

	if status != http.StatusOK {
		return fmt.Errorf("phase %q: sending %s answered %d, %s %v", phaseTransferred,
			travelPrintPath, status, fault.Code, fault.Params)
	}

	if sent.State != "done" {
		return fmt.Errorf("phase %q: the transfer settled %q: %s", phaseTransferred,
			sent.State, sent.Reason)
	}

	// A warning is recorded rather than failed. It says the driver's five-second
	// readback could not be answered by a machine running at instruction-level
	// speed, which is a fact about the emulator; what the file's landing rests
	// on here is the print starting.
	if sent.Warning != "" {
		t.Logf("note: the transfer carries a warning, so the readback could not conclude inside its "+
			"own budget: %q", sent.Warning)
	}

	if sent.BytesSent != int64(len(body)) || sent.BytesTotal != int64(len(body)) {
		return fmt.Errorf("the transfer reports %d of %d bytes and the file is %d",
			sent.BytesSent, sent.BytesTotal, len(body))
	}

	// The listing is read for the record rather than as this scenario's proof
	// that the file landed. The proof is the print below: firmware that starts a
	// print of the path is firmware that has the file, and it is a stronger
	// reading than a directory listing of the same board.
	listed, err := rg.server.deviceFiles(t.Context(), view.ID)
	if err != nil {
		t.Logf("note: the machine would not say what is on its stick (%v), so the landing rests on "+
			"the print starting", err)
	} else {
		t.Logf("observed: %s settled %q, and %s holds %s", travelPrintPath, sent.State,
			printerStorage, describe(listed))

		if _, found := entryFor(listed, travelPrintPath); !found {
			return fmt.Errorf("the server answered done and %s is not in the printer's listing "+
				"of %s: %s", travelPrintPath, printerStorage, describe(listed))
		}
	}

	if err = startPrint(t, rg, view.ID); err != nil {
		return err
	}

	// From here the reading is the bug's own: the machine's word and the
	// print's word, polled together, for a whole print.
	faults := watchThePrint(t, rg, view.ID)

	return errors.Join(append(faults, stopThePrint(t, rg, view.ID))...)
}

// startPrint asks for the print and answers whatever the firmware puts on the
// screen until the machine is printing.
//
// The preview dialog is the expected one, and it is not assumed: a board that
// goes straight to PRINTING satisfies the wait with nothing pressed, and a
// board that raises something else has it logged and answered from
// answeredButtons. What must not happen is a scenario that presses PRINT at a
// dialog which was asking something else.
func startPrint(t *testing.T, rg *rig, id string) error {
	t.Helper()

	commanding, cancel := context.WithTimeout(t.Context(), commandBudget)
	defer cancel()

	result, err := rg.server.sendCommandArgs(commanding, id, startPrintVerb,
		map[string]any{"path": travelPrintPath})
	if err != nil {
		return fmt.Errorf("send %s: %w", startPrintVerb, err)
	}

	if !result.OK {
		return fmt.Errorf("the firmware refused %s of %s: %s %q", startPrintVerb, travelPrintPath,
			result.FaultCode, result.MachineReason)
	}

	answered := map[string]bool{}

	started := waitForPhase(t, printBudget, phasePrintStart, func(ctx context.Context) error {
		current, readErr := rg.server.printer(ctx, id)
		if readErr != nil {
			return readErr
		}

		machine, _ := reading(current, statePath)
		if machine == statePrinting {
			return nil
		}

		dialog, holding := reading(current, dialogIDPath)
		if !holding || answered[dialog] {
			return fmt.Errorf("the machine reads %q and no dialog is waiting to be answered", machine)
		}

		title, _ := reading(current, dialogTitlePath)
		text, _ := reading(current, dialogTextPath)
		buttons, _ := stringsReading(current, dialogButtonsPath)

		button, chosen := answerFor(buttons)
		if !chosen {
			return fmt.Errorf("the machine reads %q and holds dialog %s (%q / %q) whose buttons are %v, "+
				"none of which carries on a print", machine, dialog, title, text, buttons)
		}

		t.Logf("observed: the machine reads %q and holds dialog %s (%q / %q), buttons %v; answering %q",
			machine, dialog, title, text, buttons, button)

		answered[dialog] = true

		answer, sendErr := rg.server.sendCommandArgs(ctx, id, dialogActionVerb,
			map[string]any{"dialog_id": dialog, "button": button})
		if sendErr != nil {
			return fmt.Errorf("answer dialog %s with %q: %w", dialog, button, sendErr)
		}

		if !answer.OK {
			return fmt.Errorf("the firmware refused %s %q on dialog %s: %s %q", dialogActionVerb,
				button, dialog, answer.FaultCode, answer.MachineReason)
		}

		return fmt.Errorf("dialog %s answered with %q, waiting for the machine to print", dialog, button)
	})

	if started != nil {
		return started
	}

	if len(answered) == 0 {
		t.Logf("observed: the emulated %s went from %s straight to %s with no dialog raised, so the "+
			"preview this scenario is written around is not on the emulator's path", rg.pin.Model,
			startPrintVerb, statePrinting)
	}

	return nil
}

// watchThePrint is BUG-013's assertion: while the machine says it is printing,
// the print says so too, it never says paused, and the print is moving.
//
// The tolerance is two polls, which is the interval the fix's own note claims:
// the job state follows the machine's within the coalesced refresh pump's
// interval. A reading that arrives on the third poll is the bug, narrowed.
func watchThePrint(t *testing.T, rg *rig, id string) []error {
	t.Helper()

	const watchFor = 60 * time.Second

	deadline := time.Now().Add(watchFor)

	var (
		printingPolls int
		followedAfter = -1
		firstMotion   float64
		motion        float64
		sawMotion     bool
		paused        []string
	)

	for time.Now().Before(deadline) {
		current, err := rg.server.printer(t.Context(), id)
		if err != nil {
			return []error{fmt.Errorf("read %s while it printed: %w", id, err)}
		}

		machine, _ := reading(current, statePath)
		job, _ := reading(current, jobStatePath)

		progress := numericReading(current, jobProgressPath)
		printing := numericReading(current, jobPrintingPath)
		moved := progress + printing

		if machine == statePrinting {
			printingPolls++

			if !sawMotion {
				firstMotion, sawMotion = moved, true
			}

			motion = moved

			switch {
			case job == statePaused:
				paused = append(paused, fmt.Sprintf("poll %d", printingPolls))
			case job == statePrinting && followedAfter < 0:
				followedAfter = printingPolls
			}
		}

		t.Logf("poll: %s reads %s=%q %s=%q progress=%.0f time_printing=%.0f",
			id, statePath, machine, jobStatePath, job, progress, printing)

		if followedAfter > 0 && motion > firstMotion && len(paused) == 0 && printingPolls >= 3 {
			break
		}

		time.Sleep(pollInterval)
	}

	if printingPolls == 0 {
		return []error{fmt.Errorf("phase %q: the machine never read %q in %s of polling, so nothing "+
			"about the print was observed", phaseJobFollows, statePrinting, watchFor)}
	}

	var faults []error

	if len(paused) > 0 {
		faults = append(
			faults,
			fmt.Errorf("phase %q: %s read %q while the machine read %q, at %s. This is BUG-013: the "+
				"firmware renders a JOB_INFO's state as PAUSED for every device state that is not exactly "+
				"Printing, and the machine's own word is what must decide.",
				phaseJobFollows, jobStatePath, statePaused, statePrinting, strings.Join(paused, ", ")),
		)
	}

	switch {
	case followedAfter < 0:
		faults = append(faults, fmt.Errorf("phase %q: the machine read %q for %d polls and %s never did",
			phaseJobFollows, statePrinting, printingPolls, jobStatePath))
	case followedAfter > 2:
		faults = append(faults, fmt.Errorf("phase %q: %s read %q only on the %d-th poll of a printing "+
			"machine, and the fix claims one refresh interval",
			phaseJobFollows, jobStatePath, statePrinting, followedAfter))
	}

	if motion <= firstMotion {
		faults = append(faults, fmt.Errorf("phase %q: the print did not move over %d printing polls: "+
			"progress and time_printing together read %.0f and started at %.0f",
			phaseJobFollows, printingPolls, motion, firstMotion))
	}

	t.Logf("observed: %s followed the machine to %q on printing poll %d, never read %q, and the print "+
		"moved from %.0f to %.0f over %d polls",
		jobStatePath, statePrinting, followedAfter, statePaused, firstMotion, motion, printingPolls)

	return faults
}

// stopThePrint is the other half of BUG-013: the way a print ended is what
// /job/state says last, and the machine returning to itself does not rewrite
// it.
func stopThePrint(t *testing.T, rg *rig, id string) error {
	t.Helper()

	commanding, cancel := context.WithTimeout(t.Context(), commandBudget)
	defer cancel()

	result, err := rg.server.sendCommandArgs(commanding, id, stopPrintVerb, nil)
	if err != nil {
		return fmt.Errorf("send %s: %w", stopPrintVerb, err)
	}

	if !result.OK {
		return fmt.Errorf("the firmware refused %s: %s %q", stopPrintVerb, result.FaultCode,
			result.MachineReason)
	}

	stopped := waitForPhase(t, printBudget, phasePrintEnds, func(ctx context.Context) error {
		current, readErr := rg.server.printer(ctx, id)
		if readErr != nil {
			return readErr
		}

		machine, _ := reading(current, statePath)
		job, _ := reading(current, jobStatePath)

		if job != stateStopped {
			return fmt.Errorf("%s reads %q and the machine reads %q", jobStatePath, job, machine)
		}

		if machine == statePrinting || machine == stateAttention {
			return fmt.Errorf("%s reads %q and the machine is still %q", jobStatePath, job, machine)
		}

		return nil
	})
	if stopped != nil {
		return stopped
	}

	// And it stays. A machine that has gone back to IDLE keeps reporting
	// telemetry, and BUG-004's rule is that the terminal word is retired only by
	// a new job number.
	settled := []string{stateIdle, stateReady, stateFinished, stateStopped}

	var faults []error

	for range 5 {
		time.Sleep(pollInterval)

		current, readErr := rg.server.printer(t.Context(), id)
		if readErr != nil {
			return fmt.Errorf("read %s after the stop: %w", id, readErr)
		}

		machine, _ := reading(current, statePath)

		job, present := reading(current, jobStatePath)
		if !present {
			faults = append(faults, fmt.Errorf("%s was cleared after the stop, so nothing says how "+
				"the print ended", jobStatePath))

			continue
		}

		if job != stateStopped {
			faults = append(faults, fmt.Errorf("%s read %q after the stop had settled it to %q, with "+
				"the machine at %q", jobStatePath, job, stateStopped, machine))
		}

		if !slices.Contains(settled, machine) {
			t.Logf("note: the machine reads %q after the stop", machine)
		}
	}

	t.Logf("observed: %s settled %q and stayed there while the machine went back to itself",
		jobStatePath, stateStopped)

	return errors.Join(faults...)
}

// answerFor is the button to press at a dialog, or nothing when none of the
// buttons carries a print on.
func answerFor(buttons []string) (string, bool) {
	for _, wanted := range answeredButtons {
		for _, offered := range buttons {
			if strings.EqualFold(offered, wanted) {
				return offered, true
			}
		}
	}

	return "", false
}

// entryFor is one path in a printer's listing.
func entryFor(listing []deviceFile, path string) (deviceFile, bool) {
	for _, entry := range listing {
		if entry.Path == path {
			return entry, true
		}
	}

	return deviceFile{}, false
}

// describe renders a printer's listing for a failure message, because a
// listing that does not hold what was sent is the whole evidence.
func describe(listing []deviceFile) string {
	if len(listing) == 0 {
		return "nothing"
	}

	out := make([]string, 0, len(listing))

	for _, entry := range listing {
		out = append(out, fmt.Sprintf("%s (name %q, display %q, %d bytes, folder=%t, read_only=%t)",
			entry.Path, entry.Name, entry.DisplayName, entry.Size, entry.Folder, entry.ReadOnly))
	}

	return strings.Join(out, "; ")
}

// travelPrint is the print this lane starts: travel moves and progress lines,
// no heating and no extrusion.
//
// It is generated rather than checked in because what matters about it is
// arithmetic, long enough to have a visible middle, and short enough that a
// scenario's budget covers it, and a checked-in file hides that arithmetic
// behind a blob. The moves are the diagonal of the MK4's bed at 50 mm/s, which
// is about three and a half seconds each, so thirty of them is under two
// minutes of motion.
func travelPrint() []byte {
	const (
		moves = 30
		// The two corners the nozzle shuttles between, inside the bed on every
		// side so no move is clipped by a soft limit.
		nearX, nearY = 20.0, 20.0
		farX, farY   = 200.0, 160.0
		// feedrate is mm/min, so 3000 is 50 mm/s.
		feedrate = 3000
	)

	lines := []string{
		"; travel-only print, generated by test/firmware/print_test.go",
		"; it heats nothing and extrudes nothing: what it exercises is the state",
		"; machine and the motion the emulator models.",
		"M73 P0 R2",
		"G21",
		"G90",
		"M83",
		"G28 XY",
	}

	for move := range moves {
		x, y := nearX, nearY
		if move%2 == 0 {
			x, y = farX, farY
		}

		lines = append(lines,
			fmt.Sprintf("M73 P%d R%d", move*100/moves, (moves-move)*4/moves),
			fmt.Sprintf("G1 X%.1f Y%.1f F%d", x, y, feedrate),
		)
	}

	lines = append(lines, "M73 P100 R0", "M84", "")

	return []byte(strings.Join(lines, "\n"))
}
