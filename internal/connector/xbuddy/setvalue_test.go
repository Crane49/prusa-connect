package xbuddy

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// setValueFrame is one setter as the firmware receives it: the frame this port
// wrote, and the kwargs names read back out of it.
type setValueFrame struct {
	verb  connectorapi.Capability
	names []string
}

// Twelve setters share the one SET_VALUE frame, so the argument names are the
// whole of what tells the firmware which setter was meant (verbs.go states the
// invariant). Two of them arriving under one set of names is a machine carrying
// out a command nobody sent.
//
// The frames are built through encodeCommand and read back off the bytes, so
// this measures what leaves the socket and not what the table says.
func TestNoTwoSetValueVerbsReachTheWireUnderOneSetOfNames(t *testing.T) {
	t.Parallel()

	frames := setValueFrames(t)

	if len(frames) < 2 {
		t.Fatalf("%d verbs travel as %s, so there is no pair to tell apart and this test measures "+
			"nothing", len(frames), setValueCommand)
	}

	seen := make(map[string]connectorapi.Capability, len(frames))

	for _, frame := range frames {
		key := strings.Join(frame.names, ",")

		if other, clash := seen[key]; clash {
			t.Errorf("%s and %s both reach the printer as %s with the names %s, so the firmware "+
				"cannot tell which was meant", other, frame.verb, setValueCommand, key)

			continue
		}

		seen[key] = frame.verb
	}

	t.Logf("%d setters travel as %s, each under its own names", len(frames), setValueCommand)
}

// The names on the wire are fixed only while every one of them is required. An
// optional argument is one a caller may leave out, and a setter that arrives
// short of a name is a setter the firmware reads as another one.
//
// It is also what keeps a profile from producing the same collision:
// Set.Intersect drops a verb whose required argument a tier did not carry, so
// the narrowing that would shorten the names removes the verb instead.
func TestEverySetValueArgumentIsRequired(t *testing.T) {
	t.Parallel()

	checked := 0

	for _, key := range slices.Sorted(slices.Values(setValueVerbs())) {
		form := wireTable[key]

		if len(form.args) == 0 {
			t.Errorf("%s travels as %s and declares no argument, so it is the empty frame every "+
				"other setter is told apart from", key, setValueCommand)

			continue
		}

		for _, name := range slices.Sorted(slices.Values(argumentNames(form))) {
			checked++

			if !form.args[name].Required {
				t.Errorf("%s declares %q as optional, so a caller who leaves it out sends a %s "+
					"frame with a shorter set of names than this verb was told apart by",
					key, name, setValueCommand)
			}
		}
	}

	if checked == 0 {
		t.Fatal("no argument was read, so this test passed over an empty table")
	}

	t.Logf("%d arguments across the %s setters, every one required", checked, setValueCommand)
}

// setValueFrames encodes one frame per SET_VALUE verb and reads the kwargs
// names back off the bytes.
func setValueFrames(t *testing.T) []setValueFrame {
	t.Helper()

	verbs := setValueVerbs()
	slices.Sort(verbs)

	out := make([]setValueFrame, 0, len(verbs))

	for _, key := range verbs {
		form := wireTable[key]

		frame, err := encodeCommand(1, form, arguments{
			verb: key, declared: form.args, values: sampleValues(t, key, form),
		}, toolBaseOne)
		if err != nil {
			t.Fatalf("encoding a %s frame: %v", key, err)
		}

		out = append(out, setValueFrame{verb: key, names: kwargsNames(t, key, frame)})
	}

	return out
}

// setValueVerbs is every verb the wire table sends under the shared frame.
func setValueVerbs() []connectorapi.Capability {
	var out []connectorapi.Capability

	for key, form := range wireTable {
		if form.command == setValueCommand {
			out = append(out, key)
		}
	}

	return out
}

// argumentNames is one verb's declared argument names.
func argumentNames(form verb) []string {
	out := make([]string, 0, len(form.args))
	for name := range form.args {
		out = append(out, name)
	}

	return out
}

// sampleValues is one legal value per declared argument. The value does not
// matter: what reaches the firmware as an identifier is the name.
func sampleValues(t *testing.T, key connectorapi.Capability, form verb) map[string]string {
	t.Helper()

	out := make(map[string]string, len(form.args))

	for name, declared := range form.args {
		switch declared.Kind {
		case connectorapi.KindBoolean:
			out[name] = "true"
		case connectorapi.KindInteger, connectorapi.KindNumber:
			out[name] = "1"
		case connectorapi.KindString:
			out[name] = "a"
		case connectorapi.KindOpaque:
			out[name] = "{}"
		case connectorapi.KindFlag:
			t.Fatalf("%s declares argument %q as a flag, which carries no value", key, name)
		default:
			t.Fatalf("%s declares argument %q with kind %d, which has no wire form",
				key, name, declared.Kind)
		}
	}

	return out
}

// kwargsNames reads the argument names out of an encoded frame, past the
// nine-byte header the firmware strips.
func kwargsNames(t *testing.T, key connectorapi.Capability, frame []byte) []string {
	t.Helper()

	if len(frame) <= headerLength {
		t.Fatalf("the %s frame is %d bytes, which is no payload at all", key, len(frame))
	}

	var body struct {
		Command string                     `json:"command"`
		Kwargs  map[string]json.RawMessage `json:"kwargs"`
	}

	if err := json.Unmarshal(frame[headerLength:], &body); err != nil {
		t.Fatalf("reading the %s frame back: %v", key, err)
	}

	if body.Command != setValueCommand {
		t.Fatalf("%s reached the wire as %q and not %s", key, body.Command, setValueCommand)
	}

	out := make([]string, 0, len(body.Kwargs))
	for name := range body.Kwargs {
		out = append(out, name)
	}

	slices.Sort(out)

	return out
}
