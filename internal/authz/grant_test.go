package authz

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// TestOnlyTheManagerOfAnObjectAdministersItsGrants is the rule the whole
// surface rests on, in both directions. dana is manager on the storage kind and
// may read and write the grants on it; bob holds a viewer grant on the camera
// kind and may do neither there.
func TestOnlyTheManagerOfAnObjectAdministersItsGrants(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		storage := kindOf(t, ref.KindStorage)
		camera := kindOf(t, ref.KindCamera)
		dana := subjectOf(t, "user:dana")
		bob := subjectOf(t, "user:bob")

		held, err := store.Grants(t.Context(), dana, storage)
		require.NoError(t, err)
		require.NotEmpty(t, held, "the manager of a kind reads the grants written on it")

		wrote, err := store.WriteGrant(t.Context(), dana,
			mustTuple(t, "resource_kind:storage#viewer@user:mallory"))
		require.NoError(t, err)
		require.True(t, wrote)

		allowed, err := store.Can(t.Context(), subjectOf(t, "user:mallory"), ActionViewResource, storage)
		require.NoError(t, err)
		require.True(t, allowed, "a grant written through the graph reached no projection")

		_, err = store.Grants(t.Context(), bob, camera)
		requireCode(t, err, CodeAccessDenied())

		_, err = store.WriteGrant(t.Context(), bob,
			mustTuple(t, "resource_kind:camera#viewer@user:mallory"))
		requireCode(t, err, CodeAccessDenied())

		err = store.WithdrawGrant(t.Context(), bob,
			mustTuple(t, "resource_kind:camera#viewer@user:bob"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err = store.Can(t.Context(), bob, ActionViewResource, camera)
		require.NoError(t, err)
		require.True(t, allowed, "a refused withdrawal took the grant away anyway")
	})
}

// TestGrantRefusalsHideWhetherTheObjectExists is this project's third standing
// suspicion on the grant surface, and the sibling of
// TestAbsenceIsIndistinguishableFromNoAccess. p-alpha is registered and carries four
// grants, p-nowhere is nothing at all, and a caller who holds neither must not
// be able to tell them apart.
//
// The two refusals are compared as the rendered problem document, because a
// difference in the code, the status, the title or one parameter is the whole
// leak.
func TestGrantRefusalsHideWhetherTheObjectExists(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		mallory := subjectOf(t, "user:mallory")

		for name, call := range map[string]func(object Object) error{
			"list": func(object Object) error {
				_, err := store.Grants(t.Context(), mallory, object)

				return err
			},
			"write": func(object Object) error {
				grant, err := NewTuple(object, RelViewer, mallory)
				require.NoError(t, err)

				_, err = store.WriteGrant(t.Context(), mallory, grant)

				return err
			},
			"withdraw": func(object Object) error {
				grant, err := NewTuple(object, RelViewer, mallory)
				require.NoError(t, err)

				return store.WithdrawGrant(t.Context(), mallory, grant)
			},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				hidden := problemOf(t, call(deviceOf(t, "p-alpha")))
				missing := problemOf(t, call(deviceOf(t, "p-nowhere")))

				require.Equal(t,
					strings.ReplaceAll(missing, "p-nowhere", "<id>"),
					strings.ReplaceAll(hidden, "p-alpha", "<id>"),
					"the refusal for an object the caller may not administer must read as the "+
						"refusal for one that is not there")
			})
		}
	})
}

// TestACallerCannotConferWhatTheyDoNotHold is the escalation half. The manager
// action is what administering grants takes, so a role grant is inside what the
// writer holds by construction; an agent tuple is not, because report_telemetry
// and fetch_job are in no role at all.
func TestACallerCannotConferWhatTheyDoNotHold(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")

		wrote, err := store.WriteGrant(t.Context(), root,
			mustTuple(t, "device:p-alpha#manager@user:mallory"))
		require.NoError(t, err)
		require.True(t, wrote, "an org admin may confer the level they hold")

		_, err = store.WriteGrant(t.Context(), root,
			mustTuple(t, "device:p-alpha#agent@device_credential:sn-forged"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(),
			subjectOf(t, "device_credential:sn-forged"), ActionReportTelemetry, deviceOf(t, "p-alpha"))
		require.NoError(t, err)
		require.False(t, allowed, "a refused grant reached the projection")
	})
}

// TestACallerCannotGrantThemselvesMore is the same rule from the other end, and
// the one an operator meets: holding a viewer grant is not permission to write
// a manager grant, however carefully the request is addressed.
func TestACallerCannotGrantThemselvesMore(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		bob := subjectOf(t, "user:bob")

		_, err := store.WriteGrant(t.Context(), bob,
			mustTuple(t, "resource_kind:camera#manager@user:bob"))
		requireCode(t, err, CodeAccessDenied())

		allowed, err := store.Can(t.Context(), bob, ActionConfigureResource, kindOf(t, ref.KindCamera))
		require.NoError(t, err)
		require.False(t, allowed, "a caller granted themselves an action they did not hold")
	})
}

// TestGrantsOnATypeThisSurfaceCannotDecideAreRefused holds the boundary of this
// surface. A group and a printer group carry grants and no owning level this
// surface can read, so nothing can authorize a write on one and the refusal
// says so rather than answering a permission question with a lookup that
// misses.
//
// An org is not here any more. It carries no action either, and it is decided
// through the admin role written on it, which is what Administrable reports and
// what TestOnlyAnOrgAdminAdministersAnOrg holds.
func TestGrantsOnATypeThisSurfaceCannotDecideAreRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")

		for _, object := range []Object{
			objectOf(t, TypeGroup, "maintenance"),
			objectOf(t, TypeDeviceGroup, "floor-one"),
		} {
			_, err := store.Grants(t.Context(), root, object)
			requireCode(t, err, CodeUnsupportedObject())
		}
	})
}

// TestAGrantWrittenThroughTheGrantSurfaceMovesTheEpoch is E10's half of this
// change: a live subscription re-authorizes when the epoch moves, so a write
// that changed the graph and left the generation where it was is a revocation
// nobody notices. The repeat is the other half, because invalidating every
// cached decision in the deployment for a no-op is its own defect.
func TestAGrantWrittenThroughTheGrantSurfaceMovesTheEpoch(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		dana := subjectOf(t, "user:dana")
		grant := mustTuple(t, "resource_kind:storage#viewer@user:mallory")

		before := store.Epoch()

		wrote, err := store.WriteGrant(t.Context(), dana, grant)
		require.NoError(t, err)
		require.True(t, wrote)
		require.Greater(t, store.Epoch(), before, "a grant that changed the graph moved no generation")

		written := store.Epoch()

		wrote, err = store.WriteGrant(t.Context(), dana, grant)
		require.NoError(t, err)
		require.False(t, wrote, "a grant the graph already held reported itself as new")
		require.Equal(t, written, store.Epoch(), "a write that changed nothing moved the generation")

		require.NoError(t, store.WithdrawGrant(t.Context(), dana, grant))
		require.Greater(t, store.Epoch(), written, "a withdrawal moved no generation")

		allowed, err := store.Can(t.Context(),
			subjectOf(t, "user:mallory"), ActionViewResource, kindOf(t, ref.KindStorage))
		require.NoError(t, err)
		require.False(t, allowed, "a withdrawn grant is still in the projection")
	})
}

// TestTheGrantListIsEveryTupleOnTheObject reads the object rather than the
// caller: administering an object is seeing who else reaches it, structure
// tuples included, because those are how most principals reach it.
func TestTheGrantListIsEveryTupleOnTheObject(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		held, err := store.Grants(t.Context(), subjectOf(t, "user:root"), deviceOf(t, "p-alpha"))
		require.NoError(t, err)

		rendered := make([]string, 0, len(held))
		for _, grant := range held {
			rendered = append(rendered, grant.String())
		}

		require.Equal(t, []string{
			"device:p-alpha#agent@device_credential:sn-alpha",
			"device:p-alpha#group@device_group:cell-a",
			"device:p-alpha#manager@service_account:audit",
			"device:p-alpha#operator@service_account:sa-ui",
			"device:p-alpha#org@org:acme",
		}, rendered)
	})
}

// TestAdministerActionCoversEveryTypeExactlyOnce keeps the two halves of the
// vocabulary in step: a type an action is defined on is administrable, and one
// with no action is not. A new type that gained an action without reaching this
// map would be checkable everywhere and ungrantable here.
func TestAdministerActionCoversEveryTypeExactlyOnce(t *testing.T) {
	t.Parallel()

	for _, typ := range Types() {
		action, administrable := AdministerAction(typ)

		var defined bool

		for _, candidate := range Actions() {
			if domain, ok := candidate.domain(); ok && domain == typ {
				defined = true
			}
		}

		require.Equalf(t, defined, administrable,
			"type %s has actions defined on it and no administer action, or the reverse", typ)

		if !administrable {
			continue
		}

		domain, ok := action.domain()
		require.True(t, ok)
		require.Equalf(t, typ, domain, "the administer action of %s is defined on another type", typ)

		manager, operator := levelManager.deviceMask(), levelOperator.deviceMask()
		if typ == TypeResourceKind {
			manager, operator = levelManager.resourceMask(), levelOperator.resourceMask()
		}

		require.NotZerof(t, action.mask()&manager, "a manager of a %s cannot %s one", typ, action)
		require.Zerof(t, action.mask()&operator,
			"an operator of a %s can %s one, so administering its grants is not the top of the role layer",
			typ, action)
	}
}

func objectOf(tb testing.TB, typ Type, id string) Object {
	tb.Helper()

	out, err := NewObject(typ, id)
	require.NoError(tb, err)

	return out
}

// problemOf renders a refusal the way a client receives it, so two refusals are
// compared as the bytes a caller can look at rather than as two Go values.
func problemOf(tb testing.TB, err error) string {
	tb.Helper()

	require.Error(tb, err)

	var refusal *fault.Fault

	require.ErrorAs(tb, err, &refusal)

	encoded, marshalErr := json.Marshal(refusal.ProblemJSON("/api/v1/grants"))
	require.NoError(tb, marshalErr)

	return string(encoded)
}
