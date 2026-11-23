package routesapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// serveOnce sends one request through Serve and returns what the handler saw.
func serveOnce(t *testing.T, request routesapi.Request) (file, principal string, answered routesapi.Response) {
	t.Helper()

	handler := routesapi.Serve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file = routesapi.BodyFileOf(r)
		principal = routesapi.PrincipalOf(r)

		w.WriteHeader(http.StatusNoContent)
	}))

	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}

	out, err := handler(t.Context(), wirerpc.Call{Params: raw})
	if err != nil {
		t.Fatalf("serve: %v", err)
	}

	answered, _ = out.(routesapi.Response)

	return file, principal, answered
}

func TestAStagedBodyFileReachesTheHandler(t *testing.T) {
	t.Parallel()

	file, principal, answered := serveOnce(t, routesapi.Request{
		Method: http.MethodPut, Path: "/files/raw", BodyFile: "staged/AbC123", Principal: "user:alice",
	})

	if file != "staged/AbC123" || principal != "user:alice" || answered.Status != http.StatusNoContent {
		t.Errorf("handler saw file %q and principal %q, answered %d", file, principal, answered.Status)
	}

	file, _, _ = serveOnce(t, routesapi.Request{Method: http.MethodGet, Path: "/files"})
	if file != "" {
		t.Errorf("a request with no body file read %q", file)
	}

	if routesapi.BodyFileOf(nil) != "" {
		t.Error("a nil request read a body file")
	}
}

func TestABodyFileOutsideTheDataDirectoryIsRefused(t *testing.T) {
	t.Parallel()

	for _, file := range []string{"/etc/passwd", "../other/x", "staged/../../x"} {
		_, err := routesapi.Request{Method: http.MethodPut, Path: "/a", BodyFile: file}.HTTP(t.Context())
		if err == nil {
			t.Errorf("body file %q was accepted", file)
		}
	}
}

// TestThePathReachesTheHandlerAsTheHostSentIt holds Request.HTTP to the path
// and the query it was given. The host matched its routes against that exact
// path, so a ? or a # inside it must not start a query or a fragment, a % must
// not be decoded again, and a trailing slash or an empty segment stays.
func TestThePathReachesTheHandlerAsTheHostSentIt(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/hooks/x?/announce",
		"/hooks/x#/announce",
		"/hooks/x%3F/announce",
		"/hooks/x/announce/",
		"/hooks//announce",
		"/files/a b.gcode",
	} {
		inner, err := routesapi.Request{Method: http.MethodPost, Path: path, Query: "a=b"}.HTTP(t.Context())
		if err != nil {
			t.Fatalf("%q: %v", path, err)
		}

		if inner.URL.Path != path || inner.URL.RawQuery != "a=b" || inner.URL.Fragment != "" {
			t.Errorf("%q reached the handler as path %q, query %q, fragment %q",
				path, inner.URL.Path, inner.URL.RawQuery, inner.URL.Fragment)
		}
	}
}
