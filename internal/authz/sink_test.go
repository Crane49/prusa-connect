package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The sink type, and the two things it exists for: a sink and a service account
// sharing an id are two principals, and one org tuple feeds a sink the fleet.
// The second rests on the first, which is why they are decided in one file.

// TestASinkAndAServiceAccountOfTheSameNameHoldDifferentGrants is the separation,
// from both sides. The fixture grants sink:audit org viewer and
// service_account:audit manager on one printer, under one id, and neither may
// be reached through the other.
func TestASinkAndAServiceAccountOfTheSameNameHoldDifferentGrants(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		sink := subjectOf(t, "sink:audit")
		account := subjectOf(t, "service_account:audit")

		// The sink does not inherit the service account's manager grant. That
		// is the half that bites while nothing issues a service account
		// credential: the sink is running, so a grant written for an
		// integration nobody has built yet would reach a live webhook.
		held, err := store.Can(t.Context(), sink, ActionAdminister, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.False(t, held, "the sink holds a grant written for the service account")

		// And the service account does not inherit the sink's fleet-wide read.
		// That is the half that bites once this build issues a service account
		// credential: every grant written to feed a sink would otherwise be a
		// grant that credential authenticates into.
		held, err = store.Can(t.Context(), account, ActionObserve, deviceOf(t, "p-beta"))
		require.NoError(t, err)
		require.False(t, held, "the service account holds the sink's fleet-wide read")

		// Each still holds its own.
		held, err = store.Can(t.Context(), sink, ActionObserve, deviceOf(t, "p-beta"))
		require.NoError(t, err)
		require.True(t, held, "the sink lost the grant written for it")

		held, err = store.Can(t.Context(), account, ActionAdminister, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.True(t, held, "the service account lost the grant written for it")
	})
}

// TestOneOrgTupleFeedsASinkTheWholeFleet is row 3 of the old backlog. A sink that wants
// every printer's events needed a grant written per printer; this is the one
// tuple that replaces four hundred of them.
func TestOneOrgTupleFeedsASinkTheWholeFleet(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		sink := subjectOf(t, "sink:audit")

		visible, err := store.Visible(t.Context(), sink)
		require.NoError(t, err)

		// Every printer placed in the org, and the camera bound to one of them.
		// cam-alpha carries no placement of its own: it arrives down
		// device#parent, at the level the sink reached p-alpha with, which is
		// ADR-20260826-5yj rule 15 composing with the org edge rather than a second
		// mechanism. So a fleet-wide sink covers a bound camera for free, and
		// what it is covered by is still tenancy and not a blanket over the
		// directory: p-hidden holds grants, is absent from the directory, and
		// appears here for nobody.
		require.ElementsMatch(t,
			[]string{"p-alpha", "p-beta", "p-gamma", "p-delta", "cam-alpha"}, deviceIDs(visible.Devices))
	})
}

// TestAFleetWideSinkReachesAPrinterAdmittedAfterTheGrantWasWritten is the half
// no per-printer grant can cover: the machine that dials in tomorrow, which
// nobody is there to write a tuple for.
func TestAFleetWideSinkReachesAPrinterAdmittedAfterTheGrantWasWritten(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		sink := subjectOf(t, "sink:audit")

		// Nothing yet: the printer does not exist, and a grant on a device the
		// directory does not hold compiles to no row.
		held, err := store.Can(t.Context(), sink, ActionObserve, deviceOf(t, "p-epsilon"))
		require.NoError(t, err)
		require.False(t, held)

		// Admission, as internal/app's register performs it: the directory row
		// and the org placement, and no grant of any kind.
		require.NoError(t, store.UpsertDevice(t.Context(), "p-epsilon", "Epsilon"))
		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-epsilon#org@org:acme")))

		held, err = store.Can(t.Context(), sink, ActionObserve, deviceOf(t, "p-epsilon"))
		require.NoError(t, err)
		require.True(t, held, "a printer admitted after the grant was written is invisible to the sink")
	})
}

// TestAnOrgRoleAboveViewerRefusesASink holds the level the vocabulary stops a
// fleet-wide sink grant at. A sink reads; org:acme#admin@sink:audit could
// confer nothing it would use, and would stand in the graph saying this webhook
// may reflash every machine in the building.
func TestAnOrgRoleAboveViewerRefusesASink(t *testing.T) {
	t.Parallel()

	sink := subjectOf(t, "sink:audit")

	org, err := NewObject(TypeOrg, "acme")
	require.NoError(t, err)

	for _, relation := range []Relation{RelAdmin, RelOperator} {
		_, err = NewTuple(org, relation, sink)
		require.Errorf(t, err, "org#%s accepted a sink", relation)
	}

	_, err = NewTuple(org, RelViewer, sink)
	require.NoError(t, err, "org#viewer refused the one shape a fleet-wide sink is written as")
}

// TestTheOrgRolesStillRefuseAServiceAccountAndANode is the rule the sink shape
// was added beside rather than through. A credential must not become fleet-wide
// by accident, and both of these are credentials.
func TestTheOrgRolesStillRefuseAServiceAccountAndANode(t *testing.T) {
	t.Parallel()

	org, err := NewObject(TypeOrg, "acme")
	require.NoError(t, err)

	for _, text := range []string{"service_account:audit", "node:site-b"} {
		for _, relation := range []Relation{RelViewer, RelOperator, RelAdmin} {
			_, err = NewTuple(org, relation, subjectOf(t, text))
			require.Errorf(t, err, "org#%s accepted %s", relation, text)
		}
	}
}

// TestASinkIsNotAGroupMember keeps the sink out of the one place a principal
// picks up grants nobody wrote for it by name.
func TestASinkIsNotAGroupMember(t *testing.T) {
	t.Parallel()

	group, err := NewObject(TypeGroup, "maintenance")
	require.NoError(t, err)

	_, err = NewTuple(group, RelMember, subjectOf(t, "sink:audit"))
	require.Error(t, err, "a sink joined a team and inherited its grants")
}

// TestNothingIsGrantedOnASink says the type stands on the subject side only. A
// sink is a line of this server's configuration; the object a grant about sinks
// is written on is resource_kind:sink, which is the surface that already
// exists.
func TestNothingIsGrantedOnASink(t *testing.T) {
	t.Parallel()

	object, err := NewObject(TypeSink, "audit")
	require.NoError(t, err, "sink:audit is a well-formed node")

	for _, relation := range []Relation{RelViewer, RelOperator, RelManager, RelBlocked, RelOwner} {
		_, err = NewTuple(object, relation, subjectOf(t, "user:root"))
		require.Errorf(t, err, "sink#%s was written", relation)
	}

	_, administrable := AdministerAction(TypeSink)
	require.False(t, administrable, "a sink reports an action for administering grants written on it")
}
