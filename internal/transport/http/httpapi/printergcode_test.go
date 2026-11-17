package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// gcodeGrants is a grant store that answers the action check from a fixed set,
// which is what *authz.Store does from the graph in a real process. holds is
// the actions the caller has; anything absent is refused.
type gcodeGrants struct {
	fakeGrantStore

	holds []authz.Action
	asked []authz.Action

	// verbs is the dangerous tier this fake holds, and askedVerbs is what the
	// surface asked for. Raw gcode is reached through the tier now: no class,
	// role or level confers it.
	verbs      []string
	askedVerbs []string
}

func (g *gcodeGrants) CanVerb(
	_ context.Context, _ authz.Subject, _ authz.Object, verb string,
) (bool, error) {
	g.askedVerbs = append(g.askedVerbs, verb)

	return slices.Contains(g.verbs, verb), nil
}

var _ GrantStore = (*gcodeGrants)(nil)

func (g *gcodeGrants) Can(
	_ context.Context, _ authz.Subject, action authz.Action, _ authz.Object,
) (bool, error) {
	g.asked = append(g.asked, action)

	return slices.Contains(g.holds, action), nil
}

// gcodeFleetStub answers a fixed verdict per line, so a test says which step
// fails rather than arranging a printer that fails there.
type gcodeFleetStub struct {
	DeviceFleet

	printer  Device
	refuseAt map[string]error
	notOKAt  map[string]bool
	reasonAt map[string]string
	received []PrinterCommand
}

func (f *gcodeFleetStub) Get(_ context.Context, _ string, id string) (Device, error) {
	if id != f.printer.ID {
		return Device{}, notFoundPrinter(id)
	}

	return f.printer, nil
}

func (f *gcodeFleetStub) Command(
	_ context.Context, _ string, id string, cmd PrinterCommand,
) (PrinterResult, error) {
	if id != f.printer.ID {
		return PrinterResult{}, notFoundPrinter(id)
	}

	f.received = append(f.received, cmd)

	line := cmd.Args["gcode"]
	if err, refused := f.refuseAt[line]; refused {
		return PrinterResult{}, err
	}

	if f.notOKAt[line] {
		return PrinterResult{OK: false, MachineReason: f.reasonAt[line]}, nil
	}

	return PrinterResult{OK: true}, nil
}

func gcodeServer(t *testing.T, fleet DeviceFleet, grants GrantStore) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     grants,
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    fleet,
		Stores:     noStorages{},
		Events:     noEvents{},
		Mounts:     devMounts(),
	})
	require.NoError(t, err)

	return server
}

func gcodeManager() *gcodeGrants {
	return &gcodeGrants{
		holds: []authz.Action{authz.ActionOperate},
		verbs: authz.RawGCodeVerbs(),
	}
}

func commandOnly() *gcodeGrants {
	return &gcodeGrants{holds: []authz.Action{authz.ActionOperate}}
}

func gcodePath() string { return "/api/v1/devices/pdeadbeef/gcode" }

func decodeOutcome(t *testing.T, body []byte) PrinterGCodeOutcome {
	t.Helper()

	var outcome PrinterGCodeOutcome
	require.NoError(t, json.Unmarshal(body, &outcome))

	return outcome
}

// A run every line of which is accepted, and the frames it produced. This is
// the shape everything else is a deviation from.
func TestASequenceRunsEveryLineInOrder(t *testing.T) {
	t.Parallel()

	fleet := &gcodeFleetStub{printer: devPrinter()}
	server := gcodeServer(t, fleet, gcodeManager())

	rec := call(t, server.Handler(), http.MethodPost, gcodePath(),
		`{"lines":["G28","M104 S215","M140 S60"]}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	outcome := decodeOutcome(t, rec.Body.Bytes())
	require.False(t, outcome.Stopped)
	require.Equal(t, 3, outcome.Completed)
	require.Len(t, outcome.Steps, 3)

	for _, step := range outcome.Steps {
		require.Equal(t, stepAccepted, step.Status, step.GCode)
	}

	require.Len(t, fleet.received, 3)
	require.Equal(t, []string{"G28", "M104 S215", "M140 S60"}, []string{
		fleet.received[0].Args["gcode"],
		fleet.received[1].Args["gcode"],
		fleet.received[2].Args["gcode"],
	})

	for _, cmd := range fleet.received {
		require.Equal(t, "core.command.send_gcode", cmd.Verb)
	}
}

// The failure rule, which is the whole of what a sequence promises. It stops,
// it says where, and the lines after it say they were not attempted rather than
// being absent or reported as successes.
func TestASequenceStopsAtTheFirstRefusalAndNamesWhatItDidNotAttempt(t *testing.T) {
	t.Parallel()

	refusal := connectorapi.GCodeNotAllowed.New("this server never sends that gcode word").
		With(fault.StringParam("word", "M997"),
			fault.StringParam("reason", "firmware is not flashed through this API"))

	fleet := &gcodeFleetStub{
		printer:  devPrinter(),
		refuseAt: map[string]error{"M997": refusal},
	}
	server := gcodeServer(t, fleet, gcodeManager())

	rec := call(t, server.Handler(), http.MethodPost, gcodePath(),
		`{"lines":["G28","M997","M104 S215","M140 S60"]}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	outcome := decodeOutcome(t, rec.Body.Bytes())
	require.True(t, outcome.Stopped)
	require.Equal(t, 1, outcome.Completed, "the line before the failure ran and is not undone")
	require.Len(t, outcome.Steps, 4)

	require.Equal(t, stepAccepted, outcome.Steps[0].Status)
	require.Equal(t, stepRefused, outcome.Steps[1].Status)
	require.Equal(t, connectorapi.GCodeNotAllowed.Code().String(), outcome.Steps[1].Code)
	require.Contains(t, outcome.Steps[1].Detail, "firmware is not flashed through this API",
		"the refusal names no reason, so a caller is told never and not why")
	require.Equal(t, stepNotAttempted, outcome.Steps[2].Status)
	require.Equal(t, stepNotAttempted, outcome.Steps[3].Status)

	// The steps after the failure must not have reached the printer at all.
	require.Len(t, fleet.received, 2)
	require.Equal(t, "M997", fleet.received[1].Args["gcode"])
}

// A printer that answers "no" is a stop for the same reason a refusal is: the
// lines after it were written on the assumption it succeeded.
func TestAPrinterRefusalStopsTheSequenceToo(t *testing.T) {
	t.Parallel()

	fleet := &gcodeFleetStub{
		printer: devPrinter(),
		notOKAt: map[string]bool{"M109 S215": true},
	}
	server := gcodeServer(t, fleet, gcodeManager())

	rec := call(t, server.Handler(), http.MethodPost, gcodePath(),
		`{"lines":["G28","M109 S215","G1 Z10 F600"]}`)

	require.Equal(t, http.StatusOK, rec.Code)

	outcome := decodeOutcome(t, rec.Body.Bytes())
	require.True(t, outcome.Stopped)
	require.Equal(t, 1, outcome.Completed)
	require.Equal(t, stepRefused, outcome.Steps[1].Status)
	require.Equal(t, stepNotAttempted, outcome.Steps[2].Status)
	require.Len(t, fleet.received, 2)
}

// TestARefusedLineCarriesThePrintersOwnWords reads the published gcode
// operation and puts the two refusals side by side.
//
// A line the printer refused carries the machine's word and no code. A line this
// server refused carries a registered code and no machine word. An operator told
// only "the printer did not accept it" learns nothing they can act on, and a
// client that cannot tell the two apart retries the one that will never work.
func TestARefusedLineCarriesThePrintersOwnWords(t *testing.T) {
	t.Parallel()

	refusal := connectorapi.GCodeNotAllowed.New("this server never sends that gcode word").
		With(fault.StringParam("word", "M997"),
			fault.StringParam("reason", "firmware is not flashed through this API"))

	fleet := &gcodeFleetStub{
		printer:  devPrinter(),
		notOKAt:  map[string]bool{"M109 S215": true},
		reasonAt: map[string]string{"M109 S215": "TEMP_LIMIT_EXCEEDED"},
		refuseAt: map[string]error{"M997": refusal},
	}
	handler := gcodeServer(t, fleet, gcodeManager()).Handler()

	byThePrinter := call(t, handler, http.MethodPost, gcodePath(),
		`{"lines":["G28","M109 S215","G1 Z10 F600"]}`)
	t.Logf("observed: the printer's refusal answered %d %s %s",
		byThePrinter.Code, byThePrinter.Header().Get("Content-Type"), byThePrinter.Body.String())

	require.Equal(t, http.StatusOK, byThePrinter.Code, byThePrinter.Body.String())

	refused := decodeOutcome(t, byThePrinter.Body.Bytes()).Steps[1]
	require.Equal(t, stepRefused, refused.Status)
	require.Equal(t, "TEMP_LIMIT_EXCEEDED", refused.MachineReason,
		"the printer's own word for the refusal did not reach the caller")
	require.Empty(t, refused.Code,
		"a printer's refusal was published as one of this API's registered codes")

	byTheAPI := call(t, handler, http.MethodPost, gcodePath(), `{"lines":["M997"]}`)
	t.Logf("observed: this server's refusal answered %d %s %s",
		byTheAPI.Code, byTheAPI.Header().Get("Content-Type"), byTheAPI.Body.String())

	declined := decodeOutcome(t, byTheAPI.Body.Bytes()).Steps[0]
	require.Equal(t, stepRefused, declined.Status)
	require.Equal(t, connectorapi.GCodeNotAllowed.Code().String(), declined.Code)
	require.Empty(t, declined.MachineReason,
		"a line this server never sent came back with words a machine is supposed to have said")
	require.NotContains(t, byTheAPI.Body.String(), "machine_reason")
}

// TestALongRefusalReachesTheCallerAndIsClippedInTheLog is the driver-shaped
// attack on the field above. A driver is third-party code and the xBuddy dialect
// admits a frame of one mebibyte, so the reason is a string a printer sizes.
//
// This surface passes on whatever the device plane hands it, and the plane
// applies ADR-20260829-jdq rule 2 before that. The audit line is held to the same bound
// here anyway, because a DeviceFleet is an interface and the line is written
// whether anybody reads it or not.
func TestALongRefusalReachesTheCallerAndIsClippedInTheLog(t *testing.T) {
	t.Parallel()

	// One ASCII byte, then multi-byte characters, so a character straddles the
	// bound rather than ending on it. A cut taken at the byte offset leaves half
	// a character behind, which every encoder downstream renders as U+FFFD.
	shouted := "x" + strings.Repeat("é", machinetext.MaxBytes)

	fleet := &gcodeFleetStub{
		printer:  devPrinter(),
		notOKAt:  map[string]bool{"G28": true},
		reasonAt: map[string]string{"G28": shouted},
	}

	var logged bytes.Buffer

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     gcodeManager(),
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    fleet,
		Stores:     noStorages{},
		Events:     noEvents{},
		Mounts:     devMounts(),
		Logger:     slog.New(slog.NewJSONHandler(&logged, nil)),
	})
	require.NoError(t, err)

	rec := call(t, server.Handler(), http.MethodPost, gcodePath(), `{"lines":["G28"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	step := decodeOutcome(t, rec.Body.Bytes()).Steps[0]

	var line struct {
		MachineReason string `json:"machine_reason"`
	}

	require.NoError(t, json.Unmarshal(logged.Bytes(), &line), logged.String())

	t.Logf("observed: the printer sent %d bytes, the answer carries %d, the audit line kept %d",
		len(shouted), len(step.MachineReason), len(line.MachineReason))

	require.Equal(t, shouted, step.MachineReason,
		"this surface changed what the plane handed it")
	require.NotEmpty(t, line.MachineReason, "the audit line does not say why")
	require.Less(t, len(line.MachineReason), len(shouted),
		"one printer sizes the audit log, so a run of 64 lines sizes it 64 times")
	require.LessOrEqual(t, len(line.MachineReason), machinetext.MaxBytes+len(machinetext.Marker))
	require.NotContains(t, line.MachineReason, "�",
		"the reason was cut through a character rather than on a rune boundary")
}

// The authorization decision, in both directions. An operator commands the
// printer and does not send it raw gcode, and the refusal happens before
// anything reaches the device plane.
func TestRawGCodeTakesItsOwnActionAndNotSendCommand(t *testing.T) {
	t.Parallel()

	t.Run("a manager runs it", func(t *testing.T) {
		t.Parallel()

		fleet := &gcodeFleetStub{printer: devPrinter()}
		rec := call(t, gcodeServer(t, fleet, gcodeManager()).Handler(),
			http.MethodPost, gcodePath(), `{"lines":["G28"]}`)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Len(t, fleet.received, 1)
	})

	t.Run("an operator does not", func(t *testing.T) {
		t.Parallel()

		fleet := &gcodeFleetStub{printer: devPrinter()}
		grants := commandOnly()

		rec := call(t, gcodeServer(t, fleet, grants).Handler(),
			http.MethodPost, gcodePath(), `{"lines":["G28"]}`)

		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		require.Empty(t, fleet.received, "a refused caller reached the device plane")
		require.Contains(t, grants.askedVerbs, authz.RawGCodeVerbs()[0])
		// The object param is the authorization object, and the graph names a
		// machine device:<id>. A refusal naming it some other way sends an
		// operator to a grant nothing is written on.
		require.Contains(t, rec.Body.String(), `"object":"device:`,
			"the refusal names the object under a type the graph does not hold")
	})
}

// The door beside the one being closed. The generic command endpoint takes a
// verb in its body and can name a raw gcode verb, so the extra action has to
// hold there as well or the dedicated endpoint is a formality.
func TestTheGenericCommandEndpointRefusesARawGCodeVerbToAnOperator(t *testing.T) {
	t.Parallel()

	for _, verb := range authz.RawGCodeVerbs() {
		t.Run(verb, func(t *testing.T) {
			t.Parallel()

			fleet := &gcodeFleetStub{printer: devPrinter()}
			body := `{"verb":"` + verb + `","args":{"gcode":"M104 S215"}}`

			rec := call(t, gcodeServer(t, fleet, commandOnly()).Handler(),
				http.MethodPost, "/api/v1/devices/pdeadbeef/commands", body)

			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
			require.Empty(t, fleet.received,
				"%s reached the device plane through the generic command endpoint", verb)
		})
	}
}

// The same endpoint must still carry every other verb for an operator, or the
// decorator has closed a door and broken the corridor.
func TestTheGenericCommandEndpointStillCarriesEveryOtherVerb(t *testing.T) {
	t.Parallel()

	fleet := &gcodeFleetStub{printer: devPrinter()}

	rec := call(t, gcodeServer(t, fleet, commandOnly()).Handler(),
		http.MethodPost, "/api/v1/devices/pdeadbeef/commands",
		`{"verb":"core.command.pause_print"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, fleet.received, 1)
	require.Equal(t, "core.command.pause_print", fleet.received[0].Verb)
}

// force picks the out-of-band verb, which is the only thing the flag does.
func TestForceSelectsTheOutOfBandVerb(t *testing.T) {
	t.Parallel()

	fleet := &gcodeFleetStub{printer: devPrinter()}

	rec := call(t, gcodeServer(t, fleet, gcodeManager()).Handler(),
		http.MethodPost, gcodePath(), `{"lines":["M112"],"force":true}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, fleet.received, 1)
	require.Equal(t, "core.command.force_gcode", fleet.received[0].Verb)
}

// The bound, in both directions. Huma's schema refuses these before the handler
// and the handler refuses them again, and this asserts what a caller sees.
func TestASequenceIsBounded(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"no lines at all": `{"lines":[]}`,
		"no lines key":    `{}`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fleet := &gcodeFleetStub{printer: devPrinter()}
			rec := call(t, gcodeServer(t, fleet, gcodeManager()).Handler(), http.MethodPost, gcodePath(), body)

			require.GreaterOrEqual(t, rec.Code, http.StatusBadRequest, rec.Body.String())
			require.Empty(t, fleet.received)
		})
	}

	t.Run("more lines than the bound", func(t *testing.T) {
		t.Parallel()

		lines := make([]string, MaxGCodeSteps+1)
		for i := range lines {
			lines[i] = "G28"
		}

		encoded, err := json.Marshal(map[string]any{"lines": lines})
		require.NoError(t, err)

		fleet := &gcodeFleetStub{printer: devPrinter()}
		rec := call(t, gcodeServer(t, fleet, gcodeManager()).Handler(),
			http.MethodPost, gcodePath(), string(encoded))

		require.GreaterOrEqual(t, rec.Code, http.StatusBadRequest, rec.Body.String())
		require.Empty(t, fleet.received, "a run past the bound reached the printer")
	})

	t.Run("exactly the bound", func(t *testing.T) {
		t.Parallel()

		lines := make([]string, MaxGCodeSteps)
		for i := range lines {
			lines[i] = "G28"
		}

		encoded, err := json.Marshal(map[string]any{"lines": lines})
		require.NoError(t, err)

		fleet := &gcodeFleetStub{printer: devPrinter()}
		rec := call(t, gcodeServer(t, fleet, gcodeManager()).Handler(),
			http.MethodPost, gcodePath(), string(encoded))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Len(t, fleet.received, MaxGCodeSteps)
	})
}

// A printer the caller cannot reach is refused by the device plane, and the
// refusal is the one the rest of this surface gives.
func TestAnUnknownPrinterIsRefusedTheSameWayEverywhereElse(t *testing.T) {
	t.Parallel()

	fleet := &gcodeFleetStub{printer: devPrinter()}

	rec := call(t, gcodeServer(t, fleet, gcodeManager()).Handler(),
		http.MethodPost, "/api/v1/devices/pnothere/gcode", `{"lines":["G28"]}`)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// The audit line. Raw gcode is the one command an operator wants a record of
// afterwards, and a record that omits the payload records nothing worth having
// (ADR-20260825-z65 rule 13).
func TestEveryRawGCodeCommandIsLoggedWithItsPayloadAndItsCaller(t *testing.T) {
	t.Parallel()

	var written bytes.Buffer

	fleet := &gcodeFleetStub{
		printer:  devPrinter(),
		refuseAt: map[string]error{"M997": connectorapi.GCodeNotAllowed.New("refused")},
	}

	server, err := New(Config{
		Version:    "test",
		Logger:     slog.New(slog.NewTextHandler(&written, nil)),
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     gcodeManager(),
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    fleet,
		Stores:     noStorages{},
		Events:     noEvents{},
		Mounts:     devMounts(),
	})
	require.NoError(t, err)

	rec := call(t, server.Handler(), http.MethodPost, gcodePath(),
		`{"lines":["M117 hello","M997","M104 S215"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	logged := written.String()

	for _, want := range []string{
		`principal=user:operator`,
		`printer=pdeadbeef`,
		`verb=core.command.send_gcode`,
		`gcode="M117 hello"`,
		`accepted=true`,
		`gcode=M997`,
		`accepted=false`,
	} {
		require.Containsf(t, logged, want, "the audit line does not carry %s", want)
	}

	// The line that was never attempted must not appear as one that was.
	require.NotContains(t, logged, `gcode="M104 S215"`,
		"a line the run did not attempt was logged as one that was sent")
}
