package authz

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// scopedActions is what each scope may reach, written out by hand rather than
// read from the masks, so a mask that drifts is caught here.
var scopedActions = map[Scope][]Action{
	ScopeRead: {ActionObserve, ActionViewAudit, ActionViewResource, ActionReadStorage},
	ScopeOperate: {
		ActionObserve, ActionViewAudit, ActionViewResource, ActionReadStorage,
		ActionOperate, ActionOperateResource, ActionWriteStorage,
	},
	ScopeAdminister: Actions(),
}

// allowedBy reports whether the hand-written table lets a scope reach an action.
func allowedBy(scope Scope, action Action) bool {
	return slices.Contains(scopedActions[scope], action)
}

// Under every scope, every principal, every device and every device action
// answers what the graph answers and what the scope allows, both at once. A
// scope never adds an action the graph does not confer, and never lets through
// one it leaves out.
func TestAScopeCutsEveryDeviceDecisionDownToWhatItAllows(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, scope := range Scopes() {
			scoped := WithScopes(t.Context(), []Scope{scope})

			for _, principal := range fixturePrincipals {
				subject := subjectOf(t, principal)

				for _, action := range printerActions() {
					for _, device := range directoryIDs() {
						unscoped, err := store.Can(t.Context(), subject, action, deviceOf(t, device))
						require.NoError(t, err)

						got, err := store.Can(scoped, subject, action, deviceOf(t, device))
						require.NoError(t, err)

						want := unscoped && allowedBy(scope, action)
						require.Equalf(t, want, got, "%s with %s: %s on %s", principal, scope, action, device)
					}

					count, err := store.CountDevices(scoped, subject, action)
					require.NoError(t, err)

					if !allowedBy(scope, action) {
						require.Zerof(t, count, "%s with %s lists devices for %s", principal, scope, action)
					}
				}
			}
		}
	})
}

// The resource kinds follow the same rule as the devices.
func TestAScopeCutsEveryResourceDecisionDownToWhatItAllows(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, scope := range Scopes() {
			scoped := WithScopes(t.Context(), []Scope{scope})

			for _, principal := range fixturePrincipals {
				subject := subjectOf(t, principal)

				for _, action := range resourceActions() {
					for _, instance := range fixtureResources(t) {
						unscoped, err := store.CanResource(t.Context(), subject, action, instance)
						require.NoError(t, err)

						got, err := store.CanResource(scoped, subject, action, instance)
						require.NoError(t, err)

						want := unscoped && allowedBy(scope, action)
						require.Equalf(t, want, got, "%s with %s: %s on %s", principal, scope, action, instance)
					}
				}
			}
		}
	})
}

// An org admin whose credential stops short of administer writes no grant,
// reads no grant, changes no org role and reaches no dangerous verb, even on a
// machine they own. With administer the same calls go through.
func TestAScopeBelowAdministerAdministersNothing(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)
		root := subjectOf(t, "user:root")
		beta := deviceOf(t, "p-beta")

		require.NoError(t, store.Write(t.Context(), mustTuple(t, "device:p-beta#owner@user:root")))

		for _, scope := range []Scope{ScopeRead, ScopeOperate} {
			ctx := WithScopes(t.Context(), []Scope{scope})

			_, err := store.WriteGrant(ctx, root, mustTuple(t, "device:p-beta#viewer@user:mallory"))
			requireCode(t, err, accessDenied.Code())

			_, err = store.Grants(ctx, root, beta)
			requireCode(t, err, accessDenied.Code())

			err = store.SetOrgRole(ctx, root, subjectOf(t, "user:mallory"), RelViewer)
			requireCode(t, err, accessDenied.Code())

			reached, err := store.CanVerb(ctx, root, beta, verbForceGCode)
			require.NoError(t, err)
			require.Falsef(t, reached, "%s reaches the dangerous tier", scope)

			actions, err := store.ObjectActions(ctx, root, beta)
			require.NoError(t, err)
			require.NotContains(t, actions, ActionAdminister)
			require.NotContains(t, actions, ActionConfigure)
		}

		ctx := WithScopes(t.Context(), []Scope{ScopeAdminister})

		wrote, err := store.WriteGrant(ctx, root, mustTuple(t, "device:p-beta#viewer@user:mallory"))
		require.NoError(t, err)
		require.True(t, wrote)

		reached, err := store.CanVerb(ctx, root, beta, verbForceGCode)
		require.NoError(t, err)
		require.True(t, reached, "administer does not reach what the owner reaches")
	})
}

// A scope set on a context that already carries one narrows it and never
// widens it, so a transport that hands the request on cannot open it again.
func TestAScopeOnAScopedContextOnlyNarrows(t *testing.T) {
	t.Parallel()

	read := WithScopes(context.Background(), []Scope{ScopeRead})

	require.Equal(t, []Scope{ScopeRead}, ScopesFrom(WithScopes(read, []Scope{ScopeAdminister})))
	require.Equal(t, []Scope{ScopeRead}, ScopesFrom(WithScopes(read, []Scope{ScopeOperate})))
	require.Equal(t, []Scope{ScopeOperate},
		ScopesFrom(WithScopes(context.Background(), []Scope{ScopeOperate})))
	require.Nil(t, ScopesFrom(WithScopes(context.Background(), []Scope{ScopeAdminister})))
	require.Nil(t, ScopesFrom(WithScopes(context.Background(), nil)))
}

// A list of scopes is read strictly: an empty one and an unknown name are
// refused, and the answer is in order with no duplicate.
func TestScopesAreReadStrictly(t *testing.T) {
	t.Parallel()

	_, err := ParseScopes(nil)
	requireCode(t, err, invalidScope.Code())

	_, err = ParseScopes([]string{"read", "everything"})
	requireCode(t, err, invalidScope.Code())

	got, err := ParseScopes([]string{"administer", "read", "read"})
	require.NoError(t, err)
	require.Equal(t, []Scope{ScopeRead, ScopeAdminister}, got)

	require.True(t, WithinScopes([]Scope{ScopeRead}, []Scope{ScopeOperate}))
	require.True(t, WithinScopes([]Scope{ScopeOperate}, []Scope{ScopeAdminister}))
	require.False(t, WithinScopes([]Scope{ScopeAdminister}, []Scope{ScopeOperate}))
	require.False(t, WithinScopes([]Scope{ScopeOperate}, []Scope{ScopeRead}))
}

// A list names on each row the actions the scope allows, as ObjectActions does,
// and not the ones the principal holds beyond it.
func TestAScopeCutsTheActionsAListedDeviceCarries(t *testing.T) {
	t.Parallel()

	forEachEngine(t, func(t *testing.T, e engine) {
		store := seedFixture(t, e)

		for _, scope := range Scopes() {
			scoped := WithScopes(t.Context(), []Scope{scope})

			for _, principal := range fixturePrincipals {
				subject := subjectOf(t, principal)

				unscoped, err := store.ListDevices(t.Context(), subject, ActionObserve, Page{})
				require.NoError(t, err)

				got, err := store.ListDevices(scoped, subject, ActionObserve, Page{})
				require.NoError(t, err)
				require.Lenf(t, got, len(unscoped), "%s with %s", principal, scope)

				for i, row := range got {
					want := slices.DeleteFunc(slices.Clone(unscoped[i].Actions), func(action Action) bool {
						return !allowedBy(scope, action)
					})
					require.Equalf(t, want, row.Actions, "%s with %s on %s", principal, scope, row.ID)
				}
			}
		}
	})
}
