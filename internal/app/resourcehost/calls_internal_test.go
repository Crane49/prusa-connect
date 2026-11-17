package resourcehost

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/extension/supervisor"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// routeGraph answers RequireResource from a set of principals it lets through,
// and records the scopes and the action of each question.
type routeGraph struct {
	allowed map[string]bool
	scopes  [][]authz.Scope
	actions []authz.Action
}

func (g *routeGraph) RequireResource(ctx context.Context, who authz.Subject, action authz.Action, _ ref.Ref) error {
	g.scopes = append(g.scopes, authz.ScopesFrom(ctx))
	g.actions = append(g.actions, action)

	if g.allowed[who.String()] {
		return nil
	}

	return errors.New("denied")
}

func callingModule(t *testing.T, calls ...string) (*Host, moduleCalls, *routeGraph) {
	t.Helper()

	caller, err := ref.New(ref.KindSink, "planner")
	require.NoError(t, err)

	graph := &routeGraph{allowed: map[string]bool{"user:ada": true}}
	host := &Host{
		cfg:     Config{RouteAccess: graph, Logger: slog.New(slog.DiscardHandler)},
		running: map[ref.Ref]*instance{},
	}

	return host, moduleCalls{host: host, caller: Module{Instance: caller, Calls: calls, Dependencies: calls}}, graph
}

func TestAModuleCallsOnlyTheModulesItDeclared(t *testing.T) {
	t.Parallel()

	host, calls, _ := callingModule(t, "com.github.acme.slicer")

	_, err := calls.Call(
		t.Context(),
		routesapi.CallParams{Module: "com.github.acme.other", Method: http.MethodGet, Path: "/"},
	)
	require.ErrorIs(t, err, routesapi.ErrCallNotDeclared)

	// Declared, and not running here.
	_, err = calls.Call(
		t.Context(),
		routesapi.CallParams{Module: "com.github.acme.slicer", Method: http.MethodGet, Path: "/"},
	)
	require.ErrorIs(t, err, routesapi.ErrCallUnavailable)

	require.Nil(t, host.callsFor(Module{Instance: calls.caller.Instance}, false),
		"a module the operator let call nothing is served routes.call")
}

func TestAModuleCallWithNoPersonRunsAsTheModule(t *testing.T) {
	t.Parallel()

	_, calls, graph := callingModule(t, "com.github.acme.slicer")

	principal, ctx, err := calls.principalOf(t.Context(),
		routesapi.CallParams{Method: http.MethodPost}, ref.Ref{})
	require.NoError(t, err)
	require.Equal(t, "sink:planner", principal)
	require.Nil(t, authz.ScopesFrom(ctx))
	require.Empty(t, graph.actions, "a call as the module asked the graph about a person")
}

// A call in a person's name runs only while the caller serves that person,
// under the scopes of their request, and only when the person may reach the
// other module's routes.
func TestAModuleCallInAPersonsNameIsHeldToThatPerson(t *testing.T) {
	t.Parallel()

	host, calls, graph := callingModule(t, "com.github.acme.slicer")
	post := routesapi.CallParams{Method: http.MethodPost, Principal: "user:ada"}

	_, _, err := calls.principalOf(t.Context(), post, ref.Ref{})
	require.ErrorIs(t, err, routesapi.ErrCallDenied, "a person the module is not serving was let through")

	leave := host.flights.board(calls.caller.Instance, "user:ada", []authz.Scope{authz.ScopeRead})
	defer leave()

	principal, _, err := calls.principalOf(t.Context(), post, ref.Ref{})
	require.NoError(t, err)
	require.Equal(t, "user:ada", principal)
	require.Equal(t, []authz.Scope{authz.ScopeRead}, graph.scopes[0])
	require.Equal(t, authz.ActionOperateResource, graph.actions[0])

	leaveBob := host.flights.board(calls.caller.Instance, "user:bob", nil)
	defer leaveBob()

	_, _, err = calls.principalOf(t.Context(),
		routesapi.CallParams{Method: http.MethodGet, Principal: "user:bob"}, ref.Ref{})
	require.ErrorIs(t, err, routesapi.ErrCallDenied, "a person the graph refuses was let through")
	require.Equal(t, authz.ActionViewResource, graph.actions[1])
}

// relayedTarget is the module called. It records each request the host
// relays to it, and whether the principal was on the board while it served.
type relayedTarget struct {
	host     *Host
	instance ref.Ref

	mu      sync.Mutex
	seen    []routesapi.Request
	boarded []bool
}

func (r *relayedTarget) Call(ctx context.Context, _ string, params any) (json.RawMessage, error) {
	request, ok := params.(routesapi.Request)
	if !ok {
		return nil, errors.New("not a routes.serve request")
	}

	_, boarded := r.host.flights.within(ctx, r.instance, request.Principal)

	r.mu.Lock()
	r.seen = append(r.seen, request)
	r.boarded = append(r.boarded, boarded)
	r.mu.Unlock()

	return json.Marshal(routesapi.Response{Status: http.StatusOK})
}

func (r *relayedTarget) requests() ([]routesapi.Request, []bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.seen), slices.Clone(r.boarded)
}

// runningTarget puts a module with routes under the namespace
// com.github.acme.slicer on the host, served by a target that records what
// reaches it.
func runningTarget(t *testing.T, host *Host, name string) *relayedTarget {
	t.Helper()

	declared := declaredAs(t, ref.KindSink, name, "com.github.acme.slicer/slicer", install.RuntimeService)
	target := &relayedTarget{host: host, instance: declared.Ref}

	runner, err := supervisor.New(supervisor.Config[any]{
		Ref:    declared.Ref,
		Policy: supervisor.DefaultPolicy(),
		Start:  func(context.Context, supervisor.Handle) (any, error) { return target, nil },
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = runner.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	require.Eventually(t, runner.Serving, 5*time.Second, 5*time.Millisecond)

	host.mu.Lock()
	host.running[declared.Ref] = &instance{runner: runner, declaration: declared}
	host.mu.Unlock()

	return target
}

// The host checks the path again where it relays the call, so a path that
// would leave the other module's mount never reaches that module, and a
// clean one reaches it as itself (ADR-20261027-pq5 rule 5).
func TestARelayedCallNeverLeavesTheMountOfTheModuleCalled(t *testing.T) {
	t.Parallel()

	host, calls, _ := callingModule(t, "com.github.acme.slicer")
	target := runningTarget(t, host, "slicer")

	for _, path := range []string{
		"/a/%2e%2e/b", "/%2e%2e/secret", "/a/%2e/b", "//evil.com/x",
		"/a/%2fb", "/a/%5cb", "/a/%252e%252e/b", "/a/../b", "/a\\b",
	} {
		_, err := calls.Call(t.Context(),
			routesapi.CallParams{Module: "com.github.acme.slicer", Method: http.MethodGet, Path: path})
		require.ErrorIs(t, err, routesapi.ErrCallInvalid, "the path %q was relayed", path)
	}

	seen, _ := target.requests()
	require.Empty(t, seen, "a refused path reached the module called")

	answered, err := calls.Call(t.Context(),
		routesapi.CallParams{Module: "com.github.acme.slicer", Method: http.MethodGet, Path: "/jobs/7", Query: "x=1"})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, answered.Status)

	seen, _ = target.requests()
	require.Len(t, seen, 1)
	require.Equal(t, "/jobs/7", seen[0].Path)

	inner, err := seen[0].HTTP(t.Context())
	require.NoError(t, err)
	require.Empty(t, inner.Host)
	require.Equal(t, "/jobs/7", inner.URL.Path)
	require.Equal(t, "x=1", inner.URL.RawQuery)
}

// The call is on the board of the module called while it is open, and off it
// after, so that module may act for the principal only for as long as the
// call lasts (ADR-20261027-pq5 rule 11).
func TestTheCallIsBoardedOnTheModuleCalled(t *testing.T) {
	t.Parallel()

	host, calls, _ := callingModule(t, "com.github.acme.slicer")
	target := runningTarget(t, host, "slicer")

	_, err := calls.Call(t.Context(),
		routesapi.CallParams{Module: "com.github.acme.slicer", Method: http.MethodPost, Path: "/slice"})
	require.NoError(t, err)

	seen, boarded := target.requests()
	require.Len(t, seen, 1)
	require.Equal(t, "sink:planner", seen[0].Principal)
	require.True(t, boarded[0], "the principal was not boarded on the module called")

	_, still := host.flights.within(t.Context(), target.instance, "sink:planner")
	require.False(t, still, "the principal stayed on the board after the call")
}

// A credential the calling module holds never reaches the module called
// (issue #605). The relay passes the headers a person's request would.
func TestARelayedCallCarriesNoCredential(t *testing.T) {
	t.Parallel()

	host, calls, _ := callingModule(t, "com.github.acme.slicer")
	target := runningTarget(t, host, "slicer")

	_, err := calls.Call(t.Context(), routesapi.CallParams{
		Module: "com.github.acme.slicer", Method: http.MethodGet, Path: "/jobs",
		Headers: map[string][]string{
			"Authorization": {"Bearer captured"}, "Cookie": {"session=captured"}, "Accept": {"application/json"},
		},
	})
	require.NoError(t, err)

	seen, _ := target.requests()
	require.Len(t, seen, 1)
	require.Equal(t, map[string][]string{"Accept": {"application/json"}}, seen[0].Headers)
}

// A person's request reaches a module without their session cookie or token
// (issue #605). The module learns who asks from the principal alone.
func TestAPersonsCredentialsStayWithTheHost(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/jobs?limit=5", nil)
	r.Header.Set("Cookie", "session=secret")
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	r.Header.Set("If-None-Match", `"4"`)

	request, err := moduleRequestOf(r)
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"If-None-Match": {`"4"`}}, request.Headers)
}

// A module that names its own namespace reads the call as unavailable, the
// way a module that is not running reads (ADR-20261027-pq5 rule 7).
func TestAModuleCannotCallItself(t *testing.T) {
	t.Parallel()

	host, calls, _ := callingModule(t, "com.github.acme.slicer")
	target := runningTarget(t, host, "planner")

	_, err := calls.Call(t.Context(),
		routesapi.CallParams{Module: "com.github.acme.slicer", Method: http.MethodGet, Path: "/"})
	require.ErrorIs(t, err, routesapi.ErrCallUnavailable)

	seen, _ := target.requests()
	require.Empty(t, seen, "a module was relayed a call from itself")
}

// A module that requires routes.call starts when the operator approved no
// call, or took the last one away on an update. It is served the method,
// and each call reads ErrCallNotDeclared (ADR-20261027-pq5 rule 6).
func TestAModuleThatRequiresCallsStartsWithNoneApproved(t *testing.T) {
	t.Parallel()

	declared := declaredAs(t, ref.KindSink, "planner", "com.github.acme.planner/planner", install.RuntimeService)
	declared.Spec.Service = &install.Service{}
	declared.Spec.Module = &install.ModuleEntry{
		Requires: []install.Requirement{{Capability: string(routesapi.MethodCall)}},
	}

	host := supervisingDeclarations(declared)

	serves, err := host.servesTo(declared)
	require.NoError(t, err)
	require.NoError(t, host.dependenciesMet(t.Context(), declared, serviceOf(declared), serves))

	client := routesapi.NewClient(wiredMethods(serves))
	_, err = client.Call(t.Context(),
		routesapi.CallParams{Module: "com.github.acme.slicer", Method: http.MethodGet, Path: "/"})
	require.ErrorIs(t, err, routesapi.ErrCallNotDeclared)

	// Without the requirement, and with no call approved, the method is not
	// served at all.
	declared.Spec.Module = &install.ModuleEntry{}

	serves, err = host.servesTo(declared)
	require.NoError(t, err)
	require.NotContains(t, serves, routesapi.MethodCall)
}

// wiredMethods hands a client's call to the host's handler, as the session
// would.
type wiredMethods wirerpc.Methods

func (w wiredMethods) Call(ctx context.Context, method wirerpc.Method, params any) (json.RawMessage, error) {
	handler, ok := w[method]
	if !ok {
		return nil, &wirerpc.Error{Code: wirerpc.CodeMethodNotFound, Message: "no such method"}
	}

	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	answer, err := handler(ctx, wirerpc.Call{Method: method, Params: raw})
	if err != nil {
		return nil, err
	}

	return json.Marshal(answer)
}
