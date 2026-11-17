package authz

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// TestAnOrgAdminWritesAndWithdrawsTheFleetWideTuple is the old backlog's fleet-wide row
// through the surface an operator has. One tuple on the org covers every
// printer in it, including one admitted after the tuple was written, and
// withdrawing it takes the whole of that back.
//
// It is written and withdrawn by a caller rather than by Store.Write, which is
// the difference between a graph that can express a fleet-wide grant and a
// product in which somebody can write one.
func TestAnOrgAdminWritesAndWithdrawsTheFleetWideTuple(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")
		nightly := subjectOf(t, "sink:nightly")
		fleet := mustTuple(t, "org:acme#viewer@sink:nightly")

		wrote, err := store.WriteGrant(t.Context(), root, fleet)
		require.NoError(t, err)
		require.True(t, wrote, "the fleet-wide tuple was already in the graph")

		for _, id := range []string{"p-alpha", "p-beta", "p-gamma", "p-delta"} {
			allowed, canErr := store.Can(t.Context(), nightly, ActionObserve, deviceOf(t, id))
			require.NoError(t, canErr)
			require.Truef(t, allowed, "one org tuple did not reach device:%s", id)
		}

		// A printer admitted afterwards, placed the way internal/app's register
		// places one: a directory row and the org edge, and no grant of its own.
		require.NoError(t, store.UpsertDevices(t.Context(), DeviceRow{ID: "p-later", Name: "Later"}))
		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-later#org@org:acme")))

		allowed, err := store.Can(t.Context(), nightly, ActionObserve, deviceOf(t, "p-later"))
		require.NoError(t, err)
		require.True(t, allowed, "a printer admitted after the grant was not covered by it")

		require.NoError(t, store.WithdrawGrant(t.Context(), root, fleet))

		for _, id := range []string{"p-alpha", "p-later"} {
			reaches, canErr := store.Can(t.Context(), nightly, ActionObserve, deviceOf(t, id))
			require.NoError(t, canErr)
			require.Falsef(t, reaches, "a withdrawn fleet tuple still reaches device:%s", id)
		}
	})
}

// TestOnlyAnOrgAdminAdministersAnOrg is the permission half. Administering an
// org is holding the admin role on it, which is the level the vocabulary
// already names as owning one, and an org viewer holds no more of it than a
// device's viewer holds of that device's manage.
//
// The second half is the same property TestGrantRefusalsHideWhetherTheObjectExists
// holds for a device, and here it needs no arrangement: there is no org
// directory, so an org nobody has written a tuple on has no admin either and
// reaches the refusal by the same path.
func TestOnlyAnOrgAdminAdministersAnOrg(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")
		olga := subjectOf(t, "user:olga")
		acme := objectOf(t, TypeOrg, "acme")

		held, err := store.Grants(t.Context(), root, acme)
		require.NoError(t, err)
		require.NotEmpty(t, held, "the admin of an org cannot read the grants written on it")

		_, err = store.Grants(t.Context(), olga, acme)
		requireCode(t, err, CodeAccessDenied())

		_, err = store.WriteGrant(t.Context(), olga, mustTuple(t, "org:acme#viewer@user:mallory"))
		requireCode(t, err, CodeAccessDenied())

		err = store.WithdrawGrant(t.Context(), olga, mustTuple(t, "org:acme#viewer@user:olga"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(), olga, ActionObserve, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.True(t, allowed, "a refused withdrawal took the grant away anyway")

		_, err = store.Grants(t.Context(), olga, acme)
		hidden := problemOf(t, err)

		_, err = store.Grants(t.Context(), olga, objectOf(t, TypeOrg, "elsewhere"))
		missing := problemOf(t, err)

		require.Equal(t,
			strings.ReplaceAll(missing, "elsewhere", "<id>"),
			strings.ReplaceAll(hidden, "acme", "<id>"),
			"the refusal for an org the caller may not administer must read as the refusal "+
				"for one that is not there")
	})
}

// TestAnOrgTupleConfersNothingTheCallerDoesNotHoldOnEveryObjectItReaches is the
// escalation rule read across a fleet, which is what an org tuple needs and a
// device tuple does not: a grant on a device is bounded by what the caller
// holds on that device, and a grant on an org lands on every device the org
// reaches.
//
// The state that makes it bite is a block. An org admin blocked off one printer
// cannot lift the block, because lifting it takes the manage the block removed,
// and without this rule they could still hand a confederate standing on that
// printer by writing it on the org. Both directions are here: the block bounds
// what they may confer, and it bounds what they may take.
func TestAnOrgTupleConfersNothingTheCallerDoesNotHoldOnEveryObjectItReaches(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")

		// Written rather than granted: what one administrator may block another
		// off is mayRemove's rule and is held in containment_test.go. This case
		// is about what the blocked administrator may do afterwards.
		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-beta#blocked@user:root")))

		_, err := store.WriteGrant(t.Context(), root, mustTuple(t, "org:acme#operator@user:mallory"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(),
			subjectOf(t, "user:mallory"), ActionOperate, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.False(t, allowed, "a refused org grant reached the projection")

		err = store.WithdrawGrant(t.Context(), root, mustTuple(t, "org:acme#viewer@user:olga"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err = store.Can(t.Context(), subjectOf(t, "user:olga"), ActionObserve, deviceOf(t, "p-beta"))
		require.NoError(t, err)
		require.True(t, allowed, "a refused withdrawal took the grant away anyway")
	})
}

// TestTheAdminRoleOnAnOrgIsNotConferrableThroughTheGrantSurface is the one org
// grant this surface refuses, and it is refused because of what it does outside
// the org rather than inside it.
//
// Inside the org it confers nothing new: admin is the manager level, so it
// hands out exactly what the caller writing it already holds everywhere the org
// reaches. Outside it raises what its holder may take. mayRemove bounds a
// caller by what they inherit on an object, a manager of a device may write
// that device's org edge, and an org admin who attaches their own org to a
// device of somebody else's org then outranks that org's administrator there.
// The old backlog records that as unreachable while nothing but Bootstrap can put a
// caller into an org's admin, and this keeps that true. Owner question 54 is
// whether it should stay true.
//
// Withdrawal is not the same act and is permitted: it takes standing rather
// than conferring it, and mayRemove already bounds what it may take.
func TestTheAdminRoleOnAnOrgIsNotConferrableThroughTheGrantSurface(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")
		mallory := subjectOf(t, "user:mallory")

		_, err := store.WriteGrant(t.Context(), root, mustTuple(t, "org:acme#admin@user:mallory"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(), mallory, ActionAdminister, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.False(t, allowed, "a refused admin grant reached the projection")

		// The two roles below it are conferrable, which is what the fleet-wide
		// row needs and what an operator writes.
		wrote, err := store.WriteGrant(t.Context(), root, mustTuple(t, "org:acme#operator@user:mallory"))
		require.NoError(t, err)
		require.True(t, wrote)

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "org:acme#admin@user:dana")))
		require.NoError(t, store.WithdrawGrant(t.Context(), root,
			mustTuple(t, "org:acme#admin@user:dana")))
	})
}

// TestAnOrgAdminCannotWithdrawTheirOwnLastAdminGrant is mayStandDown on an org.
// It is the same rule and the same recovery: nothing writes the first grant
// back, so the handover runs successor first and departure second.
func TestAnOrgAdminCannotWithdrawTheirOwnLastAdminGrant(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")
		acme := objectOf(t, TypeOrg, "acme")

		err := store.WithdrawGrant(t.Context(), root, mustTuple(t, "org:acme#admin@user:root"))
		requireCode(t, err, CodeAccessDenied())

		held, err := store.Grants(t.Context(), root, acme)
		require.NoError(t, err)
		require.NotEmpty(t, held, "a refused withdrawal locked the administrator out anyway")

		// The successor is written rather than granted, because writing an org
		// admin is what this surface refuses. The order is the one the refusal
		// names.
		require.NoError(t, store.Write(t.Context(), mustTuple(t, "org:acme#admin@user:dana")))
		require.NoError(t, store.WithdrawGrant(t.Context(), subjectOf(t, "user:dana"),
			mustTuple(t, "org:acme#admin@user:root")))

		_, err = store.Grants(t.Context(), root, acme)
		requireCode(t, err, CodeAccessDenied())
	})
}

// TestAnOrgGrantReachesTheResourceKindsTheOrgHolds is the other half of what an
// org tuple lands on. A resource kind names its org the same way a device does,
// so a fleet-wide grant is not only a fleet of printers, and the containment
// rule has to read the kinds as well or an org viewer would be conferrable by
// somebody blocked off the configuration surface.
func TestAnOrgGrantReachesTheResourceKindsTheOrgHolds(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")
		mallory := subjectOf(t, "user:mallory")

		wrote, err := store.WriteGrant(t.Context(), root, mustTuple(t, "org:acme#viewer@user:mallory"))
		require.NoError(t, err)
		require.True(t, wrote)

		allowed, err := store.CanResource(t.Context(), mallory, ActionViewResource,
			instanceOf(t, ref.KindStorage, "local"))
		require.NoError(t, err)
		require.True(t, allowed, "an org grant did not reach the kinds the org holds")

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "resource_kind:sink#blocked@user:root")))

		_, err = store.WriteGrant(t.Context(), root, mustTuple(t, "org:acme#operator@user:mallory"))
		requireCode(t, err, CodeAccessDenied())
	})
}

// TestAdministrableTypesAreTheOnesThisSurfaceCanDecide holds the boundary in
// one place. A type is administrable when this surface can say who owns an
// object of it, which is either the manager action the vocabulary defines on it
// or, for an org, the admin role written on the org itself. Nothing else has
// one: a group has no owning level at all, and a device group has one that
// nothing in the product writes, because nothing places an admitted printer in
// a group.
func TestAdministrableTypesAreTheOnesThisSurfaceCanDecide(t *testing.T) {
	t.Parallel()

	administrable := make([]string, 0, len(Types()))

	for _, typ := range Types() {
		if Administrable(typ) {
			administrable = append(administrable, typ.String())
		}
	}

	require.Equal(t, []string{"org", "device", "resource_kind"}, administrable)

	for _, typ := range Types() {
		if _, ok := AdministerAction(typ); ok {
			require.Truef(t, Administrable(typ),
				"%s carries an administering action and is not administrable", typ)
		}
	}
}
