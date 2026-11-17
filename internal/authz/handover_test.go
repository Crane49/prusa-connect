package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The lockout refusal tells an administrator to grant a successor first and
// depart second. In a deployment with no issuer that advice cannot be followed:
// the admin role on an org is the one grant the surface refuses on the write
// path, so there is no successor to grant. The recovery is then changing
// core.principal and restarting.
//
// Owner question 54 settled the trade as configurable behaviour, and
// WithOrgAdminHandover is the key. These cases hold both of its positions,
// because a key whose closed position is untested is a key that only ever ran
// one way.
func TestTheOrgHandoverKeyOpensTheAdminRoleOnAnOrg(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e, WithOrgAdminHandover(true))
		root := subjectOf(t, "user:root")
		mallory := subjectOf(t, "user:mallory")

		alpha := deviceOf(t, "p-alpha")

		allowed, err := store.Can(t.Context(), mallory, ActionAdminister, alpha)
		require.NoError(t, err)
		require.False(t, allowed, "the successor already manages the device, so this case proves nothing")

		wrote, err := store.WriteGrant(t.Context(), root, mustTuple(t, "org:acme#admin@user:mallory"))
		require.NoError(t, err, "the handover key is open and the admin role is still refused")
		require.True(t, wrote)

		allowed, err = store.Can(t.Context(), mallory, ActionAdminister, alpha)
		require.NoError(t, err)
		require.True(t, allowed, "the successor holds admin on the org and reaches no device of it")

		// The second half of the handover, in the order the lockout refusal
		// names: the successor withdraws the departing administrator's grant.
		// The departing one cannot withdraw it themselves, because that is the
		// stand-down mayStandDown refuses whichever way the key is set.
		require.NoError(t, store.WithdrawGrant(t.Context(), mallory,
			mustTuple(t, "org:acme#admin@user:root")))

		allowed, err = store.Can(t.Context(), root, ActionAdminister, alpha)
		require.NoError(t, err)
		require.False(t, allowed, "the departed administrator still reaches a device of the org")
	})
}

// The refusal a deployment takes in exchange for the handover, and the reason
// the key is one flag and not two.
//
// An object's org edge is tenancy: it says which org the object belongs to, and
// whoever manages the object may write it. That is harmless while nobody can be
// put into a second org's admin role through the surface. Once the handover
// opens that door, a caller who admins one org and manages one object of
// another can attach their own org to that object and outrank the
// administrator the object already had, because mayRemove orders two principals
// by what each inherits there.
//
// The same call is made in both positions of the key, so the case cannot pass
// by refusing a write that was never permitted.
func TestTheOrgHandoverKeyClosesTheOrgEdgeOnAnObject(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		root := subjectOf(t, "user:root")
		edge := mustTuple(t, "device:p-alpha#org@org:other")

		closed := seedFixture(t, e)

		wrote, err := closed.WriteGrant(t.Context(), root, edge)
		require.NoError(t, err, "the org edge is refused with the handover closed, so this case proves nothing")
		require.True(t, wrote)

		open := seedFixture(t, e, WithOrgAdminHandover(true))

		_, err = open.WriteGrant(t.Context(), root, edge)
		requireCode(t, err, CodeAccessDenied())

		held, err := open.Tuples(t.Context())
		require.NoError(t, err)
		require.NotContains(t, held, edge, "a refused org edge reached the tuple table")
	})
}

// Withdrawal is not refused in either position. Removing an object from an org
// takes tenancy away rather than claiming it, and mayRemove bounds what that
// may take. A fix that closed the write by closing the relation would have
// taken this with it.
func TestTheOrgHandoverLeavesAnOrgEdgeWithdrawable(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e, WithOrgAdminHandover(true))
		root := subjectOf(t, "user:root")
		edge := mustTuple(t, "device:p-alpha#org@org:other")

		// Written past the surface, because the surface is what refuses it.
		// The org:acme edge stays, so the caller does not stand themselves down
		// by taking this one away.
		require.NoError(t, store.Write(t.Context(), edge))
		require.NoError(t, store.WithdrawGrant(t.Context(), root, edge))
	})
}

// The default. A Store opened with no option refuses the admin role and permits
// the org edge, which is the deployment the product ships and the behaviour
// every other case in this package was written against.
func TestTheOrgHandoverIsClosedUnlessTheKeyOpensIt(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		require.False(t, store.orgAdminHandover)

		root := subjectOf(t, "user:root")

		_, err := store.WriteGrant(t.Context(), root, mustTuple(t, "org:acme#admin@user:mallory"))
		requireCode(t, err, CodeAccessDenied())
	})
}
