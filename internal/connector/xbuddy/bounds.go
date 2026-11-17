package xbuddy

import (
	"fmt"
	"strconv"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// aboveZero names the arguments the shipping validator requires to be strictly
// positive, keyed by the descriptor key that declares them.
//
// Value.Min is an inclusive lower bound, so the narrowest thing a descriptor can
// say about these is "zero or more" and the host would let a zero through. The
// vocabulary does not grow an exclusive bound for one argument's rule (ADR-20260825-dee,
// rejected), so the rule sits here under rule 8 and
// TestTheArgumentBoundsMatchTheShippingValidator holds it to the shipping one.
//
//nolint:gochecknoglobals // descriptor-adjacent data, read-only after init.
var aboveZero = map[connectorapi.Capability]bool{
	connectorapi.ArgumentKey(VerbSetToolDiameter, "nozzle_diameter"): true,
}

// refuseExcludedValues stops a value the descriptor admits and the wire form
// must not carry. It is the exclusive lower bound above and nothing else: every
// other bound in this dialect is declared data the host checks before Do.
func refuseExcludedValues(cmd connectorapi.Command) error {
	for name, raw := range cmd.Args {
		if !aboveZero[connectorapi.ArgumentKey(cmd.Verb, name)] {
			continue
		}

		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return fmt.Errorf("%w: %s is %q, which is not a number", ErrArgumentUnusable, name, raw)
		}

		if value <= 0 {
			return fmt.Errorf("%w: %s must be above zero, and %q is not", ErrArgumentUnusable, name, raw)
		}
	}

	return nil
}
