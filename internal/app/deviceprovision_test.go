package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The four verbs that change the machine rather than the job on it. They are
// written out here and read back from the vocabulary below, so a verb that
// slipped to another action fails this test rather than silently widening who
// may send it.
var irreversibleVerbs = []string{
	flashVerb,
	"core.command.upgrade",
	tokenVerb,
	"core.command.set_hostname",
}

// The two of the four that other tests name directly.
const (
	flashVerb = "core.command.flash"
	tokenVerb = "core.command.set_token"
)

// TestFlashingFirmwareIsAboveOperator is the owner's decision of 2026-08-27
// where the verbs actually leave: the device plane is what every transport
// reaches, so an operator refused here is refused on all of them.
//
// The graph is the real one, bootstrapped through openGraph exactly as a first
// run builds it, and the printer arrives through the host surface a driver
// uses. Nothing here is a stand-in for the check.
//
// Three principals, because narrowing an action is only proved by the pair: an
// operator on a device loses the four, a manager of that device keeps them, and
// the org admin the deployment bootstraps keeps them too.
func TestFlashingFirmwareIsAboveOperator(t *testing.T) {
	t.Parallel()

	for _, verb := range irreversibleVerbs {
		require.Equalf(t, authz.ActionConfigure, authz.ActionForVerb(authz.DeviceKindPrinter, verb),
			"%s no longer takes provision, so this test asserts nothing about it", verb)
	}

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-FLASH")

	admin := subjectOf(config.Default().Principal)

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, operator)))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))

	cases := map[string]struct {
		principal string
		verb      string
		reachable bool
	}{
		"an operator sends an ordinary command": {principal: operator, verb: nozzleVerb, reachable: true},
		"an operator flashes firmware":          {principal: operator, verb: irreversibleVerbs[0], reachable: false},
		"an operator upgrades firmware":         {principal: operator, verb: irreversibleVerbs[1], reachable: false},
		"an operator writes the token":          {principal: operator, verb: irreversibleVerbs[2], reachable: false},
		"an operator renames the machine":       {principal: operator, verb: irreversibleVerbs[3], reachable: false},

		"a manager flashes firmware":    {principal: manager, verb: irreversibleVerbs[0], reachable: true},
		"a manager upgrades firmware":   {principal: manager, verb: irreversibleVerbs[1], reachable: true},
		"a manager writes the token":    {principal: manager, verb: irreversibleVerbs[2], reachable: true},
		"a manager renames the machine": {principal: manager, verb: irreversibleVerbs[3], reachable: true},

		"the org admin flashes firmware":    {principal: admin, verb: irreversibleVerbs[0], reachable: true},
		"the org admin renames the machine": {principal: admin, verb: irreversibleVerbs[3], reachable: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := plane.Command(t.Context(), tc.principal, id,
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

			// Every principal in this table holds a role on the printer, so
			// every one of them reads it, so the refusal names the class rather
			// than pretending the machine is not there. The case below the
			// table is the other arm.
			requireClassRefusal(t, err, true)
		})
	}

	// Somebody who holds nothing at all is still told the printer is absent.
	// The rule turns on whether the caller reads the machine, and this is the
	// arm of it the table cannot exercise.
	_, err := plane.Command(t.Context(), "user:nobody", id,
		httpapi.PrinterCommand{Verb: irreversibleVerbs[0]})
	requireClassRefusal(t, err, false)
}
