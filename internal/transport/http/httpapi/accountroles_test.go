package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// roleGrants is a graph whose caller administers this server's identity, with
// the role write itself answerable: refused with whatever the case names, and
// otherwise recorded as the tuple the real store would write.
//
// It is separate from grantingStore because that one answers a refusal to every
// question, and these cases need a caller who passes the permission check and
// then meets the graph's own rule.
type roleGrants struct {
	fakeGrantStore

	refuseRole error
}

func (g *roleGrants) Can(context.Context, authz.Subject, authz.Action, authz.Object) (bool, error) {
	return true, nil
}

func (g *roleGrants) SetOrgRole(
	ctx context.Context, who authz.Subject, account authz.Subject, role authz.Relation,
) error {
	if g.refuseRole != nil {
		return g.refuseRole
	}

	return g.fakeGrantStore.SetOrgRole(ctx, who, account, role)
}

// colleague is the account these cases put on a role. The administrator's own
// account is "operator", which is the id the fixed principal resolves to.
const colleague = "u0123456789abcdefghij"

// accountsWithAColleague is a credential store holding the administrator and one
// colleague, which is the smallest server a role can be changed on.
func accountsWithAColleague() *fakeAccounts {
	return &fakeAccounts{
		info: AuthInfo{Provider: "local", Credentials: true},
		rows: []Account{
			{ID: "operator", Username: "owner@example.com", DisplayName: "Owner"},
			{ID: colleague, Username: "colleague@example.com"},
		},
	}
}

// TestSettingARoleWritesItForTheAccountTheParhNames is the operation working.
// One request puts a person on a role, and the tuple the graph is handed names
// that person and that role.
func TestSettingARoleWritesItForTheAccountThePathNames(t *testing.T) {
	t.Parallel()

	grants := &roleGrants{}
	handler := serverWithAccounts(t, accountsWithAColleague(), grants).Handler()

	rec := call(t, handler, http.MethodPut, accountsPath+"/"+colleague+"/role", `{"role":"operator"}`)
	require.Equal(t, http.StatusNoContent, rec.Code, "body: %s", rec.Body.String())

	require.Len(t, grants.roles, 1, "no role was written")
	require.Equal(t, "org:local#operator@user:"+colleague, grants.roles[0].String())
}

// TestAnAdministratorConfersTheAdminRoleOverHTTP is the old backlog at the
// transport. The grant surface refuses a write of the org's admin role; this
// operation is the one path that makes it, and it makes it in the caller's name.
func TestAnAdministratorConfersTheAdminRoleOverHTTP(t *testing.T) {
	t.Parallel()

	grants := &roleGrants{}
	handler := serverWithAccounts(t, accountsWithAColleague(), grants).Handler()

	rec := call(t, handler, http.MethodPut, accountsPath+"/"+colleague+"/role", `{"role":"admin"}`)
	require.Equal(t, http.StatusNoContent, rec.Code, "body: %s", rec.Body.String())

	require.Len(t, grants.roles, 1, "the admin role was not written")
	require.Equal(t, "org:local#admin@user:"+colleague, grants.roles[0].String())
	require.Equal(t, "user:operator", grants.callers[0].String(),
		"the role was written in somebody else's name")
}

// TestEveryRefusalTheRoleOperationGives is one case per answer a client branches
// on. Each one carries the code as well as the status, because a client that
// reads the status alone cannot tell a role this server does not have from a
// body it could not parse.
func TestEveryRefusalTheRoleOperationGives(t *testing.T) {
	t.Parallel()

	lastAdmin := fault.New(authz.CodeLastAdministrator(),
		"this is the only account holding the admin role")

	cases := map[string]struct {
		account string
		body    string
		refuse  error
		status  int
		code    fault.Code
	}{
		"an account nobody holds": {
			account: "u9999999999999999999x",
			body:    `{"role":"viewer"}`,
			status:  http.StatusNotFound,
			code:    httpfault.CodeResourceNotFound(),
		},
		"a role this server does not have": {
			account: colleague,
			body:    `{"role":"wizard"}`,
			status:  http.StatusBadRequest,
			code:    authz.CodeUnknownRelation(),
		},
		"the last administrator standing down": {
			account: "operator",
			body:    `{"role":"viewer"}`,
			refuse:  lastAdmin,
			status:  http.StatusConflict,
			code:    authz.CodeLastAdministrator(),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			grants := &roleGrants{refuseRole: tc.refuse}
			handler := serverWithAccounts(t, accountsWithAColleague(), grants).Handler()

			rec := call(t, handler, http.MethodPut, accountsPath+"/"+tc.account+"/role", tc.body)

			body := requireProblem(t, rec, tc.status)
			require.Equal(t, tc.code, body.Code)
		})
	}
}

// TestARefusedCallerLearnsNothingAboutWhoIsOnThisServer is the order the checks
// run in. A caller with no grant is refused before the account store is read, so
// the refusal for an id that exists and the one for an id that does not are the
// same answer.
func TestARefusedCallerLearnsNothingAboutWhoIsOnThisServer(t *testing.T) {
	t.Parallel()

	accounts := accountsWithAColleague()
	handler := serverWithAccounts(t, accounts, &grantingStore{allow: false}).Handler()

	for _, account := range []string{colleague, "u9999999999999999999x"} {
		rec := call(t, handler, http.MethodPut, accountsPath+"/"+account+"/role", `{"role":"admin"}`)

		body := requireProblem(t, rec, http.StatusForbidden)
		require.Equal(t, authz.CodeAccessDenied(), body.Code)
	}
}
