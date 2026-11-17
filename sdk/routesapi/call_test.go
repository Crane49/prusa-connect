package routesapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// loopback hands a client's call straight to the host's handler, the way a
// session would.
type loopback struct{ methods wirerpc.Methods }

func (l loopback) Call(ctx context.Context, method wirerpc.Method, params any) (json.RawMessage, error) {
	handler, ok := l.methods[method]
	if !ok {
		return nil, &wirerpc.Error{Code: wirerpc.CodeMethodNotFound, Message: "no such method"}
	}

	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	answer, err := handler(ctx, wirerpc.Call{Params: raw})
	if err != nil {
		return nil, err
	}

	return json.Marshal(answer)
}

// echoModules answers every call with what it was asked, or with a refusal.
type echoModules struct{ refuse error }

func (e echoModules) Call(_ context.Context, params routesapi.CallParams) (routesapi.Response, error) {
	if e.refuse != nil {
		return routesapi.Response{}, e.refuse
	}

	return routesapi.Response{Status: http.StatusAccepted, Body: []byte(params.Method + " " + params.Path)}, nil
}

func TestACallReachesTheHostAndAnswersTheOtherModule(t *testing.T) {
	t.Parallel()

	client := routesapi.NewClient(loopback{methods: routesapi.CallHandlers(echoModules{})})

	answered, err := client.Call(t.Context(), routesapi.CallParams{
		Module: "com.github.acme.slicer", Method: http.MethodPost, Path: "/jobs", Body: []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if answered.Status != http.StatusAccepted || string(answered.Body) != "POST /jobs" {
		t.Errorf("answered %d %q", answered.Status, answered.Body)
	}
}

func TestARefusalOfTheHostReadsAsItsSentinel(t *testing.T) {
	t.Parallel()

	for _, sentinel := range []error{
		routesapi.ErrCallNotDeclared, routesapi.ErrCallUnavailable, routesapi.ErrCallDenied, routesapi.ErrCallInvalid,
	} {
		client := routesapi.NewClient(loopback{methods: routesapi.CallHandlers(echoModules{
			refuse: fmt.Errorf("the host says: %w", sentinel),
		})})

		_, err := client.Call(
			t.Context(),
			routesapi.CallParams{Module: "com.github.acme.slicer", Method: http.MethodGet, Path: "/"},
		)
		if !errors.Is(err, sentinel) {
			t.Errorf("a refusal of %v read as %v", sentinel, err)
		}
	}
}

func TestACallOutsideTheBoundsIsRefusedBeforeItLeaves(t *testing.T) {
	t.Parallel()

	for _, bad := range []routesapi.CallParams{
		{Method: http.MethodGet, Path: "/"},
		{Module: "com.github.acme.slicer", Method: "TRACE", Path: "/"},
		{Module: "com.github.acme.slicer", Method: http.MethodGet, Path: "jobs"},
		{Module: "com.github.acme.slicer", Method: http.MethodGet, Path: "/a/../b"},
		{Module: "com.github.acme.slicer", Method: http.MethodPost, Path: "/", Body: make([]byte, routesapi.MaxModuleBody+1)},
		{Module: "com.github.acme.slicer", Method: http.MethodGet, Path: "/jobs", Query: "a=1#frag"},
	} {
		if err := bad.Validate(); !errors.Is(err, routesapi.ErrCallInvalid) {
			t.Errorf("%s %q was accepted", bad.Method, bad.Path)
		}
	}
}

// A path is checked as the module called reads it: net/http unescapes it
// before the module's own router sees it, so an escaped dot segment, a second
// leading slash or an escaped separator is refused, never cleaned into a
// path the check accepts.
func TestAPathThatLeavesTheMountIsRefused(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/a/%2e%2e/b",       // unescapes to /a/../b
		"/%2e%2e/secret",    // unescapes to /../secret
		"/a/%2e/b",          // unescapes to /a/./b
		"/a/%2E%2E/b",       // the same in capitals
		"/a/.%2e/b",         // half escaped
		"//evil.com/x",      // a URL reads evil.com as the host
		"///x",              // the same with an empty host
		"/a/%2fb",           // an escaped separator
		"/a%2F..%2Fb",       // an escaped separator around a dot segment
		"/a/%5c..%5cb",      // an escaped backslash
		"/a\\..\\b",         // a backslash, which some routers read as /
		"/a/%252e%252e/b",   // escaped twice: one more unescape is /a/../b
		"/a/%25",            // a percent sign after one unescape
		"/a/%zz",            // not a valid escape
		"/a/b?x=1",          // the query goes in Query
		"/a/b#top",          // a fragment never reaches a module
		"/a//b",             // an empty segment path.Clean would drop
		"/a/./b",            // a dot segment
		"/a/..",             // a dot segment at the end
		"/a/%00b",           // a control character
		"/a/\x7fb",          // DEL
		"http://evil.com/x", // a scheme
	} {
		params := routesapi.CallParams{Module: "com.github.acme.slicer", Method: http.MethodGet, Path: path}
		if err := params.Validate(); !errors.Is(err, routesapi.ErrCallInvalid) {
			t.Errorf("the path %q was accepted", path)
		}
	}
}

// A clean path is accepted, and reaches the module called as itself.
func TestACleanPathStaysAsItIs(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]string{
		"/":              "/",
		"/jobs":          "/jobs",
		"/jobs/":         "/jobs/",
		"/jobs/7/gcode":  "/jobs/7/gcode",
		"/files/a%20b":   "/files/a b",
		"/a/..b/c.d":     "/a/..b/c.d",
		"/v1/items:list": "/v1/items:list",
	} {
		params := routesapi.CallParams{Module: "com.github.acme.slicer", Method: http.MethodGet, Path: path}
		if err := params.Validate(); err != nil {
			t.Errorf("the path %q was refused: %v", path, err)

			continue
		}

		inner, err := params.Request("sink:planner").HTTP(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		if inner.URL.Path != want || inner.URL.Host != "" || inner.Host != "" {
			t.Errorf("the path %q reached the module as host %q path %q", path, inner.URL.Host, inner.URL.Path)
		}
	}
}

func TestAHostThatRelaysNoCallServesNoMethod(t *testing.T) {
	t.Parallel()

	if methods := routesapi.CallHandlers(nil); len(methods) != 0 {
		t.Errorf("a nil host serves %d methods", len(methods))
	}
}
