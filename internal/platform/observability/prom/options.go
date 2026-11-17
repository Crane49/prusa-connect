package prom

import (
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/platform/observability"
)

// Registry errors.
var (
	// ErrNotRegistered is a handle asked for a metric the registry was not
	// built with.
	ErrNotRegistered = errors.New("metric not registered")
	// ErrLabelValues is a handle asked for with the wrong number of label
	// values.
	ErrLabelValues = errors.New("wrong label values")
	// ErrFanoutBudget is more metrics carrying a per-device dimension than the
	// deployment allowed. Each one is another series per printer.
	ErrFanoutBudget = errors.New("fan-out budget exceeded")
)

// Options is what a deployment may change about the registry. The zero value
// is the default catalog's budget.
type Options struct {
	// FanoutBudget is how many metrics may carry a per-device dimension. Zero
	// means observability.DefaultFanoutBudget. A deployment raises it to turn
	// on an optional per-printer metric, so the cost is a number somebody has
	// to write down.
	FanoutBudget int
}

// validate refuses a definition set before anything is registered, in both
// builds. A definition that is wrong is wrong whether or not this binary links
// a Prometheus client, and a check that ran only in the tagged build would let
// the defect reach a release.
func validate(opts Options, defs []observability.Def) error {
	budget := opts.FanoutBudget
	if budget == 0 {
		budget = observability.DefaultFanoutBudget
	}

	var (
		problems []error
		fanout   int
	)

	for _, def := range defs {
		if err := def.Validate(); err != nil {
			problems = append(problems, err)
		}

		if _, ok := def.Fanout(); ok {
			fanout++
		}
	}

	if fanout > budget {
		problems = append(problems, fmt.Errorf("%w: %d metrics carry one, the budget is %d",
			ErrFanoutBudget, fanout, budget))
	}

	return errors.Join(problems...)
}
