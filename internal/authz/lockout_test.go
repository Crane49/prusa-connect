package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// TestACallerMayNotWithdrawTheirOwnLastAdministeringGrant is the withdrawal
// door. dana holds one grant on the storage kind and reaches it by no other
// path, so withdrawing that grant leaves nobody able to write it back: the
// first grant is written once, and re-attaching one takes the action the
// withdrawal removed.
func TestACallerMayNotWithdrawTheirOwnLastAdministeringGrant(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		dana := subjectOf(t, "user:dana")
		storage := kindOf(t, ref.KindStorage)

		err := store.WithdrawGrant(t.Context(), dana,
			mustTuple(t, "resource_kind:storage#manager@user:dana"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(), dana, ActionConfigureResource, storage)
		require.NoError(t, err)
		require.True(t, allowed, "a refused withdrawal removed the caller's own access anyway")
	})
}

// TestACallerMayNotBlockThemselvesOutOfAnObject is the other door to the same
// outcome. A block confers nothing and removes the whole role layer from
// whoever it names, so it reaches the lockout without touching the tuple the
// caller administers with.
func TestACallerMayNotBlockThemselvesOutOfAnObject(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		dana := subjectOf(t, "user:dana")
		storage := kindOf(t, ref.KindStorage)

		_, err := store.WriteGrant(t.Context(), dana,
			mustTuple(t, "resource_kind:storage#blocked@user:dana"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(), dana, ActionConfigureResource, storage)
		require.NoError(t, err)
		require.True(t, allowed, "a refused block removed the caller's own access anyway")
	})
}

// TestABlockOnTheCallerIsRefusedEvenWhenAnOrgConfersTheirAccess says what the
// two doors do not share. A withdrawal takes one tuple away and leaves every
// other path intact; a block deletes the principal from the role layer whatever
// wrote it, so an org above the object does not survive one. The rule reads the
// outcome rather than the relation, which is how one rule covers both.
func TestABlockOnTheCallerIsRefusedEvenWhenAnOrgConfersTheirAccess(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")
		storage := kindOf(t, ref.KindStorage)

		_, err := store.WriteGrant(t.Context(), root,
			mustTuple(t, "resource_kind:storage#blocked@user:root"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(), root, ActionConfigureResource, storage)
		require.NoError(t, err)
		require.True(t, allowed, "the org admin blocked themselves out of the kind they administer")
	})
}

// TestACallerMayWithdrawADirectGrantAnOrgStillConfers is the ordinary cleanup
// the rule must not refuse. root administers the storage kind as the org's
// admin and holds a direct grant beside it; withdrawing the direct grant costs
// them nothing, because the org still reaches the kind. Refusing this is the
// false positive an operator meets first, and the standing split the projection
// carries is what tells it from a lockout.
func TestACallerMayWithdrawADirectGrantAnOrgStillConfers(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")
		storage := kindOf(t, ref.KindStorage)

		wrote, err := store.WriteGrant(t.Context(), root,
			mustTuple(t, "resource_kind:storage#manager@user:root"))
		require.NoError(t, err)
		require.True(t, wrote)

		require.NoError(t, store.WithdrawGrant(t.Context(), root,
			mustTuple(t, "resource_kind:storage#manager@user:root")),
			"an ordinary cleanup of a redundant grant was refused as a lockout")

		allowed, err := store.Can(t.Context(), root, ActionConfigureResource, storage)
		require.NoError(t, err)
		require.True(t, allowed, "the org no longer reaches the kind")
	})
}

// TestAnAdministratorReachingAnObjectThroughAnOrgMayStillBlockSomebodyElse is
// the write door's permit half. The rule bounds what a mutation does to the
// caller and must say nothing about a block written on anybody else, or the
// deny relation becomes unwritable by the callers who inherit their access.
func TestAnAdministratorReachingAnObjectThroughAnOrgMayStillBlockSomebodyElse(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")
		dana := subjectOf(t, "user:dana")
		storage := kindOf(t, ref.KindStorage)

		wrote, err := store.WriteGrant(t.Context(), root,
			mustTuple(t, "resource_kind:storage#blocked@user:dana"))
		require.NoError(t, err)
		require.True(t, wrote)

		allowed, err := store.Can(t.Context(), dana, ActionConfigureResource, storage)
		require.NoError(t, err)
		require.False(t, allowed, "the block reached no projection")

		allowed, err = store.Can(t.Context(), root, ActionConfigureResource, storage)
		require.NoError(t, err)
		require.True(t, allowed, "blocking somebody else cost the caller their own access")
	})
}

// TestACallerMayNotSeverTheOrgTheyAdministerThrough is the third shape of the
// same outcome, and the reason the rule is written on what a mutation produces
// rather than on which relation it names. Withdrawing the kind's org tuple
// names neither the caller nor a role, and it removes every org role from the
// kind, the caller's own included.
func TestACallerMayNotSeverTheOrgTheyAdministerThrough(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")
		storage := kindOf(t, ref.KindStorage)

		err := store.WithdrawGrant(t.Context(), root,
			mustTuple(t, "resource_kind:storage#org@org:acme"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(), root, ActionConfigureResource, storage)
		require.NoError(t, err)
		require.True(t, allowed, "a refused withdrawal severed the org anyway")
	})
}

// TestAHandoverSucceedsWhenTheSuccessorIsGrantedFirst is the order that works.
// The departing administrator grants their successor, and the successor
// withdraws the departing grant. Both calls keep their own caller administering
// the object, so the rule permits each one, and the object is never left with
// nobody able to write a grant on it.
//
// The other order is the lockout: withdrawing first is the call
// TestACallerMayNotWithdrawTheirOwnLastAdministeringGrant refuses.
func TestAHandoverSucceedsWhenTheSuccessorIsGrantedFirst(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		dana := subjectOf(t, "user:dana")
		mallory := subjectOf(t, "user:mallory")
		storage := kindOf(t, ref.KindStorage)

		wrote, err := store.WriteGrant(t.Context(), dana,
			mustTuple(t, "resource_kind:storage#manager@user:mallory"))
		require.NoError(t, err)
		require.True(t, wrote, "the departing administrator could not grant a successor")

		require.NoError(t, store.WithdrawGrant(t.Context(), mallory,
			mustTuple(t, "resource_kind:storage#manager@user:dana")),
			"the successor could not withdraw the departing grant")

		allowed, err := store.Can(t.Context(), dana, ActionConfigureResource, storage)
		require.NoError(t, err)
		require.False(t, allowed, "the departing administrator still reaches the kind")

		allowed, err = store.Can(t.Context(), mallory, ActionConfigureResource, storage)
		require.NoError(t, err)
		require.True(t, allowed, "the successor does not administer what they were handed")
	})
}

// TestTheLockoutRefusalNamesWhatItProtects holds the usability half. A caller
// who is refused here is holding the only grant that reaches the object, and a
// bare denial tells them to ask an administrator who does not exist. The
// refusal names the object, the action it is protecting, and the order that
// works.
func TestTheLockoutRefusalNamesWhatItProtects(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		dana := subjectOf(t, "user:dana")

		err := store.WithdrawGrant(t.Context(), dana,
			mustTuple(t, "resource_kind:storage#manager@user:dana"))

		var refusal *fault.Fault
		require.ErrorAs(t, err, &refusal)

		require.Contains(t, refusal.Message(), "the caller's own ability to administer")
		require.Contains(t, refusal.Message(), "no other grant confers it")
		require.Contains(t, refusal.Message(), "grant another administrator first")

		object, ok := refusal.Params().Get(paramNameObject)
		require.True(t, ok, "the refusal names no object")
		require.Equal(t, "resource_kind:storage", object.String())

		action, ok := refusal.Params().Get(paramNameAction)
		require.True(t, ok, "the refusal names no action")
		require.Equal(t, ActionConfigureResource.String(), action.String())
	})
}

// TestAPrinterManagerMayNotWithdrawTheirOwnLastGrantOnIt runs the rule on the
// other administrable object type, because the two projections are separate
// compilers and a rule proved on one of them is proved on half the surface.
func TestAPrinterManagerMayNotWithdrawTheirOwnLastGrantOnIt(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")
		dana := subjectOf(t, "user:dana")
		delta := deviceOf(t, "p-delta")

		wrote, err := store.WriteGrant(t.Context(), root,
			mustTuple(t, "device:p-delta#manager@user:dana"))
		require.NoError(t, err)
		require.True(t, wrote)

		err = store.WithdrawGrant(t.Context(), dana,
			mustTuple(t, "device:p-delta#manager@user:dana"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(), dana, ActionAdminister, delta)
		require.NoError(t, err)
		require.True(t, allowed, "a refused withdrawal removed the delegate's own access anyway")

		require.NoError(t, store.WithdrawGrant(t.Context(), root,
			mustTuple(t, "device:p-delta#manager@user:dana")),
			"the org admin could not take back what they delegated")
	})
}
