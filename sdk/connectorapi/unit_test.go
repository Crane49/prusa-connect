package connectorapi_test

import (
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const argCelsius = connectorapi.Capability("core.command.set_nozzle_temperature#target")

func celsiusSet(unit connectorapi.Unit) connectorapi.Set {
	return connectorapi.Set{
		"core.command.set_nozzle_temperature": flag(),
		argCelsius: {
			Flag: true, Kind: connectorapi.KindInteger, Required: true,
			Min: 0, HasMin: true, Limit: 300, HasLimit: true, Unit: unit,
		},
	}
}

func TestAMergeKeepsTheUnitOneTierDeclared(t *testing.T) {
	t.Parallel()

	declared, silent := celsiusSet(connectorapi.UnitCelsius), celsiusSet("")

	for direction, merged := range map[string]connectorapi.Set{
		"declared ∩ silent": declared.Intersect(silent),
		"silent ∩ declared": silent.Intersect(declared),
	} {
		if got := merged[argCelsius].Unit; got != connectorapi.UnitCelsius {
			t.Errorf("%s: the merged unit is %q, and the one tier that declared one said celsius", direction, got)
		}
	}
}

// Two units under one key are two meanings under one name. Neither number can
// be read, so the argument goes, and its verb goes with it because the argument
// was required.
func TestAMergeOfTwoUnitsKeepsNeither(t *testing.T) {
	t.Parallel()

	merged := celsiusSet(connectorapi.UnitCelsius).Intersect(celsiusSet(connectorapi.UnitPercent))

	if merged.Has(argCelsius) {
		t.Errorf("the argument survived a merge of celsius and percent as %+v", merged[argCelsius])
	}
}

func TestProblemsRefusesAUnitNothingCanRead(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		set  connectorapi.Set
		want string
	}{
		"a unit outside the closed set": {set: celsiusSet("kelvin"), want: "and the units are"},
		"a unit on text": {
			set: connectorapi.Set{
				"core.command.start_print": flag(),
				"core.command.start_print#path": {
					Flag: true, Kind: connectorapi.KindString, Unit: connectorapi.UnitMillimeter,
				},
			},
			want: "a unit applies to a number only",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			problems := c.set.Problems()

			found := false

			for _, problem := range problems {
				found = found || strings.Contains(problem.Error(), c.want)
			}

			if !found {
				t.Errorf("the set reports %v, and a problem saying %q was expected", problems, c.want)
			}
		})
	}

	if problems := celsiusSet(connectorapi.UnitCelsius).Problems(); len(problems) > 0 {
		t.Errorf("a temperature in celsius is refused: %v", problems)
	}
}
