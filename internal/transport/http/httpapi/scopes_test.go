package httpapi

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// scopedPrincipal resolves every request to one subject whose credential is
// limited to the scopes it names.
type scopedPrincipal struct {
	who    string
	scopes []authz.Scope
}

func (p scopedPrincipal) Principal(*http.Request) (string, error) { return p.who, nil }

func (p scopedPrincipal) Caller(*http.Request) (Caller, error) {
	return Caller{Principal: p.who, Scopes: p.scopes}, nil
}

// scopeRecordingAccess is a module graph that records the scopes each question
// reached it under, and answers yes.
type scopeRecordingAccess struct {
	mu  sync.Mutex
	saw [][]authz.Scope
}

func (a *scopeRecordingAccess) RequireResource(
	ctx context.Context, _ authz.Subject, _ authz.Action, _ ref.Ref,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.saw = append(a.saw, authz.ScopesFrom(ctx))

	return nil
}

// The scopes of the caller's credential reach the graph when a module route
// is authorized, and they reach the module's request, so the plane holds what
// the module asks for on this caller's behalf to them too.
func TestTheScopesOfACredentialReachTheModuleGateAndTheModule(t *testing.T) {
	t.Parallel()

	instance, err := ref.Parse(moduleInstance)
	require.NoError(t, err)

	var forwarded []authz.Scope

	module := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = authz.ScopesFrom(r.Context())

		w.WriteHeader(http.StatusOK)
	})

	access := &scopeRecordingAccess{}
	plane := fakeModulePlane{namespace: moduleNamespace, instance: instance, handler: module}
	server, err := New(Config{
		Version:      "test",
		Principals:   scopedPrincipal{who: "user:operator", scopes: []authz.Scope{authz.ScopeRead}},
		Resources:    seededStore(),
		Presets:      seededPresets(),
		Endpoints:    seededEndpoints(),
		Grants:       &fakeGrantStore{},
		Sessions:     newFakeSessions(),
		Accounts:     &fakeAccounts{info: AuthInfo{Provider: "local", Credentials: true}},
		Readiness:    &fakeReadiness{report: readyReport()},
		Devices:      noPrinters{},
		Stores:       noStorages{},
		Events:       noEvents{},
		Modules:      plane,
		ModuleAccess: access,
	})
	require.NoError(t, err)

	answered := ask(t, server, http.MethodGet, ModuleMount(moduleNamespace)+"/spools", "")
	require.Equal(t, http.StatusOK, answered.Code, answered.Body.String())

	require.Equal(t, [][]authz.Scope{{authz.ScopeRead}}, access.saw)
	require.Equal(t, []authz.Scope{authz.ScopeRead}, forwarded)
}

// A credential limited below administer may revoke itself, which is how an
// app drops a token it no longer needs, and no other credential of its
// person's.
func TestAScopedCredentialRevokesItselfAndNoOther(t *testing.T) {
	t.Parallel()

	credentials := newFakeCredentials()
	credentials.presented = ownCredentialID
	credentials.rows["an0th3rCr3d3nt1al00000"] = APICredential{
		ID: "an0th3rCr3d3nt1al00000", Name: "phone", Subject: "user:operator",
	}

	server, err := New(Config{
		Version:     "test",
		Principals:  scopedPrincipal{who: "user:operator", scopes: []authz.Scope{authz.ScopeOperate}},
		Resources:   seededStore(),
		Presets:     seededPresets(),
		Endpoints:   seededEndpoints(),
		Grants:      &grantingStore{allow: true},
		Sessions:    newFakeSessions(),
		Accounts:    &fakeAccounts{},
		Credentials: credentials,
		Readiness:   &fakeReadiness{report: readyReport()},
		Devices:     noPrinters{},
		Stores:      noStorages{},
		Events:      noEvents{},
	})
	require.NoError(t, err)

	other := call(t, server.Handler(), http.MethodDelete, credentialsPath+"/an0th3rCr3d3nt1al00000", "")
	problem := requireProblem(t, other, http.StatusForbidden)
	require.Equal(t, authz.CodeAccessDenied(), problem.Code)

	own := call(t, server.Handler(), http.MethodDelete, credentialsPath+"/"+ownCredentialID, "")
	require.Equal(t, http.StatusNoContent, own.Code, own.Body.String())
	require.Equal(t, []string{ownCredentialID}, credentials.revoked)
}

// A mint names its scopes, and none names read: a credential is never wider
// than what was written down for it. A name outside the set is refused.
func TestAMintNamesItsScopesAndNoneIsRead(t *testing.T) {
	t.Parallel()

	credentials := newFakeCredentials()
	server := serverWithCredentials(t, credentials, &grantingStore{allow: true})

	minted := call(t, server.Handler(), http.MethodPost, credentialsPath, `{"name":"display","scopes":["operate"]}`)
	require.Equal(t, http.StatusCreated, minted.Code, minted.Body.String())
	require.Contains(t, minted.Body.String(), `"scopes":["operate"]`)

	refused := call(t, server.Handler(), http.MethodPost, credentialsPath, `{"name":"display","scopes":["root"]}`)
	require.Equal(t, http.StatusUnprocessableEntity, refused.Code, refused.Body.String())

	require.Len(t, credentials.issued, 1)
	require.Equal(t, []string{"operate"}, credentials.issued[0].Scopes)
}
