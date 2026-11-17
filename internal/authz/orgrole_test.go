package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// roleFixtureTuples is this server as it stands after a first run: one org, one
// administrator, and the org over the auth resource kind, which is what makes
// the administrator the person who may administer identity.
//
// olga is the colleague. She holds the viewer role and nothing else, so every
// case below is about what changes for her and about who is left holding admin.
var roleFixtureTuples = []string{
	"org:local#admin@user:root",
	"org:local#viewer@user:olga",
	"resource_kind:auth#org@org:local",
}

// seedRoleFixture is a graph holding the org roles this server hands out.
func seedRoleFixture(tb testing.TB, e engine) *Store {
	tb.Helper()

	store := newStore(tb, e)
	require.NoError(tb, store.Write(tb.Context(), mustTuples(tb, roleFixtureTuples...)...))

	return store
}

// rolesOf is every org role one account holds, read back off the graph. The
// reader is named because reading the org's grants takes the admin role itself,
// and a case that moves that role around has to ask somebody who still holds it.
func rolesOf(tb testing.TB, store *Store, reader, account string) []string {
	tb.Helper()

	org, err := NewObject(TypeOrg, bootstrapOrgName)
	require.NoError(tb, err)

	subject := subjectOf(tb, account)

	held, err := store.Grants(tb.Context(), subjectOf(tb, reader), org)
	require.NoError(tb, err)

	var out []string

	for _, tuple := range held {
		if tuple.subject == subject && IsOrgRole(tuple.relation) {
			out = append(out, tuple.relation.String())
		}
	}

	return out
}

// TestSettingARoleLeavesTheAccountHoldingExactlyOne is the whole of what the
// operation promises. A person who held one role holds the new one and not the
// old one, so a page that shows a role shows one and a permission check has one
// answer to reach.
func TestSettingARoleLeavesTheAccountHoldingExactlyOne(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedRoleFixture(t, e)
		root := subjectOf(t, "user:root")
		olga := subjectOf(t, "user:olga")

		require.NoError(t, store.SetOrgRole(t.Context(), root, olga, RelOperator))
		require.Equal(t, []string{"operator"}, rolesOf(t, store, "user:root", "user:olga"))

		require.NoError(t, store.SetOrgRole(t.Context(), root, olga, RelViewer))
		require.Equal(t, []string{"viewer"}, rolesOf(t, store, "user:root", "user:olga"))

		// Setting the role somebody already holds is the same answer twice,
		// so a client that retries is told what it was told the first time.
		require.NoError(t, store.SetOrgRole(t.Context(), root, olga, RelViewer))
		require.Equal(t, []string{"viewer"}, rolesOf(t, store, "user:root", "user:olga"))
	})
}

// TestAnAdministratorConfersTheAdminRole is the old backlog. The grant surface
// refuses this write and keeps refusing it; this operation is the one path that
// makes it, and the person it makes an administrator of is an administrator
// afterwards.
func TestAnAdministratorConfersTheAdminRole(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedRoleFixture(t, e)
		root := subjectOf(t, "user:root")
		olga := subjectOf(t, "user:olga")

		// The refusal on the grant surface, first, so this case reads as the
		// difference between the two paths and not as a write nobody refused.
		_, err := store.WriteGrant(t.Context(), root, mustTuple(t, "org:local#admin@user:olga"))
		requireCode(t, err, CodeAccessDenied())

		require.NoError(t, store.SetOrgRole(t.Context(), root, olga, RelAdmin))
		require.Equal(t, []string{"admin"}, rolesOf(t, store, "user:root", "user:olga"))

		// And she administers this server now: she may write a grant on the org,
		// which only an org admin may do.
		_, err = store.WriteGrant(t.Context(), olga, mustTuple(t, "org:local#viewer@user:mallory"))
		require.NoError(t, err)
	})
}

// TestTheLastAdministratorCannotGiveTheRoleUp is the door this operation opens
// and has to close behind it. A server whose org holds no admin has nobody who
// can add a person or change what one may do, and nothing short of editing the
// configuration and restarting puts an administrator back.
func TestTheLastAdministratorCannotGiveTheRoleUp(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedRoleFixture(t, e)
		root := subjectOf(t, "user:root")
		olga := subjectOf(t, "user:olga")

		err := store.SetOrgRole(t.Context(), root, root, RelViewer)
		requireCode(t, err, CodeLastAdministrator())
		require.Equal(t, []string{"admin"}, rolesOf(t, store, "user:root", "user:root"),
			"a refused demotion changed the graph")

		// With a second administrator the same call goes through, which is the
		// handover: grant the successor, then stand down.
		require.NoError(t, store.SetOrgRole(t.Context(), root, olga, RelAdmin))
		require.NoError(t, store.SetOrgRole(t.Context(), root, root, RelViewer))
		require.Equal(t, []string{"viewer"}, rolesOf(t, store, "user:olga", "user:root"))
	})
}

// TestSettingARoleTakesTheGrantThatAdministersIdentity is the permission. It is
// the one every other account operation takes, on the kind the accounts belong
// to, so there is no second answer to who may administer people here.
func TestSettingARoleTakesTheGrantThatAdministersIdentity(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedRoleFixture(t, e)
		olga := subjectOf(t, "user:olga")
		mallory := subjectOf(t, "user:mallory")

		// An org viewer holds no configure_resource on the auth kind, so she
		// may not put anybody on a role, least of all herself.
		err := store.SetOrgRole(t.Context(), olga, olga, RelAdmin)
		requireCode(t, err, CodeAccessDenied())
		require.Equal(t, []string{"viewer"}, rolesOf(t, store, "user:root", "user:olga"))

		err = store.SetOrgRole(t.Context(), mallory, olga, RelOperator)
		requireCode(t, err, CodeAccessDenied())
	})
}

// TestARoleThisServerDoesNotHaveIsRefused holds the vocabulary at the door. A
// relation the org has that is not a role, and a word that is no relation at
// all, are both refused, and neither reaches the graph.
func TestARoleThisServerDoesNotHaveIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedRoleFixture(t, e)
		root := subjectOf(t, "user:root")
		olga := subjectOf(t, "user:olga")

		for _, role := range []Relation{"wizard", "", RelMember, RelManager} {
			err := store.SetOrgRole(t.Context(), root, olga, role)
			requireCode(t, err, CodeUnknownRelation())
		}

		require.Equal(t, []string{"viewer"}, rolesOf(t, store, "user:root", "user:olga"))
	})
}

// TestARoleGrantedToASubjectTheRelationRefusesIsRefused is the shape rule the
// constructor already holds, read through this operation: a device credential is
// not a person and cannot hold a person's role.
func TestARoleGrantedToASubjectTheRelationRefusesIsRefused(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedRoleFixture(t, e)
		root := subjectOf(t, "user:root")

		err := store.SetOrgRole(
			t.Context(), root, subjectOf(t, "device_credential:sn-alpha"), RelAdmin)
		requireCode(t, err, CodeInvalidSubject())
	})
}
