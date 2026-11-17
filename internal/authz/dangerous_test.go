package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The two holes measured against a running server on 2026-09-01, each written
// as the sequence that measured it.

// A device manager wrote themselves the owner relation and got the whole
// dangerous tier with it: the projection said owner confers nothing a manager
// lacks, because what owner confers beyond the mask is not in the mask.
func TestAManagerCannotWriteThemselvesTheOwnerOfADevice(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		// mo manages p-beta and owns nothing. This is the manager the live run
		// used: no verb row anywhere.
		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-beta#manager@user:mo")))

		beta := deviceOf(t, "p-beta")
		mo := subjectOf(t, "user:mo")

		// The manager administers the device, which is what made the write look
		// legitimate to the mask comparison.
		administers, err := store.Can(t.Context(), mo, ActionAdminister, beta)
		require.NoError(t, err)
		require.True(t, administers, "the fixture does not reproduce the live manager")

		_, err = store.WriteGrant(t.Context(), mo, mustTuple(t, "device:p-beta#owner@user:mo"))
		requireCode(t, err, accessDenied.Code())

		// And the two verbs the live run reached afterwards are still out of
		// reach, which is the whole point of refusing the write.
		for _, verb := range []string{verbForceGCode, "core.command.set_token"} {
			reached, verbErr := store.CanVerb(t.Context(), mo, beta, verb)
			require.NoError(t, verbErr)
			require.Falsef(t, reached, "a refused ownership write still conferred %s", verb)
		}
	})
}

// A grant of ownership to somebody else is the same escalation written one step
// further out, so it is refused in the same words.
func TestAManagerCannotHandOwnershipToAnybodyElse(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-beta#manager@user:mo")))

		_, err := store.WriteGrant(t.Context(),
			subjectOf(t, "user:mo"), mustTuple(t, "device:p-beta#owner@user:bob"))
		requireCode(t, err, accessDenied.Code())
	})
}

// An admin of an org the device is in writes ownership on it. Somebody has to,
// or a machine that arrives with no owner never gets one.
//
// The owner does not, and that is ADR-20260825-z65 rule 31 rather than an omission
// here: ownerMask stops short of administer exactly so that whoever administers
// the device says who owns it and the owner cannot hand ownership on.
func TestOwnershipIsWrittenByAnOrgAdminAndNotPassedOnByTheOwner(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		// root is the org admin of acme, and p-beta is in acme.
		wrote, err := store.WriteGrant(t.Context(),
			subjectOf(t, "user:root"), mustTuple(t, "device:p-beta#owner@user:dana"))
		require.NoError(t, err)
		require.True(t, wrote)

		_, err = store.WriteGrant(t.Context(),
			subjectOf(t, "user:dana"), mustTuple(t, "device:p-beta#owner@user:bob"))
		requireCode(t, err, accessDenied.Code())
	})
}

// The dangerous tier had no bootstrap: the owner held the verbs and not
// administer, and everybody who held administer held none of the verbs, so the
// only working route was two hand-written tuples.
func TestTheOwnerAndTheOrgAdminBootstrapTheDangerousTier(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		require.NoError(t, store.Write(t.Context(),
			mustTuple(t, "device:p-beta#owner@user:dana"),
			mustTuple(t, "device:p-beta#operator@user:bob"),
			mustTuple(t, "device:p-beta#manager@user:mo")))

		beta := deviceOf(t, "p-beta")

		grant, err := NewVerbGrant(beta, verbForceGCode, subjectOf(t, "user:bob"))
		require.NoError(t, err)

		// A manager who neither owns the device nor administers its org holds
		// administer and no reach, and confers nothing.
		_, err = store.WriteVerbGrant(t.Context(), subjectOf(t, "user:mo"), grant)
		requireCode(t, err, accessDenied.Code())

		// The owner holds the reach and not administer, and is who hands the
		// verb out on their own machine.
		wrote, err := store.WriteVerbGrant(t.Context(), subjectOf(t, "user:dana"), grant)
		require.NoError(t, err)
		require.True(t, wrote)

		reached, err := store.CanVerb(t.Context(), subjectOf(t, "user:bob"), beta, verbForceGCode)
		require.NoError(t, err)
		require.True(t, reached, "the operator the owner granted does not reach the verb")

		// The owner can take it back, or nobody could undo what they wrote.
		require.NoError(t, store.WithdrawVerbGrant(t.Context(), subjectOf(t, "user:dana"), grant))

		reached, err = store.CanVerb(t.Context(), subjectOf(t, "user:bob"), beta, verbForceGCode)
		require.NoError(t, err)
		require.False(t, reached, "a withdrawn row still confers the verb")

		// An org admin may do the same on any machine of the org, which is what
		// gives a fleet an administrator who is not the owner of every printer.
		wrote, err = store.WriteVerbGrant(t.Context(), subjectOf(t, "user:root"), grant)
		require.NoError(t, err)
		require.True(t, wrote)

		reached, err = store.CanVerb(t.Context(), subjectOf(t, "user:bob"), beta, verbForceGCode)
		require.NoError(t, err)
		require.True(t, reached, "the org admin's row conferred nothing")
	})
}

// A device with no org and no owner is nobody's to hand out, which is the state
// a camera bound to a printer is in. It fails closed rather than falling back
// on whoever administers the parent.
func TestADeviceWithNoOwnerAndNoOrgHasNoDangerousTierBootstrap(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		_, err := store.WriteGrant(t.Context(),
			subjectOf(t, "user:root"), mustTuple(t, "device:cam-alpha#owner@user:root"))
		requireCode(t, err, accessDenied.Code())
	})
}
