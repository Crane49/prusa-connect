package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// TestADelegateCannotBlockTheAdministratorWhoDelegated is the first half of the
// rule that bounds subtraction. dana holds one grant written on the storage
// kind and nothing on the org above it; root reaches the kind as the org's
// admin. A block confers nothing, so the escalation rule passes it, and it
// takes the whole role layer from whoever it names: without this rule dana
// strips root in one call and root cannot undo it, because undoing it takes the
// action the block just removed.
func TestADelegateCannotBlockTheAdministratorWhoDelegated(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		dana := subjectOf(t, "user:dana")
		root := subjectOf(t, "user:root")
		storage := kindOf(t, ref.KindStorage)

		_, err := store.WriteGrant(t.Context(), dana,
			mustTuple(t, "resource_kind:storage#blocked@user:root"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(), root, ActionConfigureResource, storage)
		require.NoError(t, err)
		require.True(t, allowed, "a refused block took the administrator's access anyway")

		held, err := store.Grants(t.Context(), root, storage)
		require.NoError(t, err)
		require.NotEmpty(t, held, "the administrator can no longer read the grants they administer")
	})
}

// TestADelegateCannotBlockAnAdministratorThroughAGroup is the same refusal
// reached through a userset. The block names a team, and what a block removes
// is every principal the team resolves to, so a rule that reads the subject of
// the tuple rather than the principals behind it would pass this one.
func TestADelegateCannotBlockAnAdministratorThroughAGroup(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		dana := subjectOf(t, "user:dana")
		root := subjectOf(t, "user:root")
		storage := kindOf(t, ref.KindStorage)

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "group:maintenance#member@user:root")))

		_, err := store.WriteGrant(t.Context(), dana,
			mustTuple(t, "resource_kind:storage#blocked@group:maintenance#member"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(), root, ActionConfigureResource, storage)
		require.NoError(t, err)
		require.True(t, allowed, "a block naming a team took an administrator's access")
	})
}

// TestADelegateCannotSeverTheOrgTheyWereDelegatedThrough is the second half.
// root delegates one printer to dana, and dana withdraws the tuple that
// attaches the printer to the org. p-delta is in no printer group, so that
// tuple is the org's only path to it: severing it removes every org role from
// the printer while leaving dana's own grant, which is written on the printer,
// in place. root cannot re-attach it, because writing a grant on a printer
// takes manage on that printer.
func TestADelegateCannotSeverTheOrgTheyWereDelegatedThrough(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		dana := subjectOf(t, "user:dana")
		root := subjectOf(t, "user:root")
		delta := deviceOf(t, "p-delta")

		wrote, err := store.WriteGrant(t.Context(), root,
			mustTuple(t, "device:p-delta#manager@user:dana"))
		require.NoError(t, err)
		require.True(t, wrote, "the org admin could not delegate the printer")

		err = store.WithdrawGrant(t.Context(), dana, mustTuple(t, "device:p-delta#org@org:acme"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(), root, ActionAdminister, delta)
		require.NoError(t, err)
		require.True(t, allowed, "a refused withdrawal severed the org anyway")
	})
}

// TestAnAdministratorMayRemoveWhatFlowsFromAnOrgTheyAdmin is the permit half of
// the same rule, and the case the deny relation exists for: "org viewer, except
// these printers" is one block on a principal whose org role the caller holds
// more of. A rule that refused every block on inherited access would refuse
// this one and leave the vocabulary with a relation nothing can write.
func TestAnAdministratorMayRemoveWhatFlowsFromAnOrgTheyAdmin(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")
		olga := subjectOf(t, "user:olga")
		beta := deviceOf(t, "p-beta")

		allowed, err := store.Can(t.Context(), olga, ActionObserve, beta)
		require.NoError(t, err)
		require.True(t, allowed, "the org viewer does not reach the printer the block is about")

		wrote, err := store.WriteGrant(t.Context(), root, mustTuple(t, "device:p-beta#blocked@user:olga"))
		require.NoError(t, err)
		require.True(t, wrote)

		allowed, err = store.Can(t.Context(), olga, ActionObserve, beta)
		require.NoError(t, err)
		require.False(t, allowed, "the block reached no projection")
	})
}

// TestAManagerMayWithdrawWhatIsWrittenOnTheObject is the other permit half, and
// the one a delegate meets: dana inherits nothing on p-delta and still
// administers everything written there, a printer credential included. Revoking
// an agent tuple is how a stolen printer credential is withdrawn, so a rule
// that bounded a removal by the caller's whole mask would have taken that away.
func TestAManagerMayWithdrawWhatIsWrittenOnTheObject(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		dana := subjectOf(t, "user:dana")
		root := subjectOf(t, "user:root")

		wrote, err := store.WriteGrant(t.Context(), root,
			mustTuple(t, "device:p-delta#manager@user:dana"))
		require.NoError(t, err)
		require.True(t, wrote)

		require.NoError(t, store.WithdrawGrant(t.Context(), dana,
			mustTuple(t, "device:p-delta#operator@service_account:sa-ui")))

		allowed, err := store.Can(t.Context(),
			subjectOf(t, "service_account:sa-ui"), ActionOperate, deviceOf(t, "p-delta"))
		require.NoError(t, err)
		require.False(t, allowed, "a withdrawn grant is still in the projection")

		require.NoError(t, store.WithdrawGrant(t.Context(), root,
			mustTuple(t, "device:p-alpha#agent@device_credential:sn-alpha")))

		allowed, err = store.Can(t.Context(),
			subjectOf(t, "device_credential:sn-alpha"), ActionReportTelemetry, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.False(t, allowed, "a printer credential can no longer be revoked")
	})
}

// TestEveryRelationConfersWhatItsSubjectReaches holds the invariant the
// escalation rule rests on, which nothing held before: conferred() is compared
// against the projection compiler for every relation the vocabulary defines on
// an object grants can be administered on, so a conferral that stopped matching
// what the graph hands out is a failure here rather than a hole in WriteGrant.
//
// Two claims, both load-bearing. Every relation but agent confers inside the
// manager mask, which is what makes a role grant safe for any caller who may
// write here at all; agent confers outside it, which is why the check exists.
func TestEveryRelationConfersWhatItsSubjectReaches(t *testing.T) {
	t.Parallel()

	for _, spec := range relationSpecs {
		t.Run(spec.Object.String()+"#"+spec.Relation.String(), func(t *testing.T) {
			t.Parallel()

			gives := conferred(spec.Object, spec.Relation)

			if _, administrable := AdministerAction(spec.Object); !administrable {
				require.Zerof(t, gives,
					"%s carries no action, so a grant on it confers nothing this surface can bound", spec.Object)

				return
			}

			object := probeObject(t, spec.Object)

			tuples, subject, ok := conferralProbe(spec.Relation, object)
			require.Truef(t, ok, "relation %s on %s has no conferral probe", spec.Relation, spec.Object)

			reached := probeReach(t, object, tuples, subject)
			require.Equalf(t, reached, gives,
				"conferred(%s, %s) is not what the compiler hands the subject of that grant",
				spec.Object, spec.Relation)

			if spec.Relation == RelAgent {
				require.NotZerof(t, gives&^managerMaskOf(spec.Object),
					"agent confers inside the manager mask, so nothing stops a session minting one")

				return
			}

			require.Zerof(t, gives&^managerMaskOf(spec.Object),
				"%s#%s confers outside the manager mask, which a caller who may write it is not held to",
				spec.Object, spec.Relation)
		})
	}
}

// conferralProbe is the smallest graph that makes one principal reach an object
// through one relation, and the principal to look for. It is written out per
// relation rather than derived from the compiler, because a probe derived from
// the thing under test proves nothing. A relation the vocabulary gains has to
// be given one here before the property test will pass.
func conferralProbe(relation Relation, object Object) ([]string, string, bool) {
	const (
		user   = "user:probe"
		device = "device_credential:probe"
	)

	edge := object.String() + relationSeparator + relation.String() + "@"

	switch relation {
	case RelViewer, RelOperator, RelManager, RelBlocked, RelOwner:
		return []string{edge + user}, user, true
	case RelAgent:
		return []string{edge + device}, device, true
	case RelOrg:
		return []string{edge + "org:probe", "org:probe#admin@" + user}, user, true
	case RelGroup:
		return []string{edge + "device_group:probe", "device_group:probe#manager@" + user}, user, true
	case RelParent:
		// The device-to-device edge, which is a camera and the printer it
		// watches (ADR-20260826-5yj rule 15). Writing it hands whoever reaches the
		// parent the same mask on the child, so the probe puts a manager on
		// the parent and looks for them on the child. device_group#parent
		// never asks for a probe: a device group carries no action, so the
		// property test returns before it gets here.
		return []string{edge + "device:p-parent", "device:p-parent#manager@" + user}, user, true
	case RelMember, RelAdmin:
		return nil, "", false
	default:
		return nil, "", false
	}
}

// probeObject is the object a conferral probe is written on: one registered
// printer, or one kind of the closed configurable set.
func probeObject(tb testing.TB, typ Type) Object {
	tb.Helper()

	if typ == TypeResourceKind {
		return kindOf(tb, ref.KindStorage)
	}

	return deviceOf(tb, "p-probe")
}

// probeReach compiles the probe graph and returns what the subject holds on the
// object, through the same compiler every projected row comes out of.
func probeReach(tb testing.TB, object Object, tuples []string, subject string) actionSet {
	tb.Helper()

	compiled := project(mustTuples(tb, tuples...), printerDirectory("p-probe"), configurableKinds(), verbIndex{})
	who := principal{typ: subjectOf(tb, subject).typ, id: subjectOf(tb, subject).id}

	for _, row := range compiled.Devices {
		if row.Subject == who && row.Device == object.id {
			return row.Actions
		}
	}

	for _, row := range compiled.Resources {
		if row.Subject == who && row.Kind == object.id {
			return row.Actions
		}
	}

	return 0
}

// managerMaskOf is everything the top of the role layer reaches on one object
// type, spelled out here rather than taken from the code under test.
func managerMaskOf(typ Type) actionSet {
	switch typ {
	case TypeDevice:
		return maskObserve | maskOperate | maskConfigure | maskAdminister | maskViewAudit
	case TypeResourceKind:
		return maskViewResource | maskOperateResource | maskConfigureResource |
			maskReadStorage | maskWriteStorage | maskAdmitDevice | maskDevelopDevice
	case TypeUser, TypeGroup, TypeServiceAccount, TypeDeviceCredential, TypeNode, TypeSink,
		TypeHistory, TypeOrg, TypeDeviceGroup:
		return 0
	default:
		return 0
	}
}
