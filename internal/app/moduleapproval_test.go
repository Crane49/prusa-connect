package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// The module instance every case here admits through, and the fingerprint of
// the machine it admits.
const (
	admittingModule = "sink:planner"
	admittedMachine = "FP-ADMITTED-BY-A-MODULE"
	admittedCode    = "K7PQ2XR4MN"
)

// moduleWithAdmission is a module's fleet view over a plane holding one
// registration for the machine, with the plane's graph deciding.
func moduleWithAdmission(t *testing.T, graph *authz.Store, plane *devices) (moduleFleet, *fakeAdmission) {
	t.Helper()

	table := &fakeAdmission{waiting: []admission.Registration{
		{Code: admittedCode, Fingerprint: admittedMachine, Serial: "SN-1"},
	}}

	admissions := newRegistrations([]admissionTable{
		{ref: connectorapi.Ref("xbuddy"), driver: table},
	}, plane)

	return moduleFleet{
		fleet:      plane,
		admissions: &admissions,
		grants:     graph,
		standing: moduleStanding{
			principal: admittingModule,
			ceiling:   []authz.Action{authz.ActionAdmitDevice},
		},
	}, table
}

// grantAdmission writes what admit_device is conferred by, manager on the
// driver resource kind, for one subject.
func grantAdmission(t *testing.T, graph *authz.Store, subject string) {
	t.Helper()

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeResourceKind, ref.KindDriver.String(), authz.RelManager, subject)))
}

// owners is who the graph says owns a device.
func owners(t *testing.T, graph *authz.Store, id string) []string {
	t.Helper()

	held, err := graph.DeviceOwners(t.Context(), id)
	require.NoError(t, err)

	out := make([]string, 0, len(held))
	for _, one := range held {
		out = append(out, one.String())
	}

	return out
}

// A module admitting a device as itself is recorded as what admitted it, and a
// machine this fleet already has keeps its id and its owner: nobody is claimed
// and nothing is transferred, because a program is not a person.
func TestAModulesOwnApprovalRecordsTheInstanceAndKeepsTheID(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	id := arriveUnder(t, plane, connectorapi.Identity{Vendor: "Prusa", Model: "MK4", UnitUID: admittedMachine})

	require.NoError(t, graph.Write(t.Context(), tuple(t, authz.TypeDevice, id, authz.RelOwner, "user:alpha")))
	grantAdmission(t, graph, admittingModule)

	fleet, table := moduleWithAdmission(t, graph, plane)

	approved, err := fleet.Approve(t.Context(), deviceapi.ApproveParams{Code: admittedCode})
	require.NoError(t, err)
	require.True(t, approved.Approved)

	require.Equal(t, admittingModule, table.admittedBy,
		"the registration does not record the module instance as what admitted the machine")

	bound, held := plane.identities.boundTo(admittedMachine)
	require.True(t, held)
	require.Equal(t, id, bound, "a module's approval minted a new id for a machine this fleet already had")

	require.Equal(t, []string{"user:alpha"}, owners(t, graph, id),
		"a module's approval changed who owns the machine")
}

// A module the operator has not granted admit_device is refused, and the
// registration is untouched. Reading the whole fleet does not confer it.
func TestAModuleNotGrantedAdmissionIsRefusedAndTheRegistrationStays(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeOrg, bootstrapOrg, authz.RelViewer, admittingModule)))

	fleet, table := moduleWithAdmission(t, graph, plane)

	_, err := fleet.Approve(t.Context(), deviceapi.ApproveParams{Code: admittedCode})
	require.ErrorIs(t, err, deviceapi.ErrDenied)
	require.Empty(t, table.approved, "a refused approval reached the registration table")
	require.Empty(t, table.admittedBy)
}

// An approval a module makes for a person it is serving is that person's: it
// is recorded and decided as it would be at the API, so the same person
// re-approving their own machine keeps its id.
func TestAModulesApprovalForAPersonIsThatPersons(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	id := arriveUnder(t, plane, connectorapi.Identity{Vendor: "Prusa", Model: "MK4", UnitUID: admittedMachine})

	require.NoError(t, graph.Write(t.Context(), tuple(t, authz.TypeDevice, id, authz.RelOwner, "user:alpha")))
	grantAdmission(t, graph, "user:alpha")

	fleet, table := moduleWithAdmission(t, graph, plane)

	_, err := fleet.Approve(t.Context(), deviceapi.ApproveParams{Principal: "user:alpha", Code: admittedCode})
	require.NoError(t, err)
	require.Equal(t, "user:alpha", table.admittedBy)

	bound, _ := plane.identities.boundTo(admittedMachine)
	require.Equal(t, id, bound)
	require.Equal(t, []string{"user:alpha"}, owners(t, graph, id))
}

// The person a module serves is held to the same grant a person at the API is.
func TestAPersonNotGrantedAdmissionIsRefusedThroughAModule(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	grantAdmission(t, graph, admittingModule)

	fleet, table := moduleWithAdmission(t, graph, plane)

	_, err := fleet.Approve(t.Context(), deviceapi.ApproveParams{Principal: "user:beta", Code: admittedCode})
	require.ErrorIs(t, err, deviceapi.ErrDenied,
		"the module's own grant answered for a person it was serving")
	require.Empty(t, table.approved)
}
