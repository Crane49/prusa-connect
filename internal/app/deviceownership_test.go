package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// owner is somebody whose machine it is. They hold operator on it like anybody
// running prints, and the owner edge is what carries the four irreversible
// verbs the role layer stops at manager.
const owner = "user:carol"

// TestAnOwnerFlashesTheirOwnPrinterThroughTheDevicePlane is the second half of
// the owner's decision of 2026-08-27, where the verbs actually leave. Both
// principals hold operator on the same printer and only one of them owns it.
//
// The graph is the real one, bootstrapped through openGraph as a first run
// builds it, and the printer arrives through the host surface a driver uses.
func TestAnOwnerFlashesTheirOwnPrinterThroughTheDevicePlane(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	mine := arrive(t, plane, "SIM-OWNED")
	theirs := arrive(t, plane, "SIM-UNOWNED")

	for _, id := range []string{mine, theirs} {
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelOperator, owner)))
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelOperator, operator)))
	}

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, mine, authz.RelOwner, owner)))

	cases := map[string]struct {
		principal string
		device    string
		verb      string
		reachable bool
	}{
		"an owner flashes the printer they own": {
			principal: owner, device: mine, verb: flashVerb, reachable: true,
		},
		"an owner writes the token on it": {
			principal: owner, device: mine, verb: tokenVerb, reachable: true,
		},
		"an owner does not flash one they do not own": {
			principal: owner, device: theirs, verb: flashVerb, reachable: false,
		},
		"an operator does not flash a printer": {
			principal: operator, device: mine, verb: flashVerb, reachable: false,
		},
		"an operator still commands it": {
			principal: operator, device: mine, verb: nozzleVerb, reachable: true,
		},
		"an owner sends raw gcode on their own printer": {
			principal: owner, device: mine, verb: rawGCode, reachable: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := plane.Command(t.Context(), tc.principal, tc.device,
				httpapi.PrinterCommand{Verb: tc.verb})
			require.Error(t, err, "the stand-in session declares no capability, so nothing is ever accepted")

			if tc.reachable {
				require.Falsef(t, fault.HasCode(err, fault.PrinterNotFound.Code()),
					"%s holds the action %s takes and the plane reported the printer absent",
					tc.principal, tc.verb)
				// Not a second assertion that the call succeeded: these verbs
				// are in the dangerous tier, so a manager who holds the class
				// is still refused until a row names them, and that refusal is
				// the forbidden. What is being held here is only that the
				// printer was not reported absent to somebody who reads it.

				return
			}

			// Every principal here holds a role on the printer they are
			// refused, including the owner of the other machine, so each of
			// them reads it and each is refused the class by name. Ownership
			// changes who reaches the machine, not what a refusal says about
			// one the caller can already see.
			requireClassRefusal(t, err, true)
		})
	}
}
