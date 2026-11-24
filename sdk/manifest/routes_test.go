package manifest_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// withRoutes is the smallest manifest carrying a routes block and a data
// directory, so a staged route has somewhere to go.
func withRoutes(routes string) string {
	return `{"name": "uploader", "kind": "module", "license": "MIT", "contract_major": 1, "needs": {"data": true}, "routes": ` +
		routes + `}`
}

func TestARoutesBlockReadsPublicAndStagedRoutes(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withRoutes(`{
		"public": [
			{"method": "POST", "path": "/hooks/announce/"},
			{"method": "GET", "path": "/devices/{serial}/"}
		],
		"staged": [{"method": "PUT", "path": "/app/teams/{team}/files/raw", "max_mib": 1024}]
	}`)), decode)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	wantPublic := []manifest.Route{
		{Method: "POST", Path: "/hooks/announce/"},
		{Method: "GET", Path: "/devices/{serial}/"},
	}
	if !slices.Equal(held.Routes.Public, wantPublic) {
		t.Errorf("public = %+v, want %+v", held.Routes.Public, wantPublic)
	}

	wantStaged := []manifest.StagedRoute{
		{Route: manifest.Route{Method: "PUT", Path: "/app/teams/{team}/files/raw"}, MaxMiB: 1024},
	}
	if !slices.Equal(held.Routes.Staged, wantStaged) {
		t.Errorf("staged = %+v, want %+v", held.Routes.Staged, wantStaged)
	}
}

// A staged route may say in words what it is for, which the operator reads
// beside its limit instead of the path alone.
func TestAStagedRouteReadsItsTitle(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withRoutes(`{
		"staged": [{"method": "PUT", "path": "/files/{name}", "max_mib": 1024, "title": "Upload from PrusaSlicer 2.6 and later"}]
	}`)), decode)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := held.Routes.Staged[0].Title; got != "Upload from PrusaSlicer 2.6 and later" {
		t.Errorf("title = %q", got)
	}
}

func TestAManifestWithoutRoutesServesNoRouteDifferently(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(`{"name": "x", "kind": "module", "license": "MIT"}`), decode)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(held.Routes.Public) != 0 || len(held.Routes.Staged) != 0 {
		t.Errorf("routes = %+v, want none", held.Routes)
	}
}

func TestARoutesBlockIsRefused(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		document string
		says     string
	}{
		{"an unknown key", withRoutes(`{"private": []}`), "private"},
		{"an unknown entry key", withRoutes(`{"public": [{"method": "GET", "path": "/a", "max_mib": 1}]}`), "max_mib"},
		{"a method in lower case", withRoutes(`{"public": [{"method": "get", "path": "/a"}]}`), "expected one of"},
		{"no path", withRoutes(`{"public": [{"method": "GET"}]}`), "path is empty"},
		{"a relative path", withRoutes(`{"public": [{"method": "GET", "path": "a"}]}`), "starting with /"},
		{"a dot segment", withRoutes(`{"public": [{"method": "GET", "path": "/a/../b"}]}`), `segment ".."`},
		{"an empty segment", withRoutes(`{"public": [{"method": "GET", "path": "/a//b"}]}`), `segment ""`},
		{"a query", withRoutes(`{"public": [{"method": "GET", "path": "/a?b=c"}]}`), "a?b=c"},
		{"a placeholder inside a segment", withRoutes(`{"public": [{"method": "GET", "path": "/a{b}"}]}`), "a{b}"},
		{"a placeholder in capitals", withRoutes(`{"public": [{"method": "GET", "path": "/{Id}"}]}`), "{Id}"},
		{"a route twice", withRoutes(`{"public": [{"method": "GET", "path": "/a"}, {"method": "GET", "path": "/a"}]}`),
			"twice"},
		{"a route both public and staged",
			withRoutes(`{"public": [{"method": "PUT", "path": "/a"}], "staged": [{"method": "PUT", "path": "/a", "max_mib": 1}]}`),
			"both reach"},
		{"a public placeholder over a staged route",
			withRoutes(`{"public": [{"method": "POST", "path": "/up/{id}"}], "staged": [{"method": "POST", "path": "/up/x", "max_mib": 1}]}`),
			"both reach"},
		{"a staged placeholder over a public route",
			withRoutes(`{"public": [{"method": "POST", "path": "/up/x"}], "staged": [{"method": "POST", "path": "/up/{id}", "max_mib": 1}]}`),
			"both reach"},
		{"a staged route twice",
			withRoutes(`{"staged": [{"method": "PUT", "path": "/a", "max_mib": 1}, {"method": "PUT", "path": "/a", "max_mib": 2}]}`),
			"one request can reach both"},
		{"two staged routes one request reaches",
			withRoutes(`{"staged": [{"method": "PUT", "path": "/files/{name}", "max_mib": 10}, {"method": "PUT", "path": "/files/big", "max_mib": 4096}]}`),
			"one request can reach both"},
		{"a staged route with no bound", withRoutes(`{"staged": [{"method": "PUT", "path": "/a"}]}`), "expected 1 to 4096"},
		{"a staged title with a control character",
			withRoutes(`{"staged": [{"method": "PUT", "path": "/a", "max_mib": 1, "title": "Up\u001b[2Kload"}]}`), "title"},
		{"a staged title too long",
			withRoutes(`{"staged": [{"method": "PUT", "path": "/a", "max_mib": 1, "title": "` + strings.Repeat("x", 81) + `"}]}`),
			"the limit is 80"},
		{"a staged bound too large", withRoutes(`{"staged": [{"method": "PUT", "path": "/a", "max_mib": 5000}]}`),
			"expected 1 to 4096"},
		{
			"a staged route without a data directory",
			`{"name": "x", "kind": "module", "license": "MIT", "contract_major": 1, "routes": {"staged": [{"method": "PUT", "path": "/a", "max_mib": 1}]}}`,
			"needs.data is not",
		},
		{
			"a route without a process",
			`{"name": "x", "kind": "module", "license": "MIT", "routes": {"public": [{"method": "GET", "path": "/a"}]}}`,
			"contract_major is not",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := manifest.Parse("module.yaml", []byte(tc.document), decode)
			if !errors.Is(err, manifest.ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}

			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("err = %q, want it to say %q", err, tc.says)
			}
		})
	}
}

func TestARouteMatchesOneMethodAndOnePath(t *testing.T) {
	t.Parallel()

	route := manifest.Route{Method: "POST", Path: "/teams/{team}/files/"}

	for _, tc := range []struct {
		method, path string
		want         bool
	}{
		{"POST", "/teams/7/files/", true},
		{"POST", "/teams/abc/files/", true},
		{"GET", "/teams/7/files/", false},
		{"POST", "/teams/7/files", false},
		{"POST", "/teams//files/", false},
		{"POST", "/teams/7/8/files/", false},
		{"POST", "/teams/7/files/x", false},
		{"POST", "/Teams/7/files/", false},
	} {
		if got := route.Matches(tc.method, tc.path); got != tc.want {
			t.Errorf("Matches(%s %s) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestARouteReadsBackFromItsLine(t *testing.T) {
	t.Parallel()

	route := manifest.Route{Method: "POST", Path: "/hooks/announce/"}

	got, err := manifest.ParseRoute(route.String())
	if err != nil || got != route {
		t.Fatalf("ParseRoute(%q) = %+v, %v, want %+v", route.String(), got, err, route)
	}

	for _, line := range []string{"", "POST", "post /a", "POST a", "POST /a/.."} {
		if _, err = manifest.ParseRoute(line); !errors.Is(err, manifest.ErrInvalid) {
			t.Errorf("ParseRoute(%q) = %v, want ErrInvalid", line, err)
		}
	}
}

// Two routes overlap when one request could be on both, so a placeholder on
// either side reaches the other's segment. A placeholder never fills an empty
// segment, and the method and the number of segments must agree.
func TestRoutesOverlapAsPatterns(t *testing.T) {
	t.Parallel()

	cases := []struct {
		a, b manifest.Route
		want bool
	}{
		{manifest.Route{Method: "POST", Path: "/up/x"}, manifest.Route{Method: "POST", Path: "/up/x"}, true},
		{manifest.Route{Method: "POST", Path: "/up/{id}"}, manifest.Route{Method: "POST", Path: "/up/x"}, true},
		{manifest.Route{Method: "POST", Path: "/up/x"}, manifest.Route{Method: "POST", Path: "/up/{id}"}, true},
		{manifest.Route{Method: "POST", Path: "/{a}/x"}, manifest.Route{Method: "POST", Path: "/up/{b}"}, true},
		{manifest.Route{Method: "PUT", Path: "/up/{id}"}, manifest.Route{Method: "POST", Path: "/up/x"}, false},
		{manifest.Route{Method: "POST", Path: "/up/{id}"}, manifest.Route{Method: "POST", Path: "/up/"}, false},
		{manifest.Route{Method: "POST", Path: "/up/{id}"}, manifest.Route{Method: "POST", Path: "/up/x/y"}, false},
		{manifest.Route{Method: "POST", Path: "/up/x"}, manifest.Route{Method: "POST", Path: "/down/x"}, false},
	}

	for _, tc := range cases {
		if got := tc.a.Overlaps(tc.b); got != tc.want {
			t.Errorf("%s overlaps %s = %v, want %v", tc.a, tc.b, got, tc.want)
		}

		if got := tc.b.Overlaps(tc.a); got != tc.want {
			t.Errorf("%s overlaps %s = %v, want %v", tc.b, tc.a, got, tc.want)
		}
	}
}
