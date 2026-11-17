package bridge_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/prusa3d/fractal-link/bridge"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// ready builds a bridge with one printer that is printing and registers its
// interfaces. It returns the fake host and the fake node.
func ready(t *testing.T) (*fakeFleet, *fakeNode) {
	t.Helper()

	return readyWith(t, nil)
}

// readyWith builds a bridge as ready does, with the readings in change
// written over the printing printer's.
func readyWith(t *testing.T, change map[string]string) (*fakeFleet, *fakeNode) {
	t.Helper()

	reading := telemetry()
	for path, value := range change {
		reading[path] = json.RawMessage(value)
	}

	fleet := newFleet(printer(printerOne, reading))
	node := newNode()

	if err := build(t, fleet, node, fixed(at())).Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	return fleet, node
}

// raws returns the given JSON texts as the parameter list of a method call.
func raws(values ...string) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(values))
	for _, value := range values {
		out = append(out, json.RawMessage(value))
	}

	return out
}

// TestAMethodBecomesOneCommand: every method in mapping.json becomes one
// command with the verb and the arguments of its row, sent with the principal
// of the node above.
func TestAMethodBecomesOneCommand(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		iface    fractal.InterfaceName
		instance fractal.InstanceID
		method   fractal.MethodName
		params   []json.RawMessage
		state    string
		verb     string
		args     map[string]string
	}{
		"pause": {
			iface: "Printer1", instance: fractal.InstanceID(printerOne), method: "PauseJob",
			verb: "core.command.pause_print",
		},
		"print one file": {
			iface: "Printer1", instance: fractal.InstanceID(printerOne), method: "PrintFile",
			params: raws(`"file:///usb/part.bgcode"`), state: `"READY"`, verb: "core.command.start_print",
			args: map[string]string{"path": "usb/part.bgcode"},
		},
		"home two axes": {
			iface: "FDMPrinter1", instance: fractal.InstanceID(printerOne), method: "Home",
			params: raws(`true`, `false`, `true`), state: `"IDLE"`, verb: "core.command.home",
			args: map[string]string{"axis": "XZ"},
		},
		"extrude without a feedrate": {
			iface: "FDMPrinterHead1", instance: fractal.InstanceID(printerOne + "_1"),
			method: "Extrude", params: raws(`12.5`), state: `"READY"`, verb: "core.command.move_e",
			args: map[string]string{"length": "12.5", "feedrate": "27"},
		},
		"extrude with a feedrate": {
			iface: "FDMPrinterHead1", instance: fractal.InstanceID(printerOne + "_1"),
			method: "Extrude", params: raws(`12.5`, `599.6`), state: `"READY"`, verb: "core.command.move_e",
			args: map[string]string{"length": "12.5", "feedrate": "600"},
		},
		"load a filament": {
			iface: "FDMPrinterHead1", instance: fractal.InstanceID(printerOne + "_1"),
			method: "LoadFilament", params: raws(`"PETG"`), state: `"READY"`, verb: "core.command.load_filament",
			args: map[string]string{"filament": "PETG"},
		},
		"load a filament the printer names otherwise": {
			iface: "FDMPrinterHead1", instance: fractal.InstanceID(printerOne + "_1"),
			method: "LoadFilament", params: raws(`"TPU"`), state: `"READY"`, verb: "core.command.load_filament",
			args: map[string]string{"filament": "FLEX"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fleet, node := ready(t)
			if tc.state != "" {
				fleet, node = readyWith(t, map[string]string{"/state": tc.state})
			}

			owner := node.owner(t, tc.iface, tc.instance)

			result, err := owner.Invoke(t.Context(), fractalnode.Call{
				Method: tc.method,
				Params: tc.params,
				Caller: "hq",
			})
			if err != nil {
				t.Fatalf("Invoke returned an error: %v", err)
			}

			if result != nil {
				t.Errorf("Invoke returned the result %s, and no method of the Prusa vocabulary returns a value", result)
			}

			sent := fleet.sent()
			if len(sent) != 1 {
				t.Fatalf("the method call became %d commands, want 1", len(sent))
			}

			if sent[0].verb != tc.verb {
				t.Errorf("the verb is %s, want %s", sent[0].verb, tc.verb)
			}

			if sent[0].device != printerOne {
				t.Errorf("the device of the command is %s, want %s", sent[0].device, printerOne)
			}

			if sent[0].principal != "node:hq" {
				t.Errorf("the principal of the command is %q, want node:hq", sent[0].principal)
			}

			compareArgs(t, sent[0].args, tc.args)
		})
	}
}

// compareArgs fails the test when the arguments of a command differ from want.
func compareArgs(t *testing.T, got, want map[string]string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("the arguments of the command are %v, want %v", got, want)
	}

	for name, value := range want {
		if got[name] != value {
			t.Errorf("argument %s is %q, want %q", name, got[name], value)
		}
	}
}

// TestARefusalByThisServerComesBackAsACode: the bridge converts each error of
// the host into a fractal.Error with its own code, so the server above tells
// the cases apart by the number. The text of the host's error is not sent to
// the server above.
func TestARefusalByThisServerComesBackAsACode(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		refuse error
		want   fractal.Code
		base   fractal.Code
	}{
		"denied":    {refuse: deviceapi.ErrDenied, want: bridge.CodeCallNotAllowed, base: fractal.CodeDenied},
		"no device": {refuse: deviceapi.ErrNoDevice, want: bridge.CodeDeviceGone, base: fractal.CodeObjectNotFound},
		"no verb": {
			refuse: deviceapi.ErrVerbUnknown, want: bridge.CodeCommandUnknown, base: fractal.CodeInvalidParams,
		},
		"argument": {
			refuse: deviceapi.ErrArgument, want: bridge.CodeValueRefused, base: fractal.CodeInvalidParams,
		},
		"not ready": {
			refuse: deviceapi.ErrNotReady, want: bridge.CodePrinterUnreachable, base: fractal.CodeInternal,
		},
		"busy": {refuse: deviceapi.ErrBusy, want: bridge.CodePrinterUnreachable, base: fractal.CodeInternal},
		"unexpected": {
			refuse: errors.New("something inside this server"),
			want:   bridge.CodeBridgeFailed,
			base:   fractal.CodeInternal,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fleet, node := ready(t)
			fleet.refuse = tc.refuse

			owner := node.owner(t, "Printer1", fractal.InstanceID(printerOne))

			_, err := owner.Invoke(t.Context(), fractalnode.Call{Method: "PauseJob", Caller: "hq"})

			var raised *fractal.Error
			if !errors.As(err, &raised) {
				t.Fatalf("Invoke returned %v, want a fractal.Error", err)
			}

			if raised.Code != tc.want {
				t.Errorf("the code is %s, want %s", raised.Code, tc.want)
			}

			if !raised.Code.Is(tc.base) {
				t.Errorf("%s does not refine %s", raised.Code, tc.base)
			}

			text, known := bridge.TextOf(tc.want)
			if !known || raised.Message != text {
				t.Errorf("the message is %q, want the text of the table, %q", raised.Message, text)
			}
		})
	}
}

// TestADeniedCallExecutesNothing: the bridge sends the one command to the
// host, the host denies it, and the caller receives CodeCallNotAllowed.
func TestADeniedCallExecutesNothing(t *testing.T) {
	t.Parallel()

	fleet, node := ready(t)
	fleet.refuse = deviceapi.ErrDenied

	owner := node.owner(t, "Printer1", fractal.InstanceID(printerOne))

	_, err := owner.Invoke(t.Context(), fractalnode.Call{Method: "CancelJob", Caller: "hq"})

	var raised *fractal.Error
	if !errors.As(err, &raised) || raised.Code != bridge.CodeCallNotAllowed {
		t.Fatalf("Invoke returned %v, want a fractal.Error with CodeCallNotAllowed", err)
	}

	sent := fleet.sent()
	if len(sent) != 1 || sent[0].verb != "core.command.stop_print" {
		t.Fatalf("the host received %v, want one core.command.stop_print", sent)
	}
}

// TestADeviceThatSaysNoIsADeliberateRefusal: when the printer refuses a
// command, the caller receives CodePrinterRefused with the word the printer
// gave.
func TestADeviceThatSaysNoIsADeliberateRefusal(t *testing.T) {
	t.Parallel()

	fleet, node := ready(t)
	fleet.machine = "the printer is not printing"

	owner := node.owner(t, "Printer1", fractal.InstanceID(printerOne))

	_, err := owner.Invoke(t.Context(), fractalnode.Call{Method: "PauseJob", Caller: "hq"})

	var raised *fractal.Error
	if !errors.As(err, &raised) || raised.Code != bridge.CodePrinterRefused {
		t.Fatalf("Invoke returned %v, want a fractal.Error with CodePrinterRefused", err)
	}

	if raised.Message != "the printer refused the command: the printer is not printing" {
		t.Errorf("the message is %q, and it does not have the word the printer gave", raised.Message)
	}
}

// TestAMethodThisServerDoesNotProvideIsNotFound: mapping.json has no row for
// SetNozzle. A caller that invokes it receives CodeMethodUnknown, a refinement
// of ERR_OBJECT_NOT_FOUND, at once, and does not wait for a timeout.
func TestAMethodThisServerDoesNotProvideIsNotFound(t *testing.T) {
	t.Parallel()

	fleet, node := ready(t)
	owner := node.owner(t, "FDMPrinterHead1", fractal.InstanceID(printerOne+"_1"))

	_, err := owner.Invoke(t.Context(), fractalnode.Call{
		Method: "SetNozzle",
		Params: raws(`"HARDENED"`, `0.6`, `true`),
		Caller: "hq",
	})

	var raised *fractal.Error
	if !errors.As(err, &raised) || raised.Code != bridge.CodeMethodUnknown ||
		!raised.Code.Is(fractal.CodeObjectNotFound) {
		t.Fatalf("Invoke returned %v, want a fractal.Error with CodeMethodUnknown, refining %s",
			err, fractal.CodeObjectNotFound)
	}

	if len(fleet.sent()) != 0 {
		t.Fatal("the host received a command for a method that mapping.json has no row for")
	}
}

// TestAMethodMissingAParameterIsRefusedBeforeTheDevice: PrintFile needs a file
// parameter. When the call has none, the bridge returns CodeParamMissing and
// sends no command.
func TestAMethodMissingAParameterIsRefusedBeforeTheDevice(t *testing.T) {
	t.Parallel()

	fleet, node := ready(t)
	owner := node.owner(t, "Printer1", fractal.InstanceID(printerOne))

	_, err := owner.Invoke(t.Context(), fractalnode.Call{Method: "PrintFile", Caller: "hq"})

	var raised *fractal.Error
	if !errors.As(err, &raised) || raised.Code != bridge.CodeParamMissing {
		t.Fatalf("Invoke returned %v, want a fractal.Error with CodeParamMissing", err)
	}

	if len(fleet.sent()) != 0 {
		t.Fatal("the host received a PrintFile command with no file")
	}
}

// TestFileURLs: PrintFile and UpgradeFromFile take a URL that always has a
// scheme. A file URL with no authority, or localhost, names a file on the
// printer's own storage: the printer gets its path without the leading slash,
// percent-decoded. A value with no scheme, or a scheme this server does not
// take, gets CodeURLNotFetched, and a file URL it cannot read as a path gets
// CodeURLMalformed. Neither reaches the printer (domain-common.md,
// Conventions).
func TestFileURLs(t *testing.T) {
	t.Parallel()

	// outcome is what one call did: the code it was answered with, zero for
	// none, and the path of every command the host received.
	type outcome struct {
		code  fractal.Code
		paths []string
	}

	refused := outcome{code: bridge.CodeURLNotFetched}
	malformed := outcome{code: bridge.CodeURLMalformed}
	sent := func(path string) outcome { return outcome{paths: []string{path}} }

	for name, tc := range map[string]struct {
		iface  fractal.InterfaceName
		method fractal.MethodName
		url    string
		want   outcome
	}{
		"a file URL":            {"Printer1", "PrintFile", "file:///usb/part.bgcode", sent("usb/part.bgcode")},
		"localhost":             {"Printer1", "PrintFile", "file://localhost/usb/part.bgcode", sent("usb/part.bgcode")},
		"no authority":          {"Printer1", "PrintFile", "file:/usb/part.bgcode", sent("usb/part.bgcode")},
		"an upper-case scheme":  {"Printer1", "PrintFile", "FILE:///usb/part.bgcode", sent("usb/part.bgcode")},
		"an escaped percent":    {"Printer1", "PrintFile", "file:///usb/50%25.bgcode", sent("usb/50%.bgcode")},
		"an escaped space":      {"Printer1", "PrintFile", "file:///usb/my%20part.bgcode", sent("usb/my part.bgcode")},
		"a colon in the path":   {"Printer1", "PrintFile", "file:///usb/a:b.bgcode", sent("usb/a:b.bgcode")},
		"a firmware file":       {"UpgradableDevice1", "UpgradeFromFile", "file:///usb/fw.bbf", sent("usb/fw.bbf")},
		"no scheme":             {"Printer1", "PrintFile", "usb/part.bgcode", refused},
		"no scheme, from root":  {"Printer1", "PrintFile", "/usb/part.bgcode", refused},
		"a colon first":         {"Printer1", "PrintFile", ":part.bgcode", refused},
		"http":                  {"Printer1", "PrintFile", "http://example.com/part.bgcode", refused},
		"a firmware path":       {"UpgradableDevice1", "UpgradeFromFile", "usb/fw.bbf", refused},
		"a firmware over https": {"UpgradableDevice1", "UpgradeFromFile", "https://example.com/fw.bbf", refused},
		"a host":                {"Printer1", "PrintFile", "file://printer/usb/part.bgcode", malformed},
		"a port":                {"Printer1", "PrintFile", "file://localhost:80/usb/part.bgcode", malformed},
		"a user":                {"Printer1", "PrintFile", "file://me@/usb/part.bgcode", malformed},
		"an opaque path":        {"Printer1", "PrintFile", "file:usb/part.bgcode", malformed},
		"an empty path":         {"Printer1", "PrintFile", "file:///", malformed},
		"no path":               {"Printer1", "PrintFile", "file://", malformed},
		"a dot-dot segment":     {"Printer1", "PrintFile", "file:///usb/../part.bgcode", malformed},
		"an escaped dot-dot":    {"Printer1", "PrintFile", "file:///usb/%2E%2E/part.bgcode", malformed},
		"an escaped slash":      {"Printer1", "PrintFile", "file:///usb%2Fpart.bgcode", malformed},
		"a bad escape":          {"Printer1", "PrintFile", "file:///usb/50%.bgcode", malformed},
		"a query":               {"Printer1", "PrintFile", "file:///usb/part.bgcode?x", malformed},
		"a fragment":            {"Printer1", "PrintFile", "file:///usb/part.bgcode#x", malformed},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fleet, node := readyWith(t, map[string]string{"/state": `"READY"`})
			owner := node.owner(t, tc.iface, fractal.InstanceID(printerOne))

			_, err := owner.Invoke(t.Context(), fractalnode.Call{
				Method: tc.method,
				Params: raws(strconv.Quote(tc.url)),
				Caller: "hq",
			})

			var got outcome

			if raised, ok := errors.AsType[*fractal.Error](err); ok {
				got.code = raised.Code
			} else if err != nil {
				t.Fatalf("Invoke returned %v, want a fractal.Error or no error", err)
			}

			for _, command := range fleet.sent() {
				got.paths = append(got.paths, command.args["path"])
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("the call gave %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestHomingNoAxisSendsNothing: Home homes the axes set to true. With every
// axis false it homes none and succeeds, and sends no command, because the
// home command with no axis homes all of them.
func TestHomingNoAxisSendsNothing(t *testing.T) {
	t.Parallel()

	fleet, node := readyWith(t, map[string]string{"/state": `"IDLE"`})
	owner := node.owner(t, "FDMPrinter1", fractal.InstanceID(printerOne))

	_, err := owner.Invoke(t.Context(), fractalnode.Call{
		Method: "Home",
		Params: raws(`false`, `false`, `false`),
		Caller: "hq",
	})
	if err != nil {
		t.Fatalf("Invoke refused a Home call with no axis: %v", err)
	}

	if len(fleet.sent()) != 0 {
		t.Fatal("the host received a home command with no axis")
	}
}

// TestARequestedChangeBecomesOneCommand: a requested change of a writable
// property becomes one command with the verb of its "write" row. A request for
// null sends the "nullAs" text of the row.
func TestARequestedChangeBecomesOneCommand(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		iface    fractal.InterfaceName
		instance fractal.InstanceID
		props    fractal.Props
		verb     string
		args     map[string]string
	}{
		"a nozzle setpoint": {
			iface: "FDMPrinterHead1", instance: fractal.InstanceID(printerOne + "_1"),
			props: fractal.Props{"targetNozzle": json.RawMessage(`215.5`)},
			verb:  "core.command.set_nozzle_temperature",
			args:  map[string]string{"nozzle_temperature": "216"},
		},
		"a bed setpoint": {
			iface: "FDMPrinter1", instance: fractal.InstanceID(printerOne),
			props: fractal.Props{"targetBed": json.RawMessage(`59.4`)},
			verb:  "core.command.set_bed_temperature",
			args:  map[string]string{"bed_temperature": "59"},
		},
		"a heater turned off": {
			iface: "FDMPrinterHead1", instance: fractal.InstanceID(printerOne + "_1"),
			props: fractal.Props{"targetNozzle": json.RawMessage(`null`)},
			verb:  "core.command.set_nozzle_temperature",
			args:  map[string]string{"nozzle_temperature": "0"},
		},
		"a speed override": {
			iface: "Printer1", instance: fractal.InstanceID(printerOne),
			props: fractal.Props{"speed": json.RawMessage(`80`)},
			verb:  "core.command.set_speed",
			args:  map[string]string{"speed": "80"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fleet, node := ready(t)
			owner := node.owner(t, tc.iface, tc.instance)

			changed, err := owner.Desire(t.Context(), tc.props)
			if err != nil {
				t.Fatalf("Desire returned an error: %v", err)
			}

			if len(changed) != 0 {
				t.Errorf("Desire returned the values %v, want none: the device reports the new value", changed)
			}

			sent := fleet.sent()
			if len(sent) != 1 {
				t.Fatalf("the change became %d commands, want 1", len(sent))
			}

			if sent[0].verb != tc.verb {
				t.Errorf("the verb is %s, want %s", sent[0].verb, tc.verb)
			}

			compareArgs(t, sent[0].args, tc.args)
		})
	}
}

// TestAChangeOfSeveralPropertiesIsSentInNameOrder: a refusal stops the
// commands after it, so a request with several properties sends them in one
// order every time, sorted by name.
func TestAChangeOfSeveralPropertiesIsSentInNameOrder(t *testing.T) {
	t.Parallel()

	want := []string{"core.command.set_flow", "core.command.set_bed_temperature"}

	for range 20 {
		fleet, node := ready(t)
		owner := node.owner(t, "FDMPrinter1", fractal.InstanceID(printerOne))

		if _, err := owner.Desire(t.Context(), fractal.Props{
			"targetBed": json.RawMessage(`60`),
			"flow":      json.RawMessage(`95`),
		}); err != nil {
			t.Fatalf("Desire returned an error: %v", err)
		}

		var got []string
		for _, command := range fleet.sent() {
			got = append(got, command.verb)
		}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("the change sent %v, want %v", got, want)
		}
	}
}

// TestAChangeToAPropertyWithNoVerbIsRefused: Desire refuses a change to the
// read-only property state, which has no "write" row, and sends no command.
func TestAChangeToAPropertyWithNoVerbIsRefused(t *testing.T) {
	t.Parallel()

	fleet, node := ready(t)
	owner := node.owner(t, "Printer1", fractal.InstanceID(printerOne))

	if _, err := owner.Desire(t.Context(), fractal.Props{
		"state": json.RawMessage(`"READY"`),
	}); err == nil {
		t.Fatal("Desire accepted a change to the read-only property state")
	}

	if len(fleet.sent()) != 0 {
		t.Fatal("the host received a command for a property with no write row")
	}
}

// TestAChangeThisServerCannotRenderIsRefused: a command argument is a number,
// a string or a boolean. Desire refuses any other value, such as an array, and
// sends no command.
func TestAChangeThisServerCannotRenderIsRefused(t *testing.T) {
	t.Parallel()

	fleet, node := ready(t)
	owner := node.owner(t, "Printer1", fractal.InstanceID(printerOne))

	if _, err := owner.Desire(t.Context(), fractal.Props{
		"speed": json.RawMessage(`[1,2]`),
	}); err == nil {
		t.Fatal("Desire accepted an array as the value of speed")
	}

	if len(fleet.sent()) != 0 {
		t.Fatal("the host received a command with an array as its value")
	}
}

// TestACallForADeviceThatWentAwayIsNotFound: the interface instance of a
// device stays registered after the device leaves, because Fractal has no
// message that removes an interface. A method call for a printer the host no
// longer lists returns CodeDeviceGone, also where a state rule would refuse
// the printer as BLOCKED or a parameter is invalid.
func TestACallForADeviceThatWentAwayIsNotFound(t *testing.T) {
	t.Parallel()

	for name, call := range map[string]fractalnode.Call{
		"Beep":                {Method: "Beep"},
		"PauseJob":            {Method: "PauseJob"},
		"CancelJob":           {Method: "CancelJob"},
		"PrintFile of a path": {Method: "PrintFile", Params: raws(`"usb/x.gcode"`)},
		"PrintFile of a file": {Method: "PrintFile", Params: raws(`"file:///usb/x.gcode"`)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fleet := newFleet(printer(printerTwo, telemetry()))
			node := newNode()
			made := build(t, fleet, node, fixed(at()))

			if err := made.Sync(t.Context()); err != nil {
				t.Fatalf("read the devices: %v", err)
			}

			fleet.remove(printerTwo)
			fleet.refuse = deviceapi.ErrNoDevice
			made.Touch(t.Context(), printerTwo)

			call.Caller = "hq"

			_, err := node.owner(t, "Printer1", fractal.InstanceID(printerTwo)).Invoke(t.Context(), call)

			raised, ok := errors.AsType[*fractal.Error](err)
			if !ok {
				t.Fatalf("Invoke returned %v, want a fractal.Error", err)
			}

			got := callOutcome{code: raised.Code}
			for _, command := range fleet.sent() {
				got.verbs = append(got.verbs, command.verb)
			}

			if want := (callOutcome{code: bridge.CodeDeviceGone}); !reflect.DeepEqual(got, want) {
				t.Errorf("the call gave %+v, want %+v", got, want)
			}
		})
	}
}

// TestAMethodFollowsTheStateRulesOfTheVocabulary: domain-3d-print.md says in
// which states a method must be refused. The bridge refuses it there with
// CodeStateRefused and sends no command, and sends it in any other state.
func TestAMethodFollowsTheStateRulesOfTheVocabulary(t *testing.T) {
	t.Parallel()

	const (
		printing = `"PRINTING"`
		paused   = `"PAUSED"`
		busy     = `"BUSY"`
		blocked  = `"IDLE"`
		readyNow = `"READY"`
		failed   = `"ERROR"`
	)

	head := fractal.InstanceID(printerOne + "_1")
	one := fractal.InstanceID(printerOne)

	for name, tc := range map[string]struct {
		iface    fractal.InterfaceName
		instance fractal.InstanceID
		method   fractal.MethodName
		params   []json.RawMessage
		change   map[string]string
		want     callOutcome
	}{
		"pause a paused print":   {"Printer1", one, "PauseJob", nil, map[string]string{"/state": paused}, stateRefused},
		"resume a running print": {"Printer1", one, "ResumeJob", nil, map[string]string{"/state": printing}, stateRefused},
		"cancel with no print":   {"Printer1", one, "CancelJob", nil, map[string]string{"/state": blocked}, stateRefused},
		"cancel a held print": {
			"Printer1", one, "CancelJob", nil,
			map[string]string{"/state": `"ATTENTION"`, "/job/state": printing}, sentOne("core.command.stop_print"),
		},
		"print onto a full bed": {
			"Printer1", one, "PrintFile", raws(`"file:///usb/part.bgcode"`),
			map[string]string{"/state": `"FINISHED"`}, stateRefused,
		},
		"block while printing": {"Printer1", one, "SetBlocked", nil, map[string]string{"/state": printing}, stateRefused},
		"ready after an error": {
			"Printer1", one, "SetReady", nil, map[string]string{"/state": failed}, sentOne("core.command.set_printer_ready"),
		},
		"reset after an error": {
			"Printer1", one, "Reset", nil, map[string]string{"/state": failed}, sentOne("core.command.reset_printer"),
		},
		"home while printing": {
			"FDMPrinter1", one, "Home", raws(`true`, `true`, `true`), map[string]string{"/state": printing}, stateRefused,
		},
		"home no axis while printing": {
			"FDMPrinter1", one, "Home", raws(`false`, `false`, `false`), map[string]string{"/state": printing},
			stateRefused,
		},
		"home a blocked printer": {
			"FDMPrinter1", one, "Home", raws(`true`, `true`, `true`), map[string]string{"/state": blocked},
			sentOne("core.command.home"),
		},
		"level a blocked bed": {"FDMPrinter1", one, "LevelBed", nil, map[string]string{"/state": blocked}, stateRefused},
		"level a ready bed": {
			"FDMPrinter1", one, "LevelBed", nil, map[string]string{"/state": readyNow},
			sentOne("core.command.mesh_bed_leveling"),
		},
		"release a busy printer": {
			"FDMPrinter1", one, "DisableSteppers", nil, map[string]string{"/state": busy}, stateRefused,
		},
		"load while busy": {
			"FDMPrinterHead1", head, "LoadFilament", raws(`"PLA"`), map[string]string{"/state": busy}, stateRefused,
		},
		"load while paused": {
			"FDMPrinterHead1", head, "LoadFilament", raws(`"PLA"`), map[string]string{"/state": paused},
			sentOne("core.command.load_filament"),
		},
		"unload while printing": {
			"FDMPrinterHead1", head, "UnloadFilament", nil, map[string]string{"/state": printing}, stateRefused,
		},
		"extrude while printing": {
			"FDMPrinterHead1", head, "Extrude", raws(`5`, `100`), map[string]string{"/state": printing}, stateRefused,
		},
		"load nothing": {
			"FDMPrinterHead1", head, "LoadFilament", raws(`"NOTHING"`), map[string]string{"/state": readyNow},
			callOutcome{code: bridge.CodeValueRefused},
		},
		"extrude with nothing to heat to": {
			"FDMPrinterHead1", head, "Extrude", raws(`5`, `100`),
			map[string]string{"/state": readyNow, "/tools/0/nozzle_target_temp": `0`, headMaterial: `""`},
			callOutcome{code: bridge.CodeNoTargetTemperature},
		},
		"extrude an unnamed filament": {
			"FDMPrinterHead1", head, "Extrude", raws(`5`, `100`),
			map[string]string{"/state": readyNow, "/tools/0/nozzle_target_temp": `0`, headMaterial: `"WOOD"`},
			callOutcome{code: bridge.CodeNoTargetTemperature},
		},
		"extrude a known filament": {
			"FDMPrinterHead1", head, "Extrude", raws(`5`, `100`),
			map[string]string{"/state": readyNow, "/tools/0/nozzle_target_temp": `0`, headMaterial: `"PLA"`},
			sentOne("core.command.move_e"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fleet, node := readyWith(t, tc.change)

			_, err := node.owner(t, tc.iface, tc.instance).Invoke(t.Context(), fractalnode.Call{
				Method: tc.method, Params: tc.params, Caller: "hq",
			})

			var got callOutcome

			if raised, ok := errors.AsType[*fractal.Error](err); ok {
				got.code = raised.Code
			} else if err != nil {
				t.Fatalf("Invoke returned %v, want a fractal.Error or no error", err)
			}

			for _, command := range fleet.sent() {
				got.verbs = append(got.verbs, command.verb)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("the call gave %+v, want %+v", got, tc.want)
			}
		})
	}
}

// callOutcome is what one method call did: the code it was answered with, zero
// for none, and the verb of every command the host received.
type callOutcome struct {
	code  fractal.Code
	verbs []string
}

//nolint:gochecknoglobals // a fixed answer the tests compare with.
var stateRefused = callOutcome{code: bridge.CodeStateRefused}

// sentOne is the callOutcome of a call that became one command with this verb.
func sentOne(verb string) callOutcome { return callOutcome{verbs: []string{verb}} }

// TestAMethodCallAndTelemetryRunTogether: the state rules read the printer's
// state while telemetry writes it. The race detector finds a read without
// the lock.
func TestAMethodCallAndTelemetryRunTogether(t *testing.T) {
	t.Parallel()

	node := newNode()
	made := build(t, newFleet(printer(printerOne, telemetry())), node, fixed(at()))

	if err := made.Sync(t.Context()); err != nil {
		t.Fatalf("read the devices: %v", err)
	}

	owner := node.owner(t, "Printer1", fractal.InstanceID(printerOne))
	done := make(chan struct{})

	go func() {
		defer close(done)

		for range 200 {
			made.Sample(t.Context(), printerOne, "/state", json.RawMessage(`"PRINTING"`), at())
		}
	}()

	for range 200 {
		if _, err := owner.Invoke(t.Context(), fractalnode.Call{Method: "PauseJob", Caller: "hq"}); err != nil {
			t.Fatalf("pause a print: %v", err)
		}
	}

	<-done
}
