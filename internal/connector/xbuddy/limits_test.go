package xbuddy

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const (
	// limitsFixture records what the shipping command validator answered about
	// each of a corpus of values. Its head says how it was taken.
	limitsFixture = "testdata/shipping-command-limits.tsv"

	// limitsColumns is the width of a row: command, argument, type, value,
	// refused, reason. A row of any other width is a corrupt file rather than a
	// row to interpret, and an accepted value carries an empty reason, so the
	// last column is often empty and the count still has to hold.
	limitsColumns = 6
)

// notPorted are the verbs the shipping matrix carries that this driver does not
// send. Their argument bounds have nowhere to live until the verb does, so they
// are named here rather than left as a silent gap in the comparison below.
//
// START_ENCRYPTED_DOWNLOAD names a second, encrypted channel this host does not
// serve. The other two download verbs are served over the websocket, by the
// block-request path in transfer.go, and this one is not: starting it would
// leave a device fetching from somewhere nothing is listening.
var notPorted = []string{
	"START_ENCRYPTED_DOWNLOAD",
}

// beyondTheDescriptor is every value the two sides answer differently, with the
// reason. It is the answer to "what could not move into the data", written where
// it is checked rather than in prose: an entry nothing exercises fails this test,
// and so does a divergence that is not listed.
//
// Every entry is a rule the shipping validator holds that a Value cannot say,
// because a Value declares a kind, a closed set of values, an inclusive lower
// bound and a bound on magnitude and nothing else. A divergence the other way,
// a rule this driver holds that the shipping validator does not, is the only
// other direction one may take, because it narrows. The traversal rule was the
// one entry of that kind, and both sides now refuse a path on every verb that
// names one.
var beyondTheDescriptor = map[string]string{
	"SET_ENCLOSURE_POSTPRINT_FILTRATION_TIME.enclosure_postprint_filtration_time.90": "" +
		"the shipping validator also requires whole minutes, and a descriptor has no divisibility rule, " +
		"so the host accepts 90 seconds and the printer is the one that refuses it",
	"SET_ENCLOSURE_POSTPRINT_FILTRATION_TIME.enclosure_postprint_filtration_time.61":  "whole minutes, as for 90",
	"SET_ENCLOSURE_POSTPRINT_FILTRATION_TIME.enclosure_postprint_filtration_time.100": "whole minutes, as for 90",
	"SET_ENCLOSURE_POSTPRINT_FILTRATION_TIME.enclosure_postprint_filtration_time.101": "whole minutes, as for 90",
	"SET_ENCLOSURE_POSTPRINT_FILTRATION_TIME.enclosure_postprint_filtration_time.5399": "" +
		"whole minutes, as for 90",
	"HOME.axis.": "" +
		"the shipping validator reads the axis character by character and takes an empty string as every " +
		"axis, the way a bare G28 does. A closed set cannot say \"empty means all\", so the descriptor " +
		"refuses the empty spelling and a caller that means every axis omits the argument",
}

// narrowerThanShipping names the arguments this host bounds and the shipping
// validator does not, keyed by command and argument rather than by value: a
// range refuses everything outside it, and listing each refused value would be
// the same sentence twenty times.
//
// This is the one direction a divergence may take without being a fault. The
// shipping validator holds no bound at all on a setpoint, which is how an
// operator set a nozzle target of 9999 and one of -4000 on a live machine and
// got a 200 for both. Narrowing is what the tiers are allowed to do (decision
// C1), so the host refuses the value and the printer never sees it.
//
// An entry nothing exercises fails this test, the same way a beyondTheDescriptor
// entry does: a bound that is dropped from the descriptor must not leave a line
// here saying it is still enforced.
var narrowerThanShipping = map[string]string{
	"SET_NOZZLE_TEMPERATURE.nozzle_temperature": "" +
		"a hotend setpoint, bounded here to 0-300 C because the shipping validator bounds it to nothing",
	"SET_HEATBED_TEMPERATURE.bed_temperature": "" +
		"a heated bed setpoint, bounded here to 0-120 C because the shipping validator bounds it to nothing",
	"SET_CHAMBER_TARGET_TEMP.chamber.target_temp": "" +
		"a chamber setpoint, bounded here to 0-80 C because the shipping validator bounds it to nothing",
	"SET_SPEED.speed": "" +
		"the M220 percentage, bounded here to 20-999 because the shipping validator bounds it to nothing",
	"SET_FLOW.flow": "" +
		"the M221 percentage, bounded here to 20-999 because the shipping validator bounds it to nothing",
	"MOVE.feedrate": "" +
		"a feedrate is a speed and not a displacement, so it has no negative half and no zero, and the " +
		"shipping validator accepts both",
	"MOVE_E.feedrate": "a feedrate, as for MOVE",
	"MOVE_Z.feedrate": "a feedrate, as for MOVE",
	"LOAD_FILAMENT.filament": "" +
		"a filament name, bounded here to seven bytes because the firmware keeps a name in eight bytes " +
		"with the terminating zero and reads a longer one as no name, and the shipping validator bounds it to nothing",
	"FLASH.path": "" +
		"M997 reads only an 8.3 short name in the root of /usb, so any other path is refused. The session " +
		"first swaps a long name for the short one the printer reports (flash.go), which this probe does not reach",
	"UPGRADE.path": "the M997 path, as for FLASH",
}

// countedFromZero is every value the two sides answer differently about
// because this host counts the tool argument from 0, as a head id under
// /tools/<id>, and the shipping validator counted it from 1 to 5 (ADR-20261022-6df
// rule 8 as amended). The last head is 7, on an INDX with eight heads. It is
// keyed by value, as beyondTheDescriptor is. The shipping rows are a capture
// of what that validator answered, so they stay as they were taken and the
// shift is recorded here. An entry nothing exercises fails the test.
//
//nolint:gochecknoglobals // test data.
var countedFromZero = map[string]string{
	"SET_TOOL_HARDENED.tool.0":        "the first head, which the shipping validator called 1",
	"SET_TOOL_HARDENED.tool.6":        "the seventh head of an INDX, past the shipping validator's 5",
	"SET_TOOL_HIGH_FLOW.tool.0":       "the first head, as for SET_TOOL_HARDENED",
	"SET_TOOL_HIGH_FLOW.tool.6":       "the seventh head of an INDX, as for SET_TOOL_HARDENED",
	"SET_TOOL_NOZZLE_DIAMETER.tool.0": "the first head, as for SET_TOOL_HARDENED",
	"SET_TOOL_NOZZLE_DIAMETER.tool.6": "the seventh head of an INDX, as for SET_TOOL_HARDENED",
}

// The shipping command validator bounds each argument with a hand-written
// conditional, one per command, reachable only from inside its own package.
// An earlier design decision puts those bounds in the descriptor so the host refuses a value
// before any driver sees it, and a second copy of a rule is only worth having if
// something compares the two.
//
// testdata/shipping-command-limits.tsv is what that validator answered about a
// corpus of values. This offers the same values to this driver and fails where
// the two answers differ. It is the same shape as internal/printer/profile's
// divergence gate and for the same reason: the matrix is not ours, and a
// transcription of it drifts.
func TestTheArgumentBoundsMatchTheShippingValidator(t *testing.T) {
	probes := shippingLimits(t)

	set := baseline()
	explained := map[string]bool{}
	narrowed := map[string]bool{}
	shifted := map[string]bool{}
	compared := 0

	for _, probe := range probes {
		verb := verbKeyFor(probe.Command)
		if !set.Has(verb) {
			continue
		}

		compared++

		here := driverRefuses(t, set, verb, probe.Argument, probe.Value)
		if here == probe.Refused {
			continue
		}

		argument := probe.Command + "." + probe.Argument
		if here && !probe.Refused && narrowerThanShipping[argument] != "" {
			narrowed[argument] = true

			continue
		}

		key := probe.Command + "." + probe.Argument + "." + probe.Value

		if countedFromZero[key] != "" {
			shifted[key] = true

			continue
		}

		reason, known := beyondTheDescriptor[key]
		if !known {
			t.Errorf("%s %s = %q: the shipping validator refuses it = %v, this driver refuses it = %v.\n"+
				"\tthe shipping reason was %q", probe.Command, probe.Argument, probe.Value,
				probe.Refused, here, probe.Reason)

			continue
		}

		explained[key] = true

		if reason == "" {
			t.Errorf("%s is listed as beyond the descriptor with no reason", key)
		}
	}

	for key := range beyondTheDescriptor {
		if !explained[key] {
			t.Errorf("%s is listed as a rule the descriptor cannot say, and the probe found no such "+
				"divergence. Either the rule moved into the data or the corpus stopped reaching it", key)
		}
	}

	for key := range countedFromZero {
		if !shifted[key] {
			t.Errorf("%s is listed as answered differently because the tool argument counts from 0, and "+
				"the probe found no such divergence", key)
		}
	}

	for argument := range narrowerThanShipping {
		if !narrowed[argument] {
			t.Errorf("%s is listed as bounded here and unbounded in the shipping validator, and the probe "+
				"found no value the two answer differently about. Either the bound is gone or the corpus "+
				"stopped reaching it", argument)
		}
	}

	if compared == 0 {
		t.Error("no probed value reached a verb this driver sends, so this test compared nothing")
	}
}

// The verbs the driver does not send are the reason the comparison above skips
// anything, so the list of them is pinned. A verb that gains a wire form without
// its bounds fails the comparison; one that is dropped from the matrix, or
// gains a wire form here, fails this.
func TestTheVerbsWithNoWireFormAreTheOnesTheComparisonSkips(t *testing.T) {
	probes := shippingLimits(t)

	set := baseline()
	skipped := map[string]bool{}

	for _, probe := range probes {
		if !set.Has(verbKeyFor(probe.Command)) {
			skipped[probe.Command] = true
		}
	}

	for command := range skipped {
		if !slices.Contains(notPorted, command) {
			t.Errorf("%s has arguments the shipping validator bounds and no wire form here, and it is not "+
				"one of the verbs recorded as not ported", command)
		}
	}

	// A verb that gains a wire form and stays on this list is a gap the
	// comparison above no longer has, recorded as one it still does. The list
	// is what ADR-20260825-dee's open item is read from, so a stale entry is a
	// published claim that a verb is missing when it is not.
	for _, command := range notPorted {
		if skipped[command] {
			continue
		}

		if !set.Has(verbKeyFor(command)) {
			continue
		}

		t.Errorf("%s is recorded as not ported and this driver sends it, so the record is stale", command)
	}

	// The other direction is not asserted from the corpus: it holds only verbs
	// that take an argument, so a listed verb with none of its own is not
	// evidence of anything. The whole vocabulary is held by
	// TestEveryProfileIntersectsWithTheBaseline.
	if len(skipped) == 0 {
		t.Error("every probed verb has a wire form, so the skip list is stale and this test watches nothing")
	}
}

// driverRefuses reports whether one value would be stopped before it reached the
// socket, which is the question a caller is really asking. Two things answer it:
// the host's validation against the resolved descriptor, and then
// everything between the host and the frame, which holds the rules a descriptor
// cannot state (ADR-20260825-dee rule 8).
//
// Every other argument of the verb is filled with a value the driver accepts,
// for the reason the shipping side does the same: a command missing a required
// argument is refused before any value is looked at, so a probe without the
// fillers would report every value as accepted.
func driverRefuses(t *testing.T, set connectorapi.Set, verb connectorapi.Capability, name, value string) bool {
	t.Helper()

	args := acceptedArguments(t, set, verb)
	args[name] = value

	checked, err := set.Validate(connectorapi.Command{Verb: verb, Args: args})
	if err != nil {
		return true
	}

	session := &Session{storage: defaultStorage}
	session.toolBase.Store(int32(toolBaseOne))
	_, err = session.build(checked, wireTable[verb])

	return err != nil
}

// acceptedArguments builds a value for every argument of a verb out of the
// descriptor itself: what it defaults to, what it lists, or the lowest value its
// bounds admit. The result is checked rather than assumed, because a filler this
// driver refuses would report as a refusal of the probed value.
func acceptedArguments(
	t *testing.T, set connectorapi.Set, verb connectorapi.Capability,
) map[string]string {
	t.Helper()

	args := map[string]string{}

	for _, key := range set.Keys() {
		name, isArgument := key.Argument()
		if !isArgument || key.Verb() != verb {
			continue
		}

		args[name] = acceptedValue(key, set[key])
	}

	checked, err := set.Validate(connectorapi.Command{Verb: verb, Args: args})
	if err != nil {
		t.Fatalf("%s: the descriptor refuses values taken from its own declaration (%v), "+
			"so nothing can be offered to one argument at a time", verb, err)
	}

	if refusal := refuseExcludedValues(checked); refusal != nil {
		t.Fatalf("%s: this driver refuses values taken from its own declaration (%v), "+
			"so nothing can be offered to one argument at a time", verb, refusal)
	}

	return args
}

// acceptedValue returns a value the whole driver accepts. An argument carrying
// an exclusive lower bound is the one case the descriptor's own floor is not
// one, because Value.Min is inclusive and the bound is not.
func acceptedValue(key connectorapi.Capability, value connectorapi.Value) string {
	switch {
	case value.Default != "":
		return value.Default
	case len(value.Supported) > 0:
		return value.Supported[0]
	case value.Kind == connectorapi.KindBoolean:
		return "true"
	case value.Kind == connectorapi.KindOpaque:
		return "{}"
	case value.Kind == connectorapi.KindString:
		return "x"
	case aboveZero[key]:
		return strconv.FormatFloat(max(value.Min, 0)+1, 'f', -1, 64)
	case value.HasMin:
		return strconv.FormatFloat(value.Min, 'f', -1, 64)
	default:
		return "0"
	}
}

// limitsProbe is one row of the fixture: one value offered to one argument of
// one command, and what the shipping validator made of it.
type limitsProbe struct {
	Command  string
	Argument string
	Type     string
	Value    string
	// Refused is whether the validator rejected the value for its own sake.
	Refused bool
	// Reason is the error it gave, and is empty when it accepted the value.
	Reason string
}

// shippingLimits reads the recorded corpus. The file is the authority this
// driver is compared against, so a row that does not parse stops the run rather
// than being skipped: a skipped row is a comparison that silently shrinks, which
// is the failure this whole gate exists to catch.
func shippingLimits(t *testing.T) []limitsProbe {
	t.Helper()

	content, err := os.ReadFile(limitsFixture)
	if err != nil {
		t.Fatalf("reading %s: %v", limitsFixture, err)
	}

	var probes []limitsProbe

	for number, line := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) != limitsColumns {
			t.Fatalf("%s:%d has %d columns, want %d", limitsFixture, number+1, len(fields), limitsColumns)
		}

		refused, parseErr := strconv.ParseBool(fields[4])
		if parseErr != nil {
			t.Fatalf("%s:%d: the refused column reads %q: %v",
				limitsFixture, number+1, fields[4], parseErr)
		}

		probes = append(probes, limitsProbe{
			Command:  fields[0],
			Argument: fields[1],
			Type:     fields[2],
			Value:    fields[3],
			Refused:  refused,
			Reason:   fields[5],
		})
	}

	if len(probes) == 0 {
		t.Fatalf("%s holds no row, which means this gate is reading nothing", limitsFixture)
	}

	return probes
}

// verbKeyFor is the capability key for a shipping command name. The conversion
// is internal/printer/profile's own, because that package's extraction converts
// the same names and the two have to agree.
func verbKeyFor(command string) connectorapi.Capability {
	return profile.VerbForShippingCommand(command)
}
