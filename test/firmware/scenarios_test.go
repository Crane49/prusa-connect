package firmware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/tools/mini404"
)

// nozzlePath is where the xBuddy driver files the hotend reading in the state
// tree (internal/connector/xbuddy/inbound.go:97). It is spelled out rather than
// imported, because this reads the tree the way a client outside the process
// does and a constant shared with the publisher would hide a rename from
// exactly the readers it breaks.
const nozzlePath = "/tools/0/nozzle_temp"

// nozzleThermistor is the script console context of the hotend thermistor.
//
// MINI404 names a script context after the device's QOM type
// (MINI404 hw/arm/prusa/parts/thermistor.c:243) and disambiguates duplicates by
// appending an index in registration order
// (MINI404 hw/arm/prusa/utility/ScriptHost.cpp:604). The mk4 board file creates
// one per member of its own temperature enum in order
// (MINI404 hw/arm/prusa/prusa-mk4.c:720), and the first member is T_NOZ
// (MINI404 hw/arm/prusa/prusa-mk4.c:91), so the hotend is the unsuffixed
// context and the heated bed is thermistor1.
const nozzleThermistor = "thermistor"

// The stimulus and what counts as the reading following it.
const (
	// stimulusCelsius is what the hotend thermistor is pinned to. It is above
	// the machine's 18 C ambient so the change is unambiguous, and far enough
	// below a printing temperature that no heater is being asked to explain it.
	stimulusCelsius = 55.0
	// toleranceCelsius is how far the reported reading may sit from the value
	// the thermistor was pinned to. The value travels through a resistance
	// table, a 12 bit ADC and the firmware's own oversampling, so it does not
	// arrive exact.
	toleranceCelsius = 3.0
)

// The probe names, which are the keys blocked.json uses.
const (
	probeIdentity     = "identity-matches-the-pin-row"
	probeRegistration = "registration-code-flow-admits-a-tokenless-printer"
	probeDrain        = "queued-command-drains-on-reboot"
	probeCollision    = "short-name-collision-is-not-reported-done"
	probePrint        = "a-print-keeps-the-job-state-with-the-machine"
)

// probes decide whether a blocked scenario's wiring has arrived. A probe
// returns nil when the scenario it stands for would now pass, and each one is
// the scenario's own assertion rather than a paraphrase of it.
//
//nolint:gochecknoglobals // a fixture, read-only after init.
var probes = map[string]func(*testing.T) error{
	probeIdentity:     identityMatchesThePinRow,
	probeRegistration: codeFlowAdmitsATokenlessPrinter,
	probeDrain:        queuedCommandDrainsOnReboot,
	probeCollision:    shortNameCollisionIsNotReportedDone,
	probePrint:        aPrintKeepsTheJobStateWithTheMachine,
}

// TestFirmwareRegistersAndConnects is the scenario every other one rests on:
// real firmware brings its own network stack up, dials the port it was seeded
// with, is admitted, and is reported as a connected printer by the server's own
// API.
//
// It catches an EEPROM seed the firmware no longer reads, a registration wire
// that has moved, and an admission mode that refuses a printer it once
// accepted. Nothing else in this repository can catch any of the three: every
// other lane's printer is this project's own reading of the protocol.
func TestFirmwareRegistersAndConnects(t *testing.T) {
	rg := startRig(t)

	view, err := awaitConnected(t, rg)
	if err != nil {
		t.Fatalf("%v\n\nEEPROM journal drift is the first suspect: the firmware reads the seeded host, "+
			"port and token out of a journal whose format tools/mini404/eeprom.go reproduces "+
			"by hand, and a firmware bump that moves it produces a printer that boots and never dials. "+
			"TestBanksMatchTheVendorGenerator is the alarm for that.", err)
	}

	if view.Vendor != "Prusa" {
		t.Errorf("the printer reports vendor %q, and everything this dialect serves is a Prusa", view.Vendor)
	}

	if view.Driver == "" {
		t.Error("the printer arrived through no named connector instance")
	}

	if view.Kind != printerKind {
		t.Errorf("the machine is listed as a %q and this lane booted a printer", view.Kind)
	}

	// The serial and the unit uid are the two identities the device surface
	// carries: the number on the machine, and the one the connector keys a
	// reconnection by. Firmware that dials with neither produces a device the
	// fleet cannot name, and the reboot scenario has nothing to match on.
	if view.Serial == "" {
		t.Error("the printer reported no serial, so nothing on this surface names the machine")
	}

	if view.UnitUID == "" {
		t.Error("the printer carries no unit uid, so a reconnection cannot be matched to it")
	}

	if len(view.Capabilities) == 0 {
		t.Error("the printer arrived with no capabilities, so no command may be sent to it")
	}

	// The identity strings are logged rather than asserted here. What the
	// firmware puts in them is what TestFirmwareIdentityMatchesThePinRow is for,
	// and it is blocked until a run like this one has recorded them.
	t.Logf("observed: %s connected as id %s, vendor %q, model %q, firmware %q, serial %q, "+
		"unit uid %q, through %q, with %d capabilities",
		rg.pin.Model, view.ID, view.Vendor, view.Model, view.Firmware, view.Serial, view.UnitUID,
		view.Driver, len(view.Capabilities))
}

// TestFirmwareIdentityMatchesThePinRow requires the printer the server reports
// to be the printer the pin table says was booted.
//
// It catches a lane that has been passing against the wrong artefact: a stale
// firmware image in the cache, or a pin row edited without rebuilding.
func TestFirmwareIdentityMatchesThePinRow(t *testing.T) {
	requireNotBlocked(t)

	if err := identityMatchesThePinRow(t); err != nil {
		t.Fatalf("%v", err)
	}
}

// TestFirmwareRegistersOverTheCodeFlow drives the handshake a printer with no
// token takes: POST /p/register, poll GET /p/register for the token an approval
// issues, then dial the websocket with it.
//
// It is the other half of admission. The seeded-token path skips /p/register
// entirely, so nothing else in this lane exercises the code flow against real
// firmware.
func TestFirmwareRegistersOverTheCodeFlow(t *testing.T) {
	requireNotBlocked(t)

	if err := codeFlowAdmitsATokenlessPrinter(t); err != nil {
		t.Fatalf("%v", err)
	}
}

// TestTelemetryTracksTheFirmware drives a sensor inside the emulated machine
// and requires the server's API to report what the firmware made of it.
//
// It is the end-to-end reading of the telemetry path: a thermistor in the
// guest, the firmware's own sampling and framing, the driver's decoding, the
// state tree, and the HTTP surface. A number that arrives here is a number
// every layer between agreed on.
func TestTelemetryTracksTheFirmware(t *testing.T) {
	rg := startRig(t)

	view, err := awaitConnected(t, rg)
	if err != nil {
		t.Fatalf("%v", err)
	}

	awaitPhase(t, telemetryBudget, phaseSampled, func(ctx context.Context) error {
		current, readErr := rg.server.printer(ctx, view.ID)
		if readErr != nil {
			return readErr
		}

		if _, ok := nozzleReading(current); !ok {
			return fmt.Errorf("%s is not in the state tree yet, which holds %v",
				nozzlePath, paths(current))
		}

		return nil
	})

	if err = setNozzleThermistor(t.Context(), rg, stimulusCelsius); err != nil {
		t.Fatalf("phase %q: %v", phaseTelemetry, err)
	}

	awaitPhase(t, telemetryBudget, phaseTelemetry, func(ctx context.Context) error {
		current, readErr := rg.server.printer(ctx, view.ID)
		if readErr != nil {
			return readErr
		}

		reading, ok := nozzleReading(current)
		if !ok {
			return fmt.Errorf("%s stopped being reported", nozzlePath)
		}

		if math.Abs(reading-stimulusCelsius) > toleranceCelsius {
			return fmt.Errorf("%s reads %.1f C and the thermistor is pinned at %.1f C, tolerance %.1f",
				nozzlePath, reading, stimulusCelsius, toleranceCelsius)
		}

		return nil
	})

	t.Logf("observed: %s followed the thermistor to %.1f C within %.1f C", nozzlePath,
		stimulusCelsius, toleranceCelsius)
}

// beepVerb is the capability the command scenarios invoke. A beep is the one
// verb that is safe on any machine in any state: it moves nothing, heats
// nothing and changes no setting, and idle firmware accepts it.
const beepVerb = "core.command.beep"

// TestDisconnectFollowsAGuestThatDies requires the server to notice a printer
// whose machine went away: the QEMU process is stopped mid-session, and the
// printer must be reported as not connected while staying listed.
//
// It is the reachability half of the session model. The session outlives the
// absence by design, so a printer that vanishes from the listing when its
// socket dies is a session that died with its connection, and a printer that
// stays "connected" is a server that never noticed.
func TestDisconnectFollowsAGuestThatDies(t *testing.T) {
	rg := startRig(t)

	view, err := awaitConnected(t, rg)
	if err != nil {
		t.Fatalf("%v", err)
	}

	stopGuest(t, rg)

	if err = awaitDisconnected(t, rg, view.ID); err != nil {
		t.Fatalf("%v", err)
	}

	t.Logf("observed: %s was reported connected, its machine died, and the server reported it "+
		"disconnected and still listed", view.ID)
}

// TestQueuedCommandDrainsOnReboot requires the queue-and-drain rule to hold
// through the API: a command sent while the machine is off waits, the machine
// is power-cycled from the same EEPROM and external flash, and the command is
// carried out on the connection the reboot brings.
//
// It is blocked on the transport, not on the device plane. The first live run
// proved the half nothing else can prove: the rebooted machine reattaches
// under the same printer id, so the waiting session is the one it comes back
// to. What the run also proved is that the wait itself cannot survive: the
// command endpoint is synchronous and the server's write timeout cuts the
// connection at sixty seconds, minutes before a rebooted MK4 is back.
func TestQueuedCommandDrainsOnReboot(t *testing.T) {
	requireNotBlocked(t)

	if err := queuedCommandDrainsOnReboot(t); err != nil {
		t.Fatalf("%v", err)
	}
}

// queuedCommandDrainsOnReboot reports whether a command sent while the machine
// is off is carried out on the connection its reboot brings.
func queuedCommandDrainsOnReboot(t *testing.T) error {
	t.Helper()

	rg := startRig(t)

	view, err := awaitConnected(t, rg)
	if err != nil {
		return err
	}

	stopGuest(t, rg)

	if err = awaitDisconnected(t, rg, view.ID); err != nil {
		return err
	}

	// The command is sent while the printer is away, so the call is a wait:
	// it returns when the printer does. It runs in its own goroutine because
	// the reboot it waits for is started below, and the answer is read on the
	// test's goroutine.
	queued := make(chan struct {
		result commandResult
		err    error
	}, 1)

	sending, cancel := context.WithTimeout(t.Context(), drainBudget)
	defer cancel()

	go func() {
		result, sendErr := rg.server.sendCommand(sending, view.ID, beepVerb)
		queued <- struct {
			result commandResult
			err    error
		}{result, sendErr}
	}()

	rebootGuest(t, rg)

	after, err := awaitConnected(t, rg)
	if err != nil {
		return fmt.Errorf("after the reboot: %w", err)
	}

	if after.ID != view.ID {
		return fmt.Errorf("the rebooted machine came back as printer %q and it went down as %q, so the "+
			"new connection was not put under the waiting session and the queued command can never drain",
			after.ID, view.ID)
	}

	answer := <-queued
	if answer.err != nil {
		return fmt.Errorf("the queued %s did not drain: %w", beepVerb, answer.err)
	}

	if !answer.result.OK {
		return fmt.Errorf("the queued %s drained and the printer refused it: %q",
			beepVerb, answer.result.MachineReason)
	}

	t.Logf("observed: %s queued while %s was down, survived a reboot from the same EEPROM, and the "+
		"firmware accepted it", beepVerb, view.ID)

	return nil
}

// TestCommandRoundTripIsRecorded requires a command to reach real firmware, be
// accepted, and leave a row in the developer command log.
//
// The round trip is the outbound half of the wire this lane exists for: the
// telemetry scenario proves the server hears the firmware, and this proves the
// firmware hears the server. The log read on top is what makes the developer
// view trustworthy at a bench, because a log that misses commands is worse
// than none.
func TestCommandRoundTripIsRecorded(t *testing.T) {
	rg := startRig(t)

	view, err := awaitConnected(t, rg)
	if err != nil {
		t.Fatalf("%v", err)
	}

	commanding, cancel := context.WithTimeout(t.Context(), commandBudget)
	defer cancel()

	result, err := rg.server.sendCommand(commanding, view.ID, beepVerb)
	if err != nil {
		t.Fatalf("send %s: %v", beepVerb, err)
	}

	if !result.OK {
		t.Fatalf("the printer refused %s: %q", beepVerb, result.MachineReason)
	}

	recorded, err := rg.server.recordedCommands(t.Context(), view.ID)
	if err != nil {
		t.Fatalf("%v", err)
	}

	row, found := findRecorded(recorded, beepVerb)
	if !found {
		t.Fatalf("the command log holds no %s row for %s, and the command was just accepted", beepVerb, view.ID)
	}

	if !row.OK {
		t.Errorf("the log records %s as refused and the caller was told it was accepted", beepVerb)
	}

	if row.ElapsedMS < 0 {
		t.Errorf("the log records a negative elapsed time for %s", beepVerb)
	}

	t.Logf("observed: %s accepted by the firmware in %d ms, and the developer log holds the row",
		beepVerb, row.ElapsedMS)
}

// awaitDisconnected polls the fleet listing until the printer reports not
// connected. A printer that leaves the listing never satisfies the wait: the
// session is supposed to outlive the absence, so a vanished printer is a
// session that died with its socket, and the timeout carries that verdict.
//
// It returns an error rather than failing the test, for the same reason
// awaitConnected does.
func awaitDisconnected(t *testing.T, rg *rig, id string) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), disconnectBudget)
	defer cancel()

	return mini404.WaitFunc(ctx, phaseGuestDown, func(ctx context.Context) error {
		listed, err := rg.server.printers(ctx)
		if err != nil {
			return err
		}

		for _, item := range listed {
			if item.ID != id {
				continue
			}

			if item.Connected {
				return fmt.Errorf("%s is still reported connected", id)
			}

			return nil
		}

		return fmt.Errorf("%s left the listing when its machine died, so the session did not survive "+
			"the absence", id)
	})
}

// findRecorded returns the first row of the developer command log holding the
// verb.
func findRecorded(rows []recordedCommand, verb string) (recordedCommand, bool) {
	for _, row := range rows {
		if row.Verb == verb {
			return row, true
		}
	}

	return recordedCommand{}, false
}

// TestNothingIsStillBlockedThatHasBeenWired fails the moment a blocked
// scenario's probe starts passing.
//
// A suite that quietly skips is how a lane goes green for a year without
// running, so the list is a claim about the world that something checks rather
// than a note somebody has to remember.
func TestNothingIsStillBlockedThatHasBeenWired(t *testing.T) {
	requireLane(t)

	blocked := blockedScenarios(t)
	if len(blocked) == 0 {
		t.Skip("blocked.json is empty, so there is nothing to unblock")
	}

	for _, entry := range blocked {
		probe, known := probes[entry.Probe]
		if !known {
			t.Errorf("blocked.json lists probe %q for %s and no such probe exists, so nothing is "+
				"watching that entry", entry.Probe, entry.Scenario)

			continue
		}

		t.Run(entry.Probe, func(t *testing.T) {
			if err := probe(t); err != nil {
				t.Logf("still blocked: %v", err)

				return
			}

			t.Errorf("the probe %q passes, so %s can be unblocked: delete its entry from blocked.json "+
				"and let the scenario run", entry.Probe, entry.Scenario)
		})
	}
}

// identityMatchesThePinRow reports whether the printer the server has is the
// one the pin table describes.
func identityMatchesThePinRow(t *testing.T) error {
	t.Helper()

	rg := startRig(t)

	view, err := awaitConnected(t, rg)
	if err != nil {
		return err
	}

	var faults []error

	if !strings.EqualFold(view.Model, rg.pin.Model) {
		faults = append(faults, fmt.Errorf("the printer reports model %q and the pin row is %q",
			view.Model, rg.pin.Model))
	}

	// The version is checked by prefix. The build metadata after it moves with
	// every firmware build and pins nothing (earlier research records this
	// generation as Buddy 6.6.0).
	if !strings.HasPrefix(view.Firmware, firmwareSeries) {
		faults = append(faults, fmt.Errorf("the printer reports firmware %q, which is not %s",
			view.Firmware, firmwareSeries))
	}

	// The pin row does not carry a serial: the firmware image mints one and the
	// artefact cache holds one image, so what is checked is that the machine the
	// fleet named is the machine that dialled rather than a second reading of it.
	if view.UnitUID != view.Serial {
		faults = append(faults, fmt.Errorf("the printer is keyed by unit uid %q and reports serial %q, "+
			"so a reconnection under the serial would arrive as a different device",
			view.UnitUID, view.Serial))
	}

	return errors.Join(faults...)
}

// firmwareSeries is the release the pinned image is built from.
const firmwareSeries = "6.6.0"

// codeFlowAdmitsATokenlessPrinter reports whether firmware with no seeded token
// registers itself and connects with nobody at the machine.
func codeFlowAdmitsATokenlessPrinter(t *testing.T) error {
	t.Helper()

	rg := startTokenlessRig(t)

	if _, err := awaitConnected(t, rg); err != nil {
		// A timeout here has two very different causes, and the registration
		// surface is what separates them: a row means the firmware posted to
		// /p/register and admission held it, and no row means the firmware never
		// dialled at all, which is what this probe is blocked on.
		waiting, listErr := rg.server.registrations(t.Context())
		if listErr != nil {
			return fmt.Errorf("%w (and the registration listing could not be read: %w)", err, listErr)
		}

		if len(waiting) == 0 {
			return fmt.Errorf("%w, and nothing is waiting on %s either, so the tokenless firmware never "+
				"posted a registration", err, registrationsPath)
		}

		return fmt.Errorf("%w, and %d registration(s) are waiting on %s: %v, so the firmware did dial and "+
			"admission held it", err, len(waiting), registrationsPath, waiting)
	}

	return nil
}

// awaitConnected polls the fleet listing until one printer is there and
// connected.
//
// It returns an error rather than failing the test, so that a probe for a
// blocked scenario can read the same wait as a verdict instead of as a failure.
func awaitConnected(t *testing.T, rg *rig) (printerView, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), registerBudget)
	defer cancel()

	var found printerView

	err := mini404.WaitFunc(ctx, phaseRegistered, func(ctx context.Context) error {
		listed, listErr := rg.server.printers(ctx)
		if listErr != nil {
			return listErr
		}

		for _, item := range listed {
			if item.Connected {
				found = item

				return nil
			}
		}

		return fmt.Errorf("%d printers are known to the server and none is connected", len(listed))
	})
	if err != nil {
		return printerView{}, fmt.Errorf("%w (the firmware was seeded with 10.0.2.2:%d)", err, rg.port)
	}

	return found, nil
}

// setNozzleThermistor pins the hotend reading, which the heater cannot then pull
// back: MINI404 holds a scripted value until Restore
// (MINI404 hw/arm/prusa/parts/thermistor.c:145). The action takes one float
// (MINI404 hw/arm/prusa/parts/thermistor.c:248).
func setNozzleThermistor(ctx context.Context, rg *rig, celsius float64) error {
	action := fmt.Sprintf("%s::Set(%g)", nozzleThermistor, celsius)

	if err := rg.printer.Console().Do(ctx, action); err != nil {
		return fmt.Errorf("drive %s: %w", action, err)
	}

	return nil
}

// nozzleReading is the hotend temperature as the state tree carries it.
func nozzleReading(view printerView) (float64, bool) {
	raw, present := view.State[nozzlePath]
	if !present {
		return 0, false
	}

	var reading float64
	if err := json.Unmarshal(raw, &reading); err != nil {
		return 0, false
	}

	return reading, true
}

// paths is what the state tree carries, for a diagnostic that says what did
// arrive when the expected path did not.
func paths(view printerView) []string {
	out := make([]string, 0, len(view.State))
	for path := range view.State {
		out = append(out, path)
	}

	return out
}

// blockedEntry is one scenario that cannot be asserted yet.
type blockedEntry struct {
	// Scenario is the test function that is skipped while this entry stands.
	Scenario string `json:"scenario"`
	// Probe is the key in probes that decides whether the entry still holds.
	Probe string `json:"probe"`
	// Reason is what is missing, and what would settle it.
	Reason string `json:"reason"`
}

// blockedScenarios reads the list.
func blockedScenarios(t *testing.T) []blockedEntry {
	t.Helper()

	content, err := os.ReadFile("blocked.json")
	if err != nil {
		t.Fatalf("read blocked.json: %v", err)
	}

	var entries []blockedEntry
	if err = json.Unmarshal(content, &entries); err != nil {
		t.Fatalf("parse blocked.json: %v", err)
	}

	return entries
}

// requireNotBlocked skips a scenario that blocked.json still lists, and names
// the reason so the skip line is the whole explanation.
func requireNotBlocked(t *testing.T) {
	t.Helper()
	requireLane(t)

	for _, entry := range blockedScenarios(t) {
		if entry.Scenario != t.Name() {
			continue
		}

		t.Skipf("blocked on %q: %s", entry.Probe, entry.Reason)
	}
}
