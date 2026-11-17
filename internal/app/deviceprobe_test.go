package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// bedVerb is the verb the machine in these cases does not have. A driver's code
// can send it, and this particular machine has no heated bed.
const bedVerb = "core.command.set_bed_temperature"

// twoVerbBaseline is what a driver's code can do for any machine of its family.
func twoVerbBaseline() connectorapi.Set {
	return connectorapi.Set{
		connectorapi.Capability(nozzleVerb): {Flag: true},
		connectorapi.Capability(bedVerb):    {Flag: true},
	}
}

// probingSession is a driver session whose machine answered for itself during
// the handshake, which is what connectorapi.ReportsProbe carries.
type probingSession struct {
	talkingSession

	probe connectorapi.Set
}

func (s *probingSession) Capabilities() (connectorapi.Set, uint64) { return twoVerbBaseline(), 1 }

func (s *probingSession) Probe() connectorapi.Set { return s.probe }

// arriveProbing puts one machine in the plane through the host surface a driver
// uses, with the answer that machine gave about itself.
func arriveProbing(t *testing.T, plane *devices, serial string, probe connectorapi.Set) string {
	t.Helper()

	host := plane.Arrivals("test", twoVerbBaseline(),
		capability.NewResolver(profile.Registry{}, discard()), "")

	session := &probingSession{
		talkingSession: talkingSession{
			countedSession: countedSession{
				identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: serial},
				patches:  make(chan connectorapi.StatePatch),
			},
			answer: connectorapi.Result{OK: true},
		},
		probe: probe,
	}

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { close(session.patches) })

	return plane.idOf(session.identity)
}

// A machine that says it has no heated bed is a machine the API does not offer
// a bed verb for, and one the host refuses a bed command for before the driver
// is reached.
//
// Without the probe the driver's whole baseline was in force: the listing
// offered a verb the machine does not have, and the refusal came from the
// driver, one call after a caller had been told the command was on its way.
func TestAMachineThatSaysItHasNoBedIsNotOfferedTheBedVerbs(t *testing.T) {
	t.Parallel()

	plane, _ := planeAndBus(t)

	probe := connectorapi.Set{connectorapi.Capability(nozzleVerb): {Flag: true}}
	id := arriveProbing(t, plane, "SIM-NO-BED", probe)

	device, err := plane.Get(t.Context(), "user:"+config.DefaultPrincipal, id)
	require.NoError(t, err)

	verbs := map[string]bool{}
	for _, verb := range device.Capabilities {
		verbs[verb] = true
	}

	require.True(t, verbs[nozzleVerb], "the machine said it has a nozzle and the API does not offer it")
	require.False(t, verbs[bedVerb],
		"the machine said it has no heated bed and the API offers a bed verb for it anyway")

	_, err = plane.Command(t.Context(), "user:"+config.DefaultPrincipal, id,
		httpapi.PrinterCommand{Verb: bedVerb})
	require.Error(t, err)
	require.True(t, fault.HasCode(err, connectorapi.CapabilityNotSupported.Code()),
		"a command for a verb this machine does not have was refused with %v", err)
}

// A driver that says nothing about its machine keeps the set the admission
// resolved. Reporting nothing is not reporting an empty machine.
func TestAMachineThatSaysNothingKeepsTheSetTheAdmissionResolved(t *testing.T) {
	t.Parallel()

	plane, _ := planeAndBus(t)

	id := arriveProbing(t, plane, "SIM-SILENT", nil)

	device, err := plane.Get(t.Context(), "user:"+config.DefaultPrincipal, id)
	require.NoError(t, err)

	require.Len(t, device.Capabilities, len(twoVerbBaseline()),
		"a driver that reported no probe had its machine narrowed anyway")
}
