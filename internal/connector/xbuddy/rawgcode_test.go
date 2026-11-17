package xbuddy

import (
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// render puts one payload through the verb's own template, which is where every
// refusal the descriptor cannot state lives.
func render(t *testing.T, verb connectorapi.Capability, payload string) (string, error) {
	t.Helper()

	form, declared := wireTable[verb]
	if !declared {
		t.Fatalf("%s has no wire form", verb)
	}

	return form.gcode(arguments{
		verb: verb, declared: form.args, values: map[string]string{"gcode": payload},
	})
}

// The payloads that reach the wire, and the exact bytes they reach it as. A
// template that rewrote its input would change an M117 message, so this pins
// that it does not.
func TestAnAllowedPayloadReachesTheWireUnchanged(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"a bare word":                    "G28",
		"a word with parameters":         "M104 S215",
		"a decimal word":                 "M862.3 P \"MK4S\"",
		"a tool change":                  "T1",
		"a message with punctuation":     "M117 Ready: 2/3 (~4m)",
		"an EEPROM save":                 "M500",
		"a factory reset":                "M502",
		"an emergency stop":              "M112",
		"a stepper current":              "M906 Z900",
		"a comment after the word":       "M104 S215 ; warm up",
		"a parameter glued to the word":  "M104S215",
		"a decimal the firmware ignores": "M862. P1",
		"surrounding spaces trimmed":     "  G28 X  ",
	}

	want := map[string]string{
		"surrounding spaces trimmed": "G28 X",
	}

	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			code, err := render(t, VerbSendGCode, payload)
			if err != nil {
				t.Fatalf("%q was refused: %v", payload, err)
			}

			expected, rewritten := want[name]
			if !rewritten {
				expected = payload
			}

			if code != expected {
				t.Errorf("%q reaches the wire as %q, and this template must not rewrite its input", payload, code)
			}
		})
	}
}

// Every refusal, with the code a caller branches on. A refusal that carried no
// registered code would reach a client as an internal error and tell them
// nothing they could act on.
func TestARefusedPayloadNeverReachesTheWire(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload string
		code    fault.Code
	}{
		// Rule 5: printable ASCII, which is what stops a second command being
		// smuggled behind a line break.
		{"a smuggled second command", "M104 S200\r\nM502", connectorapi.GCodeMalformed.Code()},
		{"a bare newline", "M104 S200\nM997", connectorapi.GCodeMalformed.Code()},
		{"a tab", "M104\tS200", connectorapi.GCodeMalformed.Code()},
		{"a null byte", "M104 S200\x00", connectorapi.GCodeMalformed.Code()},
		{"a delete byte", "M104\x7f", connectorapi.GCodeMalformed.Code()},
		{"a non-ascii message", "M117 čau", connectorapi.GCodeMalformed.Code()},

		// Rule 4: one word, or nothing. Refusing what it cannot read is what
		// makes the word extraction total.
		{"an empty payload", "", connectorapi.GCodeMalformed.Code()},
		{"only spaces", "   ", connectorapi.GCodeMalformed.Code()},
		{"a comment", "; M502", connectorapi.GCodeMalformed.Code()},
		{"a line number in front", "N5 M502", connectorapi.GCodeMalformed.Code()},
		{"a space inside the word", "M 502", connectorapi.GCodeMalformed.Code()},
		{"a letter with no number", "M", connectorapi.GCodeMalformed.Code()},
		{"a letter followed by a letter", "MX", connectorapi.GCodeMalformed.Code()},
		{"a letter outside the set", "S200", connectorapi.GCodeMalformed.Code()},
		{"a bare number", "104", connectorapi.GCodeMalformed.Code()},

		// Rule 6: the words no caller sends.
		{"a firmware flash", "M997", connectorapi.GCodeNotAllowed.Code()},
		{"an sd write", "M28 out.gco", connectorapi.GCodeNotAllowed.Code()},
		{"an sd write stop", "M29", connectorapi.GCodeNotAllowed.Code()},
		{"an sd delete", "M30 /usb/x.bgcode", connectorapi.GCodeNotAllowed.Code()},
		{"a memory write debug code", "D3 Ax0 C1 X01", connectorapi.GCodeNotAllowed.Code()},
		{"a flash debug code", "D5", connectorapi.GCodeNotAllowed.Code()},
		{"the lowest debug code", "D-1", connectorapi.GCodeNotAllowed.Code()},
		{"a bare debug letter", "D", connectorapi.GCodeNotAllowed.Code()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, verb := range []connectorapi.Capability{VerbSendGCode, VerbForceGCode} {
				code, err := render(t, verb, tc.payload)
				if err == nil {
					t.Fatalf("%s accepted %q and put %q on the wire", verb, tc.payload, code)
				}

				if !fault.HasCode(err, tc.code) {
					t.Errorf("%s refused %q with %v, and the caller branches on %s",
						verb, tc.payload, err, tc.code)
				}
			}
		})
	}
}

// A refused word must have no spelling that reaches the wire. This is the half
// of rule 4 that matters: the parser exists so that the refusal set cannot be
// walked past, so every respelling somebody would try is asserted here rather
// than left to the parser being obviously right.
func TestARefusedWordCannotBeReachedByRespelling(t *testing.T) {
	t.Parallel()

	spellings := []string{
		"M997",
		"m997",
		"M997.0",
		"M997 ",
		" M997",
		"  m997  ",
		"M997 R",
		"M997;now",
		"M28",
		"m28.1",
		"D3",
		"d3",
		"D3.5",
	}

	for _, payload := range spellings {
		t.Run(payload, func(t *testing.T) {
			t.Parallel()

			code, err := render(t, VerbSendGCode, payload)
			if err == nil {
				t.Fatalf("%q was accepted and reached the wire as %q", payload, code)
			}

			if !fault.HasCode(err, connectorapi.GCodeNotAllowed.Code()) {
				t.Errorf("%q was refused as unreadable rather than as a refused word: %v", payload, err)
			}
		})
	}
}

// The two verbs differ in exactly one byte on the wire, and that byte is the
// whole reason there are two of them.
func TestTheTwoRawVerbsTravelUnderDifferentFrames(t *testing.T) {
	t.Parallel()

	send, ok := wireTable[VerbSendGCode]
	if !ok {
		t.Fatal("send_gcode has no wire form")
	}

	forced, ok := wireTable[VerbForceGCode]
	if !ok {
		t.Fatal("force_gcode has no wire form")
	}

	if send.frame != frameGCode {
		t.Errorf("send_gcode travels as %q and the queued frame is %q", string(send.frame), string(frameGCode))
	}

	if forced.frame != frameForced {
		t.Errorf("force_gcode travels as %q and the out-of-band frame is %q",
			string(forced.frame), string(frameForced))
	}

	frame, err := encodeCommand(1, forced, arguments{
		verb: VerbForceGCode, declared: forced.args, values: map[string]string{"gcode": "M112"},
	}, toolBaseUnknown)
	if err != nil {
		t.Fatalf("encoding a forced payload: %v", err)
	}

	if want := "F00000001M112"; string(frame) != want {
		t.Errorf("a forced M112 encodes as %q and the framing is %q", string(frame), want)
	}
}

// The descriptor half. The bound and the requirement are what the host checks
// before the driver is reached, so a verb that declared neither would hand this
// template an unbounded payload.
func TestTheRawPayloadArgumentIsBoundedAndRequired(t *testing.T) {
	t.Parallel()

	set := baseline()

	for _, verb := range []connectorapi.Capability{VerbSendGCode, VerbForceGCode} {
		if !set.Has(verb) {
			t.Errorf("%s is not in the driver baseline", verb)

			continue
		}

		key := connectorapi.ArgumentKey(verb, "gcode")

		value, declared := set[key]
		if !declared || !value.Flag {
			t.Errorf("%s declares no payload argument", verb)

			continue
		}

		if value.Kind != connectorapi.KindString {
			t.Errorf("%s carries kind %s and a payload is text", key, value.Kind)
		}

		if !value.Required {
			t.Errorf("%s is optional, so a command with no payload would reach the driver", key)
		}

		if !value.HasLimit || value.Limit != maxRawGCode {
			t.Errorf("%s declares limit %v (set %v) and the bound is %d",
				key, value.Limit, value.HasLimit, maxRawGCode)
		}

		if len(value.Supported) != 0 {
			t.Errorf("%s carries a Supported set in the baseline, which is a compile-time ceiling on "+
				"what a customer may send (ADR-20260825-z65 rule 3)", key)
		}
	}

	// The host is what enforces the bound, so this checks the bound is
	// reachable through Validate rather than only declared.
	long := strings.Repeat("M117 ", maxRawGCode)

	_, err := set.Validate(connectorapi.Command{
		Verb: VerbSendGCode,
		Args: map[string]string{"gcode": long},
	})
	if err == nil {
		t.Errorf("a %d-byte payload passed host validation against a %d-byte limit", len(long), maxRawGCode)
	}
}

// The refusal set is a floor and not a fence, and that is only true while the
// things it does not stop really do pass. A denylist that quietly grew into an
// allowlist would be a different decision with the same name.
func TestTheRefusalSetStopsNothingItDoesNotName(t *testing.T) {
	t.Parallel()

	// Named in ADR-20260825-z65 rule 21 as what this does not stop.
	notStopped := []string{"M104 S500", "G1 Z-100 F600", "M502", "M500", "M23 /usb/x.bgcode", "M24"}

	for _, payload := range notStopped {
		t.Run(payload, func(t *testing.T) {
			t.Parallel()

			if _, err := render(t, VerbSendGCode, payload); err != nil {
				t.Errorf("%q was refused: %v. ADR-20260825-z65 rule 21 says this is not stopped, so either the "+
					"refusal set grew or the record is out of date", payload, err)
			}
		})
	}
}
