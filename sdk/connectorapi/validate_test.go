package connectorapi_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// descriptor is one printer that can start a print and set a chamber fan.
func descriptor() connectorapi.Set {
	return connectorapi.Set{
		verbPrint: {Flag: true},
		argPath:   {Flag: true, Kind: connectorapi.KindString, Required: true, Limit: 8, HasLimit: true},
		"core.command.start_print#storage": {
			Flag: true, Kind: connectorapi.KindString, Supported: []string{"sd", "usb"}, Default: "usb",
		},
		"core.command.set_chamber_fan_target": {Flag: true},
		"core.command.set_chamber_fan_target#chamber.fan_pwm_target": {
			Flag: true, Kind: connectorapi.KindInteger, Limit: 100, HasLimit: true,
		},
	}
}

func TestValidateRefusesWhatTheDescriptorDoesNotPermit(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		cmd  connectorapi.Command
		want fault.Code
	}{
		{
			name: "a verb the printer does not have",
			cmd:  connectorapi.Command{Verb: verbPause},
			want: connectorapi.CapabilityNotSupported.Code(),
		},
		{
			name: "an argument the verb does not declare",
			cmd: connectorapi.Command{
				Verb: verbPrint,
				Args: map[string]string{"path": "a.gcode", "speed": "200"},
			},
			want: connectorapi.ArgumentUnknown.Code(),
		},
		{
			name: "a required argument with no default, left out",
			cmd:  connectorapi.Command{Verb: verbPrint},
			want: connectorapi.ArgumentMissing.Code(),
		},
		{
			name: "a value outside the supported set",
			cmd: connectorapi.Command{
				Verb: verbPrint,
				Args: map[string]string{"path": "a.gcode", "storage": "nfs"},
			},
			want: connectorapi.ArgumentNotSupported.Code(),
		},
		{
			name: "text where a whole number is declared",
			cmd: connectorapi.Command{
				Verb: "core.command.set_chamber_fan_target",
				Args: map[string]string{"chamber.fan_pwm_target": "fast"},
			},
			want: connectorapi.ArgumentMalformed.Code(),
		},
		{
			name: "a number above the limit",
			cmd: connectorapi.Command{
				Verb: "core.command.set_chamber_fan_target",
				Args: map[string]string{"chamber.fan_pwm_target": "101"},
			},
			want: connectorapi.ArgumentOutOfRange.Code(),
		},
		{
			name: "a string longer than the limit",
			cmd: connectorapi.Command{
				Verb: verbPrint,
				Args: map[string]string{"path": "much-too-long.gcode"},
			},
			want: connectorapi.ArgumentOutOfRange.Code(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := descriptor().Validate(tc.cmd)
			if err == nil {
				t.Fatal("validation accepted the command")
			}

			if !fault.HasCode(err, tc.want) {
				t.Errorf("code = %v, want %v", err, tc.want)
			}
		})
	}
}

// driver.go tells a driver its arguments have already been checked and that it
// never re-checks one it declared. A value that walks past the bounds therefore
// reaches a heater setpoint with nobody left to stop it. NaN is the worst of
// them: every comparison against it is false, so it passed an upper-bound test
// by not being greater than anything.
func TestValidateRefusesAValueNoComparisonCatches(t *testing.T) {
	t.Parallel()

	const verb = connectorapi.Capability("core.command.set_temp")

	set := connectorapi.Set{
		verb: {Flag: true},
		connectorapi.ArgumentKey(verb, "target"): {
			Flag: true, Kind: connectorapi.KindNumber, Limit: 300, HasLimit: true,
		},
		connectorapi.ArgumentKey(verb, "bounded"): {
			Flag: true, Kind: connectorapi.KindNumber, Limit: 300, HasLimit: true, Min: 0, HasMin: true,
		},
	}

	for _, tc := range []struct {
		name     string
		argument string
		value    string
		want     fault.Code
	}{
		{"above the limit", "target", "301", connectorapi.ArgumentOutOfRange.Code()},
		{"far above the limit", "target", "1e300", connectorapi.ArgumentOutOfRange.Code()},
		{"not a number at all", "target", "NaN", connectorapi.ArgumentMalformed.Code()},
		{"negative infinity", "target", "-Inf", connectorapi.ArgumentMalformed.Code()},
		{"positive infinity", "target", "+Inf", connectorapi.ArgumentMalformed.Code()},
		{"far below zero", "target", "-999999", connectorapi.ArgumentOutOfRange.Code()},
		{"below a declared lower bound", "bounded", "-1", connectorapi.ArgumentOutOfRange.Code()},
		// strconv.ParseFloat reads Go's floating-point literal syntax, which is
		// wider than anything a device speaks. Both of these parsed, passed the
		// limit and reached the driver as the original text, so the driver was
		// handed "0x1p3" for an argument the host had told it was a checked
		// number. KindInteger never had it: ParseInt in base 10 permits neither.
		{"a hex float, which is Go syntax and not a wire number", "target", "0x1p3",
			connectorapi.ArgumentMalformed.Code()},
		{"digit separators, which are Go syntax too", "target", "1_0.5",
			connectorapi.ArgumentMalformed.Code()},
		{"a hex float below the limit is still not a number", "bounded", "0x1p3",
			connectorapi.ArgumentMalformed.Code()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := set.Validate(connectorapi.Command{
				Verb: verb, Args: map[string]string{tc.argument: tc.value},
			})
			if !fault.HasCode(err, tc.want) {
				t.Errorf("Validate(%s = %q) = %v, want %v", tc.argument, tc.value, err, tc.want)
			}
		})
	}

	// The values the descriptor really does permit still get through, or the
	// refusal has taken the working path with it. 1e2 is here because refusing
	// Go's spellings must not refuse the exponent JSON and every device speak.
	for _, value := range []string{"0", "300", "-300", "42.5", "1e2", "-1.5e-2", "2.5E1"} {
		if _, err := set.Validate(connectorapi.Command{
			Verb: verb, Args: map[string]string{"target": value},
		}); err != nil {
			t.Errorf("Validate(target = %q) = %v, want it accepted", value, err)
		}
	}
}

// Kind's own documentation says a boolean is "true" or "false". Accepting "T",
// "1" and "TRUE" and forwarding them verbatim hands a driver four spellings to
// compare against when the contract named one.
func TestValidateRefusesABooleanSpellingTheKindDoesNotDefine(t *testing.T) {
	t.Parallel()

	const verb = connectorapi.Capability("core.command.set_flag")

	set := connectorapi.Set{
		verb:                                 {Flag: true},
		connectorapi.ArgumentKey(verb, "on"): {Flag: true, Kind: connectorapi.KindBoolean},
	}

	for _, value := range []string{"T", "1", "0", "TRUE", "False", "yes"} {
		if _, err := set.Validate(connectorapi.Command{
			Verb: verb, Args: map[string]string{"on": value},
		}); !fault.HasCode(err, connectorapi.ArgumentMalformed.Code()) {
			t.Errorf("Validate(on = %q) = %v, want it refused as malformed", value, err)
		}
	}

	for _, value := range []string{"true", "false"} {
		if _, err := set.Validate(connectorapi.Command{
			Verb: verb, Args: map[string]string{"on": value},
		}); err != nil {
			t.Errorf("Validate(on = %q) = %v, want it accepted", value, err)
		}
	}
}

// A declared default is injected without the caller ever naming it, so an
// unchecked default is the one value that reaches a driver by declaration alone.
// A descriptor is data and can carry a default its own limit refuses.
func TestValidateChecksTheDefaultItInjects(t *testing.T) {
	t.Parallel()

	const verb = connectorapi.Capability("core.command.set_temp")

	for _, tc := range []struct {
		name  string
		value connectorapi.Value
		want  fault.Code
	}{
		{
			name: "a default above its own limit",
			value: connectorapi.Value{
				Flag: true, Kind: connectorapi.KindInteger, Default: "99999", Limit: 10, HasLimit: true,
			},
			want: connectorapi.ArgumentOutOfRange.Code(),
		},
		{
			name:  "a default unreadable as its own kind",
			value: connectorapi.Value{Flag: true, Kind: connectorapi.KindInteger, Default: "not-a-number"},
			want:  connectorapi.ArgumentMalformed.Code(),
		},
		{
			name: "a default below its own lower bound",
			value: connectorapi.Value{
				Flag: true, Kind: connectorapi.KindNumber, Default: "-5", Min: 0, HasMin: true,
			},
			want: connectorapi.ArgumentOutOfRange.Code(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			set := connectorapi.Set{
				verb:                                     {Flag: true},
				connectorapi.ArgumentKey(verb, "target"): tc.value,
			}

			checked, err := set.Validate(connectorapi.Command{Verb: verb})
			if !fault.HasCode(err, tc.want) {
				t.Errorf("Validate = %+v, %v, want %v", checked, err, tc.want)
			}

			if _, injected := checked.Args["target"]; injected {
				t.Errorf("the default was injected anyway, as %q", checked.Args["target"])
			}
		})
	}
}

// The SDK offers Capability.Verb() as the obvious dispatch helper, so a driver
// writing switch cmd.Verb.Verb() sends an argument key straight to its verb, with
// no arguments and no required-argument check. The set carries the key, so
// asking only whether the set has it is not asking whether it is a verb.
func TestValidateRefusesAnArgumentKeyInvokedAsAVerb(t *testing.T) {
	t.Parallel()

	checked, err := descriptor().Validate(connectorapi.Command{Verb: argPath})
	if !fault.HasCode(err, connectorapi.CapabilityNotSupported.Code()) {
		t.Fatalf("Validate(%s) = %+v, %v, want the capability refused", argPath, checked, err)
	}

	if checked.Verb.Verb() == verbPrint {
		t.Errorf("the command came back dispatchable as %s", verbPrint)
	}
}

func TestValidateFillsTheDeclaredDefault(t *testing.T) {
	t.Parallel()

	checked, err := descriptor().Validate(connectorapi.Command{
		Verb: verbPrint,
		Args: map[string]string{"path": "a.gcode"},
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if got := checked.Args["storage"]; got != "usb" {
		t.Errorf("storage = %q, want the declared default %q", got, "usb")
	}
}

func TestValidateDoesNotMutateTheCallersArguments(t *testing.T) {
	t.Parallel()

	args := map[string]string{"path": "a.gcode"}

	if _, err := descriptor().Validate(connectorapi.Command{Verb: verbPrint, Args: args}); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if _, filled := args["storage"]; filled {
		t.Error("validation wrote the default back into the caller's map")
	}
}

// An error a caller cannot act on is a bug report. The supported set and the
// limit belong in the payload, not in a log line the caller cannot read.
func TestValidateNamesWhatWouldHaveBeenAccepted(t *testing.T) {
	t.Parallel()

	_, err := descriptor().Validate(connectorapi.Command{
		Verb: verbPrint,
		Args: map[string]string{"path": "a.gcode", "storage": "nfs"},
	})

	var f *fault.Fault
	if !errors.As(err, &f) {
		t.Fatalf("error is not a fault: %v", err)
	}

	params := map[string]string{}
	for _, p := range f.Params() {
		params[p.Key] = p.Value.String()
	}

	// A JSON array and not a joined list: a value containing the separator has
	// to read back whole, which is the same ambiguity Set.Digest avoids.
	if params["supported"] != `["sd","usb"]` {
		t.Errorf("supported = %q, want the two values the printer accepts as a JSON array",
			params["supported"])
	}

	var supported []string
	if decodeErr := json.Unmarshal([]byte(params["supported"]), &supported); decodeErr != nil {
		t.Fatalf("the supported payload is not readable by a client: %v", decodeErr)
	}

	if !slices.Equal(supported, []string{"sd", "usb"}) {
		t.Errorf("a client reads back %v", supported)
	}

	if params["argument"] != "storage" {
		t.Errorf("argument = %q, want the one that was refused", params["argument"])
	}
}

// A capability turned off by a lower tier is not a capability. The set still
// carries the key so a diagnostic can say it was subtracted.
func TestValidateRefusesAVerbWhoseFlagIsOff(t *testing.T) {
	t.Parallel()

	set := descriptor()
	set[verbPrint] = connectorapi.Value{Flag: false}

	if _, err := set.Validate(connectorapi.Command{Verb: verbPrint}); !fault.HasCode(
		err, connectorapi.CapabilityNotSupported.Code(),
	) {
		t.Errorf("err = %v, want the capability to be refused", err)
	}
}

// TestARefusalOffersThePrintersVerbsWithinABound is the driver-shaped attack on
// the list a refusal carries.
//
// A Baseline is third-party code, and a key runs to 253 bytes, so how large one
// refusal grows is a number this contract chooses rather than a driver. The list
// is a window on the set and the count is the whole of it, so a caller can tell
// a whole list from a clipped one and read the printer for the rest.
func TestARefusalOffersThePrintersVerbsWithinABound(t *testing.T) {
	t.Parallel()

	const declared = 500

	// Keys at the length the vocabulary permits, which is the worst a driver can
	// hand this refusal.
	set := make(connectorapi.Set, declared)
	for i := range declared {
		key := connectorapi.Capability(fmt.Sprintf("core.command.%s%03d", strings.Repeat("x", 236), i))
		if !key.Valid() {
			t.Fatalf("the fixture built %q, which is not a capability key", key)
		}

		set[key] = connectorapi.Value{Flag: true}
	}

	_, err := set.Validate(connectorapi.Command{Verb: "core.command.absent"})

	var f *fault.Fault
	if !errors.As(err, &f) {
		t.Fatalf("error is not a fault: %v", err)
	}

	offered, ok := f.Params().Get("verbs")
	if !ok {
		t.Fatal("the refusal offers no verbs")
	}

	var verbs []string
	if decodeErr := json.Unmarshal([]byte(offered.String()), &verbs); decodeErr != nil {
		t.Fatalf("the verbs payload is not readable by a client: %v", decodeErr)
	}

	count, _ := f.Params().Get("verb_count")

	t.Logf("observed: a driver declaring %d verbs produced a refusal offering %d of them "+
		"in %d bytes, reporting verb_count %s", declared, len(verbs), len(offered.String()), count)

	if len(verbs) >= declared {
		t.Errorf("the refusal listed %d of the %d verbs the driver declared, so a driver "+
			"decides how large one refusal is", len(verbs), declared)
	}

	if count.String() != strconv.Itoa(declared) {
		t.Errorf("verb_count = %s, want %d, so a caller cannot tell a whole list from a clipped one",
			count, declared)
	}

	if !slices.IsSorted(verbs) {
		t.Error("the verbs are unsorted, so two refusals from one printer offer different windows")
	}
}
