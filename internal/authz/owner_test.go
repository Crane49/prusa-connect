package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Ownership is what makes the four irreversible verbs reachable by somebody who
// runs the machine and is not an administrator of it. It is the owner's
// decision of 2026-08-27, and the pair is the whole of it: an owner holding
// operator on their own device flashes it, and an operator who owns nothing
// does not.
//
// user:carol owns p-beta and holds nothing else. user:alice holds operator on
// p-beta through a team and owns nothing, which is the fixture's own shape.
func TestAnOwnerFlashesTheirOwnDeviceAndAnOperatorDoesNot(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:p-beta#owner@user:carol"),
			mustTuple(t, "device:p-beta#operator@user:carol"),
			mustTuple(t, "device:p-delta#owner@user:dana")))

		cases := []struct {
			name    string
			subject string
			device  string
			action  Action
			want    bool
		}{
			{
				name:    "an owner who holds operator flashes their own device",
				subject: "user:carol",
				device:  "p-beta",
				action:  ActionConfigure,
				want:    true,
			},
			{
				name:    "a team operator who owns nothing does not flash the same device",
				subject: "user:alice",
				device:  "p-beta",
				action:  ActionConfigure,
				want:    false,
			},
			{
				name:    "and that operator still commands it",
				subject: "user:alice",
				device:  "p-beta",
				action:  ActionOperate,
				want:    true,
			},
			{
				name:    "an owner does not reach a device they do not own",
				subject: "user:carol",
				device:  "p-alpha",
				action:  ActionConfigure,
				want:    false,
			},
			{
				name:    "ownership alone carries the commands",
				subject: "user:dana",
				device:  "p-delta",
				action:  ActionOperate,
				want:    true,
			},
			{
				name:    "ownership alone carries provision",
				subject: "user:dana",
				device:  "p-delta",
				action:  ActionConfigure,
				want:    true,
			},
			{
				name:    "an owner does not administer the grants on their device",
				subject: "user:dana",
				device:  "p-delta",
				action:  ActionAdminister,
				want:    false,
			},
			{
				name:    "an org admin flashes a device nobody owns",
				subject: "user:root",
				device:  "p-alpha",
				action:  ActionConfigure,
				want:    true,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				got, err := store.Can(t.Context(), subjectOf(t, tc.subject), tc.action, deviceOf(t, tc.device))
				require.NoError(t, err)
				require.Equalf(t, tc.want, got, "%s %s on %s", tc.subject, tc.action, tc.device)
			})
		}
	})
}

// A block covers every action on the device, and ownership is added after the
// role layer is resolved. So the deny has to reach it too: a blocked owner who
// still commanded the machine would be the one action somebody forgot.
func TestABlockedOwnerReachesNothing(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:p-beta#owner@user:carol")))

		reached, err := store.ObjectActions(t.Context(), subjectOf(t, "user:carol"), deviceOf(t, "p-beta"))
		require.NoError(t, err)
		require.Contains(t, reached, ActionConfigure, "the owner grant did not take effect")

		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:p-beta#blocked@user:carol")))

		reached, err = store.ObjectActions(t.Context(), subjectOf(t, "user:carol"), deviceOf(t, "p-beta"))
		require.NoError(t, err)
		require.Empty(t, reached, "a blocked owner still reaches the device")
	})
}

// Ownership is assigned by whoever administers the device, and never by the
// owner. ownerMask stops short of manage, so an owner who holds nothing else
// cannot write a grant on their own device: that is what stops ownership
// spreading without an administrator.
func TestAnOwnerCannotHandOwnershipOn(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:p-delta#owner@user:dana")))

		_, err := store.WriteGrant(t.Context(), subjectOf(t, "user:dana"),
			mustTuple(t, "device:p-delta#owner@user:mallory"))
		requireCode(t, err, accessDenied.Code())

		// The administrator's half. An org admin holds the whole manager mask
		// on every device in the org, so ownership is theirs to assign, which
		// is the only path this build has: nothing in the core approves a
		// registration, so nothing writes an owner tuple on its own.
		written, err := store.WriteGrant(t.Context(), subjectOf(t, "user:root"),
			mustTuple(t, "device:p-delta#owner@user:mallory"))
		require.NoError(t, err)
		require.True(t, written)

		got, err := store.Can(t.Context(), subjectOf(t, "user:mallory"), ActionConfigure, deviceOf(t, "p-delta"))
		require.NoError(t, err)
		require.True(t, got, "the grant an org admin wrote conferred nothing")
	})
}

// Ownership is a second thing the projection has to get right, so it is swept
// against the oracle like every other path. The oracle transcribes the owner
// rule from the package doc by hand and shares no table with the compiler.
func TestOwnershipMatchesTheGraphWalk(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:p-beta#owner@user:carol"),
			mustTuple(t, "device:p-beta#operator@user:carol"),
			mustTuple(t, "device:p-delta#owner@group:maintenance#member"),
			mustTuple(t, "device:p-gamma#owner@user:olga")))

		reference := newOracle(t, store)

		for _, name := range append(fixturePrincipals, "user:carol") {
			who := subjectOf(t, name)

			for _, action := range printerActions() {
				for _, id := range append(directoryIDs(), "p-hidden") {
					got, err := store.Can(t.Context(), who, action, deviceOf(t, id))
					require.NoError(t, err)
					require.Equalf(t, reference.can(who, action, id), got,
						"%s %s on device:%s", name, action, id)
				}
			}
		}
	})
}

// ownerMask is inside the manager mask, which is the invariant the escalation
// rule rests on: a caller who may administer a device holds the whole manager
// mask, so writing an owner tuple can never hand out more than they have.
func TestOwnershipIsInsideTheManagerMask(t *testing.T) {
	t.Parallel()

	require.Zero(t, ownerMask&^levelManager.deviceMask(),
		"owning a device reaches an action its manager does not, so nobody can be allowed to write the edge")
	require.NotZero(t, ownerMask&maskConfigure,
		"ownership does not carry configure, which is the whole of the 2026-08-27 decision")
	require.Zero(t, ownerMask&maskAdminister,
		"an owner administers the grants on their own device, which ADR-20260825-z65 rule 31 stops short of")
	require.Zero(t, ownerMask&levelManager.resourceMask(), "ownership of a device reaches a resource action")

	_, isRole := roleLevel(RelOwner)
	require.False(t, isRole, "owner is a role, so it raises a level instead of adding to one")
}
