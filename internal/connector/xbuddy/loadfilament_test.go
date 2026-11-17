package xbuddy

import (
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// renderLoadFilament renders LOAD_FILAMENT through the wire table with the given
// arguments, the same way the session renders it.
func renderLoadFilament(values map[string]string) (string, error) {
	form := wireTable[VerbLoadFilament]

	return form.gcode(arguments{verb: VerbLoadFilament, declared: form.args, values: values})
}

// The firmware reads S"<name>" on M701 and loads that filament without asking
// when it knows the name. A caller who picks PETG must see the printer receive
// PETG.
func TestLoadFilamentSendsTheFilamentName(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		values map[string]string
		want   string
	}{
		"a name":           {values: map[string]string{"filament": "PETG"}, want: `M701 S"PETG" W2`},
		"a custom name":    {values: map[string]string{"filament": "My_PLA-"}, want: `M701 S"My_PLA-" W2`},
		"no name":          {values: nil, want: "M701 W2"},
		"an empty name":    {values: map[string]string{"filament": ""}, want: "M701 W2"},
		"seven characters": {values: map[string]string{"filament": "ABCDEFG"}, want: `M701 S"ABCDEFG" W2`},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := renderLoadFilament(tc.values)
			if err != nil {
				t.Fatalf("render: %v", err)
			}

			if got != tc.want {
				t.Errorf("LOAD_FILAMENT renders %q, want %q", got, tc.want)
			}
		})
	}

	if dropped := wireTable[VerbLoadFilament].dropped; len(dropped) != 0 {
		t.Errorf("LOAD_FILAMENT reports %v as not sent, and the printer receives the name", dropped)
	}
}

// A name travels inside a gcode line. A quote, a space, a semicolon or a line
// break would end the name early or start a second command, so the driver
// refuses every character the firmware does not allow in a filament name.
func TestLoadFilamentRefusesANameTheFirmwareCannotRead(t *testing.T) {
	t.Parallel()

	names := []string{
		`PE"TG`, "PLA\r\nM502", "\n", "\r", "\t", `\`, "PL A", "PLA;x", "PLÁ", "PLA\x00",
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := renderLoadFilament(map[string]string{"filament": name})
			if err == nil {
				t.Fatalf("the name %q rendered as %q, and it must be refused", name, got)
			}

			var refusal *fault.Fault
			if !errors.As(err, &refusal) || refusal.Code() != connectorapi.ArgumentMalformed.Code() {
				t.Fatalf("the name %q was refused with %v, want %s", name, err, connectorapi.ArgumentMalformed.Code())
			}

			params := map[string]string{}
			for _, param := range refusal.Params() {
				params[param.Key] = param.Value.String()
			}

			if params["allowed_characters"] != filamentNameAllowed || params["limit_characters"] != "7" {
				t.Errorf("the refusal of %q carries %v, want the allowed characters and a length of 7", name, params)
			}

			if _, carries := params["kind"]; carries {
				t.Errorf("the refusal of %q names a kind, and the value is text as the argument declares", name)
			}
		})
	}
}

// The firmware holds a filament name in eight bytes with the terminating zero,
// and it reads a longer name as no name at all. The host refuses such a name
// before the driver sees it.
func TestTheFilamentNameIsBoundedByTheFirmwareBuffer(t *testing.T) {
	t.Parallel()

	value := published()[connectorapi.ArgumentKey(VerbLoadFilament, "filament")]

	if value.Kind != connectorapi.KindString || !value.HasLimit || value.Limit != maxFilamentNameLength {
		t.Errorf("the filament argument is %+v, want a string of at most %d bytes", value, maxFilamentNameLength)
	}

	if maxFilamentNameLength != 7 {
		t.Errorf("the name limit is %d, and the firmware keeps seven bytes of a name", maxFilamentNameLength)
	}
}
