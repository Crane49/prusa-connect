package xbuddy

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The rewrite, transcribed a second time.
//
// It is written out here rather than derived from wireTable because a table
// checked against itself checks nothing. The source is buildGCodeCommand and
// shouldConvertToSetValue in legacy/connector/xbuddy/commands/builder.go, which
// is the authority earlier research names, and the counts it gives are 19
// gcode verbs and 12 SET_VALUE verbs out of 57.
var (
	//nolint:gochecknoglobals // test data.
	expectedGCode = map[connectorapi.Capability]string{
		VerbHome:            "G28",
		VerbMeshBedLeveling: "G28\r\nG29",
		VerbReset:           "M999 R",
		VerbDisableSteppers: "M84",
		VerbBeep:            "M300 S950 P800",
		VerbLoadFilament:    "M701 W2",
		VerbUnloadFilament:  "M702 W2",
		VerbSetSpeed:        "M220 S150",
		VerbSetFlow:         "M221 S95",
		VerbSetNozzleTemp:   "M104 S215",
		VerbSetBedTemp:      "M140 S60",
		VerbMove:            "G91\r\nG1 F3000 X10.500 Y-2.250\r\nG90",
		VerbMoveE:           "M83\r\nG1 F300 E12.500",
		VerbMoveZ:           "G91\r\nG1 F600 Z5.000\r\nG90",
		VerbZPowerMove: "M906 Z900\r\nM569 S0 Z\r\nM211 S0\r\nG0 Z20.000 F480\r\n" +
			"M400\r\nM906 Z600\r\nM569 S1 Z\r\nM211 S1",
	}

	//nolint:gochecknoglobals // test data.
	gcodeArguments = map[connectorapi.Capability]map[string]string{
		VerbSetSpeed:      {"speed": "150"},
		VerbSetFlow:       {"flow": "95"},
		VerbSetNozzleTemp: {"nozzle_temperature": "215"},
		VerbSetBedTemp:    {"bed_temperature": "60"},
		VerbMove:          {"feedrate": "3000", "x": "10.5", "y": "-2.25"},
		VerbMoveE:         {"feedrate": "300", "length": "12.5"},
		VerbMoveZ:         {"feedrate": "600", "distance": "5"},
		VerbZPowerMove:    {"distance": "20"},
	}

	//nolint:gochecknoglobals // test data.
	expectedSetValue = []connectorapi.Capability{
		VerbSetChamberTemp, VerbSetChamberFan, VerbSetChamberLED, VerbSetAddonPower,
		VerbSetToolDiameter, VerbSetToolHardened, VerbSetToolHighFlow,
		VerbSetEnclosure, VerbSetEnclosureFilter, VerbSetEnclosurePost, VerbSetEnclosurePostFor,
		VerbSetHostname,
	}
)

// The rewrite is the piece of knowledge profiles.json is missing, so it is the
// piece a port is most likely to lose. This is the assertion that would fail if
// it were.
func TestTheRewriteMatchesTheShippingBuilder(t *testing.T) {
	t.Parallel()

	for key, want := range expectedGCode {
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()

			form, declared := wireTable[key]
			if !declared {
				t.Fatalf("%s has no wire form", key)
			}

			if form.frame != frameGCode {
				t.Fatalf("%s travels as %q, and the shipping builder makes it a G frame", key, string(form.frame))
			}

			code, err := form.gcode(arguments{
				verb: key, declared: form.args, values: gcodeArguments[key],
			})
			if err != nil {
				t.Fatalf("render %s: %v", key, err)
			}

			if code != want {
				t.Errorf("%s renders %q, and the shipping builder renders %q", key, code, want)
			}
		})
	}

	if len(expectedGCode) != 15 {
		t.Errorf("the transcription holds %d gcode verbs, want 15", len(expectedGCode))
	}
}

// The shipping builder has no tool form, so the expected line is the
// firmware's: M104 names a head from 0, and so does the argument. Head 1 is
// the right head of a two-head XL.
func TestANozzleTargetForOneToolNamesItFromZero(t *testing.T) {
	t.Parallel()

	form := wireTable[VerbSetNozzleTemp]

	code, err := form.gcode(arguments{
		verb: VerbSetNozzleTemp, declared: form.args,
		values: map[string]string{"nozzle_temperature": "215", "tool": "1"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if code != "M104 T1 S215" {
		t.Errorf("tool 1 at 215 renders %q, and the firmware reads M104 T1 S215", code)
	}
}

func TestTwelveSettersCollapseIntoOneSetValueFrame(t *testing.T) {
	t.Parallel()

	for _, key := range expectedSetValue {
		form, declared := wireTable[key]

		switch {
		case !declared:
			t.Errorf("%s has no wire form", key)
		case form.frame != frameJSON:
			t.Errorf("%s travels as %q, and a SET_VALUE is a J frame", key, string(form.frame))
		case form.command != setValueCommand:
			t.Errorf("%s reaches the socket as %q, and the shipping builder sends SET_VALUE", key, form.command)
		}
	}

	if len(expectedSetValue) != 12 {
		t.Errorf("the transcription holds %d setters, and earlier research counts 12", len(expectedSetValue))
	}

	for key, form := range wireTable {
		if form.command != setValueCommand || slices.Contains(expectedSetValue, key) {
			continue
		}

		t.Errorf("%s sends SET_VALUE and the transcription does not list it", key)
	}
}

// A SET_VALUE frame carries no verb, so the argument names are the only thing
// that says which setter was meant. Two setters with the same argument set
// would be a command the printer resolves by luck.
func TestNoTwoSettersShareAnArgumentSet(t *testing.T) {
	t.Parallel()

	seen := map[string]connectorapi.Capability{}

	for _, key := range expectedSetValue {
		names := slices.Sorted(maps.Keys(wireTable[key].args))
		signature := strings.Join(names, ",")

		if other, clash := seen[signature]; clash {
			t.Errorf("%s and %s both send SET_VALUE with %q, so neither can be told from the other",
				key, other, signature)
		}

		seen[signature] = key
	}
}

func TestAFrameCarriesTheNineByteHeader(t *testing.T) {
	t.Parallel()

	frame := encodeFrame(frameJSON, 0x2a, []byte(`{"command":"SEND_INFO"}`))

	if got, want := string(frame[:headerLength]), "J0000002a"; got != want {
		t.Errorf("the header is %q, want %q", got, want)
	}

	if got, want := string(frame[headerLength:]), `{"command":"SEND_INFO"}`; got != want {
		t.Errorf("the payload is %q, want %q", got, want)
	}
}

func TestAnInboundFrameIsClassifiedByItsContent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want inboundKind
	}{
		{"an event key makes it an event", `{"event":"FINISHED","command_id":7}`, inboundEvent},
		{"transfer inline makes it a block request", `{"transfer":"inline","file_id":1}`, inboundBlockRequest},
		{"anything else is telemetry", `{"state":"IDLE","temp_nozzle":25.5}`, inboundTelemetry},
		{"an event that also names a transfer is an event",
			`{"event":"TRANSFER_FINISHED","transfer":"inline"}`, inboundEvent},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			message, err := classify([]byte(testCase.body))
			if err != nil {
				t.Fatalf("classify: %v", err)
			}

			if message.kind != testCase.want {
				t.Errorf("classified as %d, want %d", message.kind, testCase.want)
			}
		})
	}
}

// A command id outside the uint32 the framing carries is refused rather than
// converted, because a conversion lands it on a command somebody is waiting for.
func TestACommandIdOutsideUint32IsRefused(t *testing.T) {
	t.Parallel()

	message, err := classify([]byte(`{"event":"FINISHED","command_id":4294967296}`))
	if err != nil {
		t.Fatalf("classify: %v", err)
	}

	if _, _, rangeErr := message.event.commandID(); rangeErr == nil {
		t.Error("an id above the uint32 range was accepted")
	}
}

// The descriptor says how a text argument is read, because "1" is a legal
// string and a legal integer and the firmware reads them differently.
func TestKwargsTakeTheirTypeFromTheDescriptor(t *testing.T) {
	t.Parallel()

	form := wireTable[VerbSetEnclosurePostFor]

	kwargs, err := arguments{
		verb: VerbSetEnclosurePostFor, declared: form.args,
		values: map[string]string{"enclosure_postprint_filtration_time": "600"},
	}.kwargs()
	if err != nil {
		t.Fatalf("kwargs: %v", err)
	}

	encoded, err := json.Marshal(kwargs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if got, want := string(encoded), `{"enclosure_postprint_filtration_time":600}`; got != want {
		t.Errorf("the kwargs encode as %s, want %s", got, want)
	}
}

func TestAPathReachesTheFirmwareNamespace(t *testing.T) {
	t.Parallel()

	cases := []struct{ path, want string }{
		{"model.gcode", "/usb/model.gcode"},
		{"usb/model.gcode", "/usb/model.gcode"},
		{"/usb/model.gcode", "/usb/model.gcode"},
		{"", "/usb"},
	}

	for _, testCase := range cases {
		if got := absolutePath(testCase.path, "/usb"); got != testCase.want {
			t.Errorf("%q becomes %q, want %q", testCase.path, got, testCase.want)
		}
	}
}

// The descriptor has no way to say "not this substring", so the traversal check
// lives in the encoder. Losing it puts a path the caller composed straight onto
// a filesystem.
//
// The last case is the one the shipping validator lets through: it checks the
// relative path and then prefixes the storage, so "../etc/passwd" carries no
// "/../" when it is checked and carries one when it is sent.
func TestATraversingPathIsRefused(t *testing.T) {
	t.Parallel()

	session := &Session{storage: "/usb"}

	for _, path := range []string{"usb/../../etc/passwd", "/usb/../etc/passwd", "../etc/passwd", ".."} {
		if _, err := session.wirePaths(connectorapi.Command{
			Verb: VerbStartPrint, Args: map[string]string{"path": path},
		}); err == nil {
			t.Errorf("the traversing path %q was accepted", path)
		}
	}
}

// servedNotSent are the baseline keys that are deliberately not one frame.
//
// A key here is a capability and not a command: it says what this driver can do
// for a caller, and it is served by an exchange rather than sent. Nothing may be
// added without a reason, because the ordinary case is still that a key in the
// baseline is a frame the driver can write.
var servedNotSent = map[connectorapi.Capability]string{
	connectorapi.VerbFileUpload: "the whole transfer exchange in transfer.go, which is a start command, " +
		"a stream of chunk frames the printer asks for, and an event that ends it. It is the generic " +
		"name the contract carries so that no caller learns this dialect's own transfer commands",
}

// The baseline is what a driver's code can do at all, so a verb without a wire
// form must not be in it unless it is served by an exchange, and a wire form
// without a key cannot be reached.
func TestTheBaselineIsExactlyTheWireTable(t *testing.T) {
	t.Parallel()

	set := baseline()

	for key := range wireTable {
		if !set.Has(key) {
			t.Errorf("%s has a wire form and is not in the baseline", key)
		}
	}

	for _, key := range set.Keys() {
		if _, isArgument := key.Argument(); isArgument {
			continue
		}

		if reason, served := servedNotSent[key]; served {
			if reason == "" {
				t.Errorf("%s is listed as served rather than sent, with no reason", key)
			}

			continue
		}

		if _, declared := wireTable[key]; !declared {
			t.Errorf("%s is in the baseline and has no wire form, so nothing can send it", key)
		}
	}

	for key := range servedNotSent {
		if !set.Has(key) {
			t.Errorf("%s is listed as served rather than sent and is not in the baseline", key)
		}

		if _, declared := wireTable[key]; declared {
			t.Errorf("%s is listed as served rather than sent and has a wire form, so the list is stale", key)
		}
	}

	if problems := set.Problems(); len(problems) > 0 {
		t.Errorf("the baseline breaks the descriptor rules: %v", problems)
	}
}

// The published baseline is the wire table less the verbs only the driver sends,
// each with a reason.
//
// It is the inverse of servedNotSent above and the other half of ADR-20260826-ezr rule
// 3: a published key is invokable through the command endpoint, so a wire form
// that must not be caller-invokable is kept out and says why. A verb that
// disappears from the published set without a reason fails here, and so does a
// reason for a verb that is published after all.
func TestThePublishedBaselineIsTheWireTableLessWhatOnlyTheDriverSends(t *testing.T) {
	t.Parallel()

	full, callable := baseline(), published()

	for key := range wireTable {
		reason, withheld := sentNotInvokable[key]

		switch {
		case withheld && reason == "":
			t.Errorf("%s is kept out of the published baseline with no reason", key)
		case withheld && callable.Has(key):
			t.Errorf("%s is kept out of the published baseline and is in it, so the list is stale", key)
		case !withheld && !callable.Has(key):
			t.Errorf("%s has a wire form and is not published, and nothing records why", key)
		}
	}

	for key := range sentNotInvokable {
		if _, declared := wireTable[key]; !declared {
			t.Errorf("%s is kept out of the published baseline and has no wire form, so nothing sends it", key)
		}

		if !full.Has(key) {
			t.Errorf("%s is kept out of the published baseline and is not in the driver's own set either, "+
				"so Transfers.Send cannot reach it", key)
		}

		for name := range wireTable[key].args {
			if callable.Has(connectorapi.ArgumentKey(key, name)) {
				t.Errorf("%s#%s is published for a verb that is not, which is a key naming a verb "+
					"no caller can send", key, name)
			}
		}
	}

	if problems := callable.Problems(); len(problems) > 0 {
		t.Errorf("the published baseline breaks the descriptor rules: %v", problems)
	}
}

// A refusal carries the firmware's own word for it, which is the difference
// between "that did not work" and "the printer already has this file". The
// second is answered by printing the file, and a host that only sees OK false
// has to re-parse the raw frame and know this dialect to tell them apart.
//
// machine_reason wins where the firmware sent both, because that is the value a
// caller compares. Where it sent only reason, the sentence is what there is:
// dropping it leaves a caller holding "not accepted" and nothing else, and the
// printer had already said why.
func TestARefusalCarriesTheMachineReason(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		event  printerEvent
		ok     bool
		reason string
	}{
		{
			name:   "a rejection naming a machine reason",
			event:  printerEvent{Event: eventRejected, MachineReason: "FILE_EXISTS", Reason: "already there"},
			reason: "FILE_EXISTS",
		},
		{
			name:   "a rejection with only a sentence for a person",
			event:  printerEvent{Event: eventRejected, Reason: "the printer is busy"},
			reason: "the printer is busy",
		},
		{
			name:  "a failure naming a machine reason",
			event: printerEvent{Event: eventFailed, MachineReason: "OUT_OF_FILAMENT"}, reason: "OUT_OF_FILAMENT",
		},
		{
			name:   "a failure with only a sentence for a person",
			event:  printerEvent{Event: eventFailed, Reason: "the heater did not reach target"},
			reason: "the heater did not reach target",
		},
		{
			name:  "an event that completes the command",
			event: printerEvent{Event: "FINISHED", MachineReason: "FILE_EXISTS"},
			ok:    true,
		},
		{
			name:  "an event that completes the command and carries a sentence",
			event: printerEvent{Event: "FINISHED", Reason: "resumed after the door was shut"},
			ok:    true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			answer := resultOf(tc.event, nil, nil, "core.command.beep").result

			if answer.OK != tc.ok {
				t.Errorf("OK = %v, want %v", answer.OK, tc.ok)
			}

			if answer.MachineReason != tc.reason {
				t.Errorf("machine reason = %q, want %q", answer.MachineReason, tc.reason)
			}
		})
	}
}

// TestARefusalReachesTheCallerWithThePrintersOwnWords is the same rule at the
// socket rather than at the decoder.
//
// The unit case above proves resultOf carries the sentence. This one proves it
// survives the whole round trip, because the value a caller acts on is the one
// Do returns and nothing between here and there is obliged to keep it.
func TestARefusalReachesTheCallerWithThePrintersOwnWords(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		frame string
		want  string
	}{
		{
			name:  "a refusal the firmware spelled for a person",
			frame: `{"event":"REJECTED","command_id":%d,"state":"IDLE","reason":"Printer is busy"}`,
			want:  "Printer is busy",
		},
		{
			name: "a refusal the firmware named a code for",
			frame: `{"event":"REJECTED","command_id":%d,"state":"IDLE","reason":"already here",` +
				`"machine_reason":"FILE_EXISTS"}`,
			want: "FILE_EXISTS",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			printer := standHostile(t)

			answered := printer.start(t)
			id := printer.takeFrame(t)

			printer.text(t, fmt.Sprintf(tc.frame, id))

			result, err := printer.await(t, answered)

			// The refusal travels as the registered code on the result and not
			// as an error, which is the driver contract on connectorapi.Result:
			// the printer answered, so the call did not fail.
			if err != nil {
				t.Fatalf("the refused command came back as %v, not as a refusal", err)
			}

			if got, want := result.FaultCode, connectorapi.CommandRefused.Code().String(); got != want {
				t.Errorf("the refusal carries the code %q, want %q", got, want)
			}

			if result.OK {
				t.Fatal("REJECTED came back as a command the printer accepted")
			}

			if result.MachineReason != tc.want {
				t.Errorf("the caller was told %q, and the printer said %q",
					result.MachineReason, tc.want)
			}

			t.Logf("observed: Do returned OK=%v MachineReason=%q raw=%s",
				result.OK, result.MachineReason, result.Raw)
		})
	}
}

// TestADialogTheMachineRaisesReachesTheStateTree is what makes
// core.command.dialog_action usable.
//
// The verb names a dialog_id and the printer is the only thing that knows one.
// Without the id in the state tree a caller has nothing to name, so a printer
// waiting on a dialog is one somebody has to walk to. The clearing half is here
// too: an id that outlives its dialog is a button a caller presses on a machine
// that has moved on.
func TestADialogTheMachineRaisesReachesTheStateTree(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		raise string
	}{
		{
			name:  "raised by an event",
			raise: `{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":42}`,
		},
		{
			name:  "raised by telemetry",
			raise: `{"state":"ATTENTION","dialog_id":42,"temp_nozzle":215}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			printer := standHostile(t)

			printer.text(t, tc.raise)
			printer.awaitDialog(t, "42")

			// The machine left the state a dialog is up in, which is the only
			// thing that says the dialog is gone: the firmware sends no
			// "dialog closed" of its own.
			printer.text(t, `{"state":"IDLE","temp_nozzle":215}`)
			printer.awaitDialog(t, "null")

			t.Logf("observed: %s put %s on the state tree and IDLE removed it", tc.name, dialogIDPath)
		})
	}
}

// TestADialogCarriesWhatItSaysAndNotOnlyItsID is what makes a dialog answerable
// by somebody who is not standing at the machine.
//
// The id names the dialog and says nothing about it, so a caller holding only
// the id can press a button without knowing what it is agreeing to. The code,
// the title, the text and the button labels are what the firmware puts on the
// screen, and they arrive in the same event as the id.
//
// The clearing half is the same rule the id already has, for the same reason: a
// title that outlives its dialog describes a machine that has moved on.
func TestADialogCarriesWhatItSaysAndNotOnlyItsID(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":42,`+
		`"data":{"code":"13501","title":"Filament runout",`+
		`"text":"Insert filament, then press Retry.","buttons":["Retry","Cancel"]}}`)

	printer.awaitDialogTree(t, map[string]string{
		dialogIDPath:      "42",
		dialogCodePath:    `"13501"`,
		dialogTitlePath:   `"Filament runout"`,
		dialogTextPath:    `"Insert filament, then press Retry."`,
		dialogButtonsPath: `["Retry","Cancel"]`,
	})

	t.Logf("observed: one STATE_CHANGED put the whole dialog on the state tree")

	// The machine left the state a dialog is up in, which is the only thing
	// that says the dialog is gone.
	printer.text(t, `{"state":"IDLE","temp_nozzle":215}`)

	printer.awaitDialogTree(t, map[string]string{
		dialogIDPath:      "null",
		dialogCodePath:    "null",
		dialogTitlePath:   "null",
		dialogTextPath:    "null",
		dialogButtonsPath: "null",
	})

	t.Logf("observed: IDLE removed every one of them")
}

// TestAStateChangeThatIsNotADialogLeavesTheOneOnScreenAlone is the guard on the
// case above.
//
// STATE_CHANGED is the event this dialect uses for every state transition, and
// most of them carry no dialog at all. A driver that read every one of them as a
// dialog would blank the title and the buttons of the dialog actually on the
// screen, in a state where the dialog is still up, which is worse than never
// having carried them.
func TestAStateChangeThatIsNotADialogLeavesTheOneOnScreenAlone(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":42,`+
		`"data":{"code":"13501","title":"Filament runout","buttons":["Retry"]}}`)
	printer.awaitDialogTree(t, map[string]string{dialogTitlePath: `"Filament runout"`})

	// Still in ATTENTION, so the dialog is still up, and neither of these says
	// anything about it. The second is the one that needs saying: a data object
	// that names no code is a state change about something else, and a driver
	// that read the object rather than the code would blank the dialog from it.
	printer.text(t, `{"event":"STATE_CHANGED","state":"ATTENTION"}`)
	printer.text(t, `{"event":"STATE_CHANGED","state":"ATTENTION","data":{"job_id":7}}`)

	// Nor does an event of another kind that happens to name a code. A command
	// that failed carries the machine's code for the failure, and reading one as
	// the dialog on the screen puts a caller in front of a Retry button that
	// belongs to something else.
	printer.text(t, `{"event":"FAILED","state":"ATTENTION","data":{"code":"04502"}}`)

	// A frame with something else to say, so the reader has demonstrably passed
	// every frame above before the tree is read. It carries no dialog either: a
	// barrier naming a second dialog would be a case about a dialog that
	// changed, which TestTheWordsOfADialogGoWhenTheIDMovesOnTelemetry is.
	printer.text(t, `{"state":"ATTENTION","temp_nozzle":215}`)
	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "215"})

	if title := printer.dialogValue(dialogTitlePath); title != `"Filament runout"` {
		t.Errorf("a state change carrying no dialog left %s at %s, and the dialog is still on the screen",
			dialogTitlePath, title)
	}

	if code := printer.dialogValue(dialogCodePath); code != `"13501"` {
		t.Errorf("%s reads %s, so a code that belongs to something else was read as the dialog's",
			dialogCodePath, code)
	}
}

// TestASecondDialogDoesNotInheritTheFirstOnesWords is the case a reader is hurt
// by rather than the one a writer thinks of.
//
// A dialog that carries a code and no title replaces one that carried both. A
// tree that left the old title in place would describe the new dialog with the
// old dialog's words, and everything a person checks would agree: the id moved,
// the code moved, and the sentence on the screen is somebody else's.
func TestASecondDialogDoesNotInheritTheFirstOnesWords(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":42,`+
		`"data":{"code":"13501","title":"Filament runout","buttons":["Retry"]}}`)
	printer.awaitDialogTree(t, map[string]string{dialogTitlePath: `"Filament runout"`})

	printer.text(t, `{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":43,`+
		`"data":{"code":"04502"}}`)

	printer.awaitDialogTree(t, map[string]string{
		dialogIDPath:      "43",
		dialogCodePath:    `"04502"`,
		dialogTitlePath:   "null",
		dialogButtonsPath: "null",
	})

	t.Logf("observed: the second dialog kept none of the first one's words")
}

// TestARepeatedDialogSaysNothingNew keeps a machine that repeats a state change
// from writing the same dialog to the tree again.
//
// Every patch reaches every reader watching that printer, so a field re-reported
// unchanged is a wake-up nobody can act on. The id already has this rule, and
// the four fields beside it are read out of the same frame.
func TestARepeatedDialogSaysNothingNew(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	const raise = `{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":42,` +
		`"data":{"code":"13501","title":"Filament runout","buttons":["Retry"]}}`

	printer.text(t, raise)
	printer.awaitDialogTree(t, map[string]string{dialogTitlePath: `"Filament runout"`})

	written := printer.dialogPatchCount(dialogTitlePath)

	for range 4 {
		printer.text(t, raise)
	}

	// A frame with something else to say, so the reader has demonstrably passed
	// every repeat above.
	printer.text(t, `{"state":"ATTENTION","dialog_id":7}`)
	printer.awaitDialogTree(t, map[string]string{dialogIDPath: "7"})

	repeats := printer.dialogPatchCount(dialogTitlePath) - written
	if repeats != 0 {
		t.Errorf("%d further %s patches arrived, and every frame between them carried the same title",
			repeats, dialogTitlePath)
	}

	t.Logf("observed: four repeats of one dialog produced %d further %s patches",
		repeats, dialogTitlePath)
}

// TestADialogRaisedInAStateNoDialogIsUpInLeavesNothingBehind holds the order
// the two halves of a frame are read in.
//
// The dialog is applied first and the state is read afterwards, so a frame that
// names a dialog and reports a state no dialog is up in ends with an empty tree.
// Reading the state first and the dialog afterwards would leave a machine that
// says it is idle showing a question on its screen, and every field a caller
// checks would agree with the other.
func TestADialogRaisedInAStateNoDialogIsUpInLeavesNothingBehind(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"event":"STATE_CHANGED","state":"IDLE","dialog_id":5,`+
		`"data":{"code":"13501","title":"Filament runout"}}`)

	// A frame with something else to say, so the reader has demonstrably passed
	// the frame above before the tree is read.
	printer.text(t, `{"state":"IDLE","temp_nozzle":215}`)
	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "215"})

	for _, path := range []string{dialogIDPath, dialogCodePath, dialogTitlePath} {
		if held := printer.dialogValue(path); held != "" && held != dialogAbsent {
			t.Errorf("%s reads %s on a printer that reported IDLE in the frame that raised the dialog",
				path, held)
		}
	}

	t.Logf("observed: the tree stands at %v", printer.dialogTree())
}

// TestADialogWithNoTextRemovesNothingFromTheTree keeps a field the tree never
// held from being removed.
//
// A removal is a patch, and a patch reaches every reader watching that printer.
// A dialog that carries no text on a tree that holds no text has nothing to say
// about the text, so it says nothing.
func TestADialogWithNoTextRemovesNothingFromTheTree(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	// Two dialogs in a row, neither of which carries text.
	printer.text(t, `{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":42,`+
		`"data":{"code":"13501","title":"Filament runout"}}`)
	printer.awaitDialogTree(t, map[string]string{dialogTitlePath: `"Filament runout"`})

	printer.text(t, `{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":43,`+
		`"data":{"code":"04502","title":"Heater fault"}}`)
	printer.awaitDialogTree(t, map[string]string{dialogTitlePath: `"Heater fault"`})

	if written := printer.dialogPatchCount(dialogTextPath); written != 0 {
		t.Errorf("%d patches arrived at %s, and no dialog ever carried text", written, dialogTextPath)
	}

	t.Logf("observed: two dialogs with no text produced %d patches at %s",
		printer.dialogPatchCount(dialogTextPath), dialogTextPath)
}

// awaitDialogTree waits until every named path has reached its value, and keeps
// what it saw so a later assertion can read the tree as it stands.
func (h *hostile) awaitDialogTree(t *testing.T, want map[string]string) {
	t.Helper()

	deadline := time.After(hostileBudget)

	for {
		outstanding := 0

		for path, value := range want {
			if h.dialogValue(path) != value {
				outstanding++
			}
		}

		if outstanding == 0 {
			return
		}

		select {
		case patch, open := <-h.session.State():
			if !open {
				t.Fatalf("the state channel closed; the dialog tree stands at %v", h.dialogTree())
			}

			if h.dialog == nil {
				h.dialog, h.dialogPatches = map[string]string{}, map[string]int{}
			}

			h.dialog[patch.Path] = string(patch.Value)
			h.dialogPatches[patch.Path]++
		case <-deadline:
			t.Fatalf("the dialog tree never reached %v; it stands at %v", want, h.dialogTree())

			return
		}
	}
}

// dialogValue is what the state tree holds at one path, as this reader has seen
// it. A path nothing has reported is the empty string, which is no value rather
// than a null.
func (h *hostile) dialogValue(path string) string { return h.dialog[path] }

func (h *hostile) dialogTree() map[string]string { return h.dialog }

// dialogPatchCount is how many patches this reader has taken for one path, which
// is what separates a field reported once from one reported on every frame.
func (h *hostile) dialogPatchCount(path string) int { return h.dialogPatches[path] }

// TestARepeatedDialogIsReportedOnce keeps telemetry at 4 Hz from writing a
// patch per frame for a dialog that has not moved.
func TestARepeatedDialogIsReportedOnce(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"state":"ATTENTION","dialog_id":42}`)
	printer.awaitDialog(t, "42")

	for range 4 {
		printer.text(t, `{"state":"ATTENTION","dialog_id":42,"temp_nozzle":215}`)
	}

	// A frame with something else to say, so the reader has demonstrably passed
	// every repeat above before the count is read.
	printer.text(t, `{"state":"ATTENTION","dialog_id":7}`)

	repeats := printer.awaitDialog(t, "7")
	if repeats != 0 {
		t.Errorf("%d dialog patches arrived between 42 and 7, and the id was 42 in every frame "+
			"between them", repeats)
	}

	t.Logf("observed: four repeats of dialog 42 produced %d further patches, and 7 produced one",
		repeats)
}

// TestAFrameThatOmitsTheDialogIDLeavesTheDialogAlone is the regression the
// shipping connector was fixed for against a real machine.
//
// Telemetry on this dialect is sparse, so alternate frames carry the dialog id
// and leave it out. A driver that reads the absence as a closed dialog makes the
// id flap between 42 and nothing at 4 Hz. The shipping connector queued a
// SEND_STATE_INFO on every flap (legacy/connector/xbuddy/connection.go:415), so
// both halves are measured here: the state tree and the wire.
func TestAFrameThatOmitsTheDialogIDLeavesTheDialogAlone(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"state":"ATTENTION","dialog_id":42}`)
	printer.awaitDialog(t, "42")

	// ATTENTION is a state this session had not seen, so the dialect asks the
	// printer what it is printing: the firmware volunteers no JOB_INFO when its
	// own state moves (jobrefresh.go). That request belongs to BUG-004 and not
	// to this case, and taking it off the wire here is what makes the silence
	// below mean what it says. It is also the measurement that separates one
	// request per state change from one per frame.
	printer.expectRefresh(t)

	for range 4 {
		// No state key either. A frame that still said ATTENTION would be carried
		// by the state half of the rule, and the absent id would prove nothing.
		printer.text(t, `{"temp_nozzle":215}`)
		printer.text(t, `{"dialog_id":42}`)
	}

	// A frame with something else to say, so the reader has demonstrably passed
	// every frame above before the count is read.
	printer.text(t, `{"dialog_id":7}`)

	flaps := printer.awaitDialog(t, "7")
	if flaps != 0 {
		t.Errorf("%d dialog patches arrived between 42 and 7, and four of the frames between them left "+
			"dialog_id out rather than saying the dialog closed", flaps)
	}

	printer.expectQuiet(t)

	t.Logf("observed: four frames without dialog_id produced %d further patches and left the wire silent",
		flaps)
}

// TestADialogIDThatNamesNoDialogClosesTheOneOnTheTree is the other half of the
// sparse rule, and the half a reader gets backwards.
//
// The id is a uint32 on the machine, so zero is the firmware reporting no dialog
// and the state stays ATTENTION while it does
// (legacy/connector/xbuddy/connection.go:819-833). A value that is no whole
// number names nothing the verb could answer and is read the same way. A tree
// that kept 42 leaves a caller pressing a button on a machine that has moved on.
func TestADialogIDThatNamesNoDialogClosesTheOneOnTheTree(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		close string
	}{
		{name: "zero", close: `{"state":"ATTENTION","dialog_id":0}`},
		{name: "negative", close: `{"state":"ATTENTION","dialog_id":-1}`},
		{name: "zero as a fraction", close: `{"state":"ATTENTION","dialog_id":0.5}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			printer := standHostile(t)

			printer.text(t, `{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":42,`+
				`"data":{"code":"13501","title":"Filament runout","buttons":["Retry"]}}`)
			printer.awaitDialogTree(t, map[string]string{
				dialogIDPath:    "42",
				dialogTitlePath: `"Filament runout"`,
			})

			printer.text(t, tc.close)

			printer.awaitDialogTree(t, map[string]string{
				dialogIDPath:      "null",
				dialogCodePath:    "null",
				dialogTitlePath:   "null",
				dialogButtonsPath: "null",
			})

			t.Logf("observed: %s in ATTENTION removed the whole dialog subtree", tc.name)
		})
	}
}

// TestTheWordsOfADialogGoWhenTheIDMovesOnTelemetry is the case a caller is hurt
// by rather than the one a writer thinks of.
//
// The words of a dialog arrive with an event and the id repeats on telemetry, so
// a machine that moved from one dialog to another can report the new id before it
// says anything about it. A tree that kept the old title describes dialog 7 with
// dialog 42's sentence, and everything a person checks agrees: the id is current,
// and the question they are answering is not.
func TestTheWordsOfADialogGoWhenTheIDMovesOnTelemetry(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":42,`+
		`"data":{"code":"13501","title":"Filament runout","buttons":["Retry"]}}`)
	printer.awaitDialogTree(t, map[string]string{dialogTitlePath: `"Filament runout"`})

	// Telemetry carries the id and nothing else, and the id is a different one.
	printer.text(t, `{"state":"ATTENTION","dialog_id":7}`)

	printer.awaitDialogTree(t, map[string]string{
		dialogIDPath:      "7",
		dialogCodePath:    "null",
		dialogTitlePath:   "null",
		dialogButtonsPath: "null",
	})

	t.Logf("observed: dialog 7 arrived on telemetry and kept none of dialog 42's words")
}

// TestADialogInAStateNoDialogIsUpInReachesTheTreeAtAll pins the precedence
// between the two halves of one frame.
//
// A frame naming a dialog in a state no dialog is up in disagrees with itself.
// The state is the half to believe, and the dialog is never published: a tree
// that wrote the id and the words and then removed them is four wake-ups and a
// window in which a caller reads a dialog that is not on any screen.
func TestADialogInAStateNoDialogIsUpInReachesTheTreeAtAll(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"event":"STATE_CHANGED","state":"PRINTING","dialog_id":42,`+
		`"data":{"code":"13501","title":"Filament runout","buttons":["Retry"]}}`)

	// A frame with something else to say, so the reader has demonstrably passed
	// the frame above before the tree is read.
	printer.text(t, `{"state":"PRINTING","temp_nozzle":215}`)
	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "215"})

	if published := printer.dialogPathsSeen(); published != 0 {
		t.Errorf("%d dialog paths were published for a frame whose own state says no dialog is up: %v",
			published, printer.dialogTree())
	}

	t.Logf("observed: a dialog named in PRINTING produced %d dialog patches", printer.dialogPathsSeen())
}

// TestADialogIDNoAnswerCouldNameIsNotPublished keeps the tree to ids the verb
// accepts.
//
// The reply bounds dialog_id to a uint32 (verbs.go), so an id above that reaches
// the state tree as a dialog every attempt to answer is refused for, and the
// caller has no way to tell that from a dialog they are simply failing to
// dismiss.
func TestADialogIDNoAnswerCouldNameIsNotPublished(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"state":"ATTENTION","dialog_id":4294967296}`)

	printer.text(t, `{"state":"ATTENTION","temp_nozzle":215}`)
	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "215"})

	if id := printer.dialogValue(dialogIDPath); id != "" {
		t.Errorf("%s was published as %s, and the verb refuses every id above %d",
			dialogIDPath, id, maxUint32)
	}

	t.Logf("observed: an id above the verb's own bound left %s unpublished", dialogIDPath)
}

// TestAnUnreadableDialogIDLeavesTheDialogOnTheTree is the mirror of the sparse
// rule, and the way a driver breaks it while looking careful.
//
// A dialog_id that is no number names nothing, and a machine in ATTENTION with a
// dialog on the screen is not a machine reporting that the dialog closed. The
// shipping connector reads a fraction as the whole number under it
// (legacy/connector/xbuddy/connection.go:822-830) rather than as a frame to act
// on, which is the other half of the case.
func TestAnUnreadableDialogIDLeavesTheDialogOnTheTree(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		frame string
		want  string
	}{
		{name: "no number at all", frame: `{"state":"ATTENTION","dialog_id":"none"}`, want: "42"},
		{name: "a fraction", frame: `{"state":"ATTENTION","dialog_id":43.5}`, want: "43"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			printer := standHostile(t)

			printer.text(t, `{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":42,`+
				`"data":{"code":"13501","title":"Filament runout","buttons":["Retry"]}}`)
			printer.awaitDialogTree(t, map[string]string{dialogIDPath: "42"})

			printer.text(t, tc.frame)

			printer.text(t, `{"state":"ATTENTION","temp_nozzle":215}`)
			printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "215"})

			if id := printer.dialogValue(dialogIDPath); id != tc.want {
				t.Errorf("%s reads %s after %s, want %s", dialogIDPath, id, tc.frame, tc.want)
			}

			t.Logf("observed: %s left %s at %s", tc.name, dialogIDPath, printer.dialogValue(dialogIDPath))
		})
	}
}

// dialogPathsSeen is how many paths under /dialog this reader has been told
// about at all, which is what separates a dialog never published from one
// published and taken back.
func (h *hostile) dialogPathsSeen() int {
	seen := 0

	for path := range h.dialog {
		if strings.HasPrefix(path, "/dialog/") {
			seen++
		}
	}

	return seen
}

// quietWindow is how long the driver is watched for a frame it should not send.
// It is the budget the rest of this suite waits an answer for, cut down: the
// case is measuring silence, and every test pays this one in full.
const quietWindow = 250 * time.Millisecond

// expectQuiet fails if the driver sent the printer anything.
//
// The shipping connector answered a dialog id it thought had changed by queueing
// a SEND_STATE_INFO, which is what turned the flap into a storm on the wire
// (legacy/connector/xbuddy/connection.go:415). This driver reads the dialog off
// the frames it already has and asks the printer for nothing, and this is what
// holds that: no case calling it puts a command in flight, so any frame at all
// is the driver having grown a request of its own.
func (h *hostile) expectQuiet(t *testing.T) {
	t.Helper()

	if err := h.client.SetReadDeadline(time.Now().Add(quietWindow)); err != nil {
		t.Fatalf("arm the printer's read deadline: %v", err)
	}

	_, data, err := h.client.ReadMessage()
	if err == nil {
		t.Fatalf("the driver sent the printer %q, and nothing in this case asked it for anything", data)
	}

	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("read as the printer: %v", err)
	}
}

// expectRefresh takes the one frame the machine's state moving costs. The
// dialect asks what the print is whenever /state changes, because the firmware
// volunteers nothing about the print when it does; see jobrefresh.go.
func (h *hostile) expectRefresh(t *testing.T) {
	t.Helper()

	if err := h.client.SetReadDeadline(time.Now().Add(hostileBudget)); err != nil {
		t.Fatalf("arm the printer's read deadline: %v", err)
	}

	_, data, err := h.client.ReadMessage()
	if err != nil {
		t.Fatalf("read as the printer: %v", err)
	}

	if !strings.Contains(string(data), "SEND_STATE_INFO") {
		t.Fatalf("the driver sent %q after a state change, want a request for the state", data)
	}
}

// awaitDialog waits for the dialog path to reach a value, and reports how many
// other patches for it arrived on the way.
func (h *hostile) awaitDialog(t *testing.T, want string) int {
	t.Helper()

	deadline := time.After(hostileBudget)

	var (
		others int
		seen   []string
	)

	for {
		select {
		case patch, open := <-h.session.State():
			if !open {
				t.Fatalf("the state channel closed before %s reached %s", dialogIDPath, want)
			}

			if patch.Path != dialogIDPath {
				continue
			}

			if string(patch.Value) == want {
				return others
			}

			others++

			seen = append(seen, string(patch.Value))
		case <-deadline:
			t.Fatalf("%s never reached %s; it arrived as %v", dialogIDPath, want, seen)

			return 0
		}
	}
}

// backloggedFrames is how many frames arrive while nothing is reading the state
// channel. It is far above patchBacklog, so every patch queued before the last
// of them has been offered for eviction and the case measures a host that is
// behind rather than one that is briefly slow.
const backloggedFrames = 200

// quietChannel is how long the state channel must say nothing before a drain
// treats it as empty. The session's reader has already finished every frame the
// case sent, so this bounds a queue that is drained rather than a printer that
// is idle.
const quietChannel = 250 * time.Millisecond

// TestADialogReachesAHostThatIsBehind is the rule the four dialog fields need
// and telemetry does not.
//
// Telemetry is a repeating snapshot, so a dropped patch costs a quarter of a
// second. A dialog is written when it moves and repeated by nothing, so a
// dropped one is a machine waiting on a question that no reader is ever told
// about. The host here reads nothing until 400 frames have arrived, which is
// what a reader on a slow link or a browser on a busy tab does.
func TestADialogReachesAHostThatIsBehind(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	const raise = `{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":42,` +
		`"data":{"code":"13501","title":"Filament runout",` +
		`"text":"Insert filament, then press Retry.","buttons":["Retry","Cancel"]}}`

	printer.text(t, raise)

	// The machine stays in ATTENTION and repeats the dialog, which is what one
	// waiting on an operator does. A repeat says nothing new, because
	// reportDialogField writes a patch only when a value moves.
	for range backloggedFrames {
		printer.text(t, `{"state":"ATTENTION","dialog_id":42,"temp_nozzle":215,`+
			`"temp_bed":60,"axis_x":10,"axis_y":20,"axis_z":30}`)
		printer.text(t, raise)
	}

	// Every drop happens while the flood is arriving, because nothing reads the
	// state channel until the line below. Waiting for the counter here is what
	// says the case measures a host that is behind rather than one that is
	// briefly slow.
	dropped := printer.awaitDropped(t)

	t.Logf("the host was behind by %d dropped patches when the dialog was raised", dropped)

	printer.awaitDialogTree(t, map[string]string{
		dialogIDPath:      "42",
		dialogCodePath:    `"13501"`,
		dialogTitlePath:   `"Filament runout"`,
		dialogTextPath:    `"Insert filament, then press Retry."`,
		dialogButtonsPath: `["Retry","Cancel"]`,
	})

	// The session records a field as published when it writes the patch, and a
	// record that says published for a patch the host never took is how a
	// dialog goes missing with nothing reporting it.
	printer.session.mu.Lock()
	held := maps.Clone(printer.session.dialogFields)
	printer.session.mu.Unlock()

	for path, value := range held {
		if got := printer.dialogValue(path); got != value {
			t.Errorf("the session holds %s at %s and the host was told %s", path, value, got)
		}
	}

	t.Logf("observed: the whole dialog reached a host that was behind by %d dropped patches", dropped)
}

// TestOneDialogFieldHoldsOneWaitingValue bounds what the dialog's reserved path
// costs.
//
// A patch nothing may evict is a way to exhaust memory unless something bounds
// it, and what bounds this one is structural: one slot per dialog field per
// printer. A machine raising a new dialog on every frame replaces what is
// waiting rather than adding to it, so the host is told the dialog that is on
// the screen and never the 199 before it.
func TestOneDialogFieldHoldsOneWaitingValue(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	// A command on the wire, answered after the flood. The reader serves one
	// frame at a time in one goroutine, so an outcome that has arrived is proof
	// every dialog below it was dealt with, and the drain therefore reads what
	// the slots held rather than racing the reader.
	answered := printer.start(t)
	command := printer.takeFrame(t)

	for id := range backloggedFrames {
		printer.text(t, fmt.Sprintf(`{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":%d,`+
			`"data":{"code":"135%02d","title":"Filament runout","text":"Insert filament.",`+
			`"buttons":["Retry","Cancel"]}}`, id, id))
	}

	// The answer carries no state, so it says nothing about the dialog the
	// machine is still waiting on.
	printer.text(t, fmt.Sprintf(`{"event":"FINISHED","command_id":%d}`, command))

	if _, err := printer.await(t, answered); err != nil {
		t.Fatalf("the command offered before the flood never settled: %v", err)
	}

	carried := printer.drainDialog(t)

	// One value per field, and one more for the patch the pump had already
	// taken out of a slot when the flood started.
	if ceiling := len(dialogPaths) + 1; carried > ceiling {
		t.Errorf("%d dialogs raised at a host reading nothing produced %d patches, and the reserved "+
			"path holds %d", backloggedFrames, carried, ceiling)
	}

	newest := strconv.Itoa(backloggedFrames - 1)
	if got := printer.dialogValue(dialogIDPath); got != newest {
		t.Errorf("%s reads %s and the dialog on the screen is %s", dialogIDPath, got, newest)
	}

	t.Logf("observed: %d dialogs raised at a host reading nothing reached it as %d patches, ending at "+
		"dialog %s", backloggedFrames, carried, newest)
}

// drainDialog reads the state channel until it goes quiet, keeping what it saw
// and reporting how many of the patches carried a dialog path.
func (h *hostile) drainDialog(t *testing.T) int {
	t.Helper()

	if h.dialog == nil {
		h.dialog, h.dialogPatches = map[string]string{}, map[string]int{}
	}

	carried := 0
	deadline := time.After(hostileBudget)

	for {
		select {
		case patch, open := <-h.session.State():
			if !open {
				t.Fatalf("the state channel closed; the dialog tree stands at %v", h.dialogTree())
			}

			h.dialog[patch.Path] = string(patch.Value)
			h.dialogPatches[patch.Path]++

			// The membership test is spelled out rather than taken from
			// reservedPath, because a case that asks the code under test which
			// paths are reserved passes whatever answer it gives.
			if slices.Contains(dialogPaths, patch.Path) {
				carried++
			}
		case <-time.After(quietChannel):
			return carried
		case <-deadline:
			t.Fatalf("the state channel never went quiet; the dialog tree stands at %v", h.dialogTree())

			return carried
		}
	}
}

// A dialog with text and no code is still a dialog. The HT90 raises "Door
// Opened!" that way, with no code and no buttons, and a person has to see why
// the printer stopped.
func TestADialogWithTextAndNoCodeIsShown(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"event":"STATE_CHANGED","state":"ATTENTION","dialog_id":1794231552,`+
		`"data":{"text":"Door Opened!"}}`)
	printer.awaitDialogTree(t, map[string]string{
		dialogIDPath:   "1794231552",
		dialogTextPath: `"Door Opened!"`,
	})
}
