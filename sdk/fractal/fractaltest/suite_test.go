package fractaltest_test

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal/fractaltest"
)

// TestInMemoryBindingPassesTheSuite runs the conformance suite on the
// in-memory transport of this package. It also shows that the suite cases
// themselves pass on a correct transport.
func TestInMemoryBindingPassesTheSuite(t *testing.T) {
	t.Parallel()
	fractaltest.RunBindingSuite(t, fractaltest.InMemoryLink)
}
