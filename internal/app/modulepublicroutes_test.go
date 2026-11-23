package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// routingManifest is the two-halves module with one public route and one
// staged route (ADR-20261120-xys).
func routingManifest() string {
	return strings.Replace(bothHalvesManifest(), "permissions:\n", strings.Join([]string{
		"needs:",
		"  data: true",
		"routes:",
		"  public:",
		"    - method: POST",
		"      path: /hooks/announce/",
		"  staged:",
		"    - method: PUT",
		"      path: /files/{name}",
		"      max_mib: 512",
		"permissions:",
		"",
	}, "\n"), 1)
}

// declaredRouter installs the routing module and reads back what the overlay
// holds.
func declaredRouter(t *testing.T, extra ...string) (resource.Declaration, string) {
	t.Helper()

	configPath, core := installConfig(t)
	release := aRelease(t, routingManifest(), true)

	said := addModuleFrom(t, configPath, theModulePath+"@1.0.0", release, extra...)

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: core.Overlay})
	require.NoError(t, err)
	require.Empty(t, doc.Failures)

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	declared, found := doc.Find(instance)
	require.True(t, found)

	return declared, said
}

// An approved install writes each public route into the service block and
// the ask, and the command names each one.
func TestAnApprovedInstallGrantsItsPublicRoutes(t *testing.T) {
	t.Parallel()

	declared, said := declaredRouter(t, "-approve")

	token := manifest.Route{Method: "POST", Path: "/hooks/announce/"}
	require.Equal(t, []manifest.Route{token}, declared.Spec.Service.PublicRoutes)
	require.Equal(t, []string{"POST /hooks/announce/"}, declared.Spec.Module.Asked.PublicRoutes)
	require.Equal(t, []install.StagedRoute{{Method: "PUT", Path: "/files/{name}", MaxMiB: 512}},
		declared.Spec.Service.StagedRoutes)
	require.Equal(t, []install.StagedRoute{{Method: "PUT", Path: "/files/{name}", MaxMiB: 512}},
		declared.Spec.Module.Asked.StagedRoutes)
	require.Contains(t, said, "public      POST /hooks/announce/, answered without a signed-in caller")
	require.Contains(t, said, "upload      PUT /files/{name}, up to 512 MiB (routes.staged)")
}

// An install without approve serves no route without a caller, and lists
// each one among the requests.
func TestAnInstallWithoutApproveOnlyListsItsPublicRoutes(t *testing.T) {
	t.Parallel()

	declared, said := declaredRouter(t)

	require.Empty(t, declared.Spec.Service.PublicRoutes)
	require.Empty(t, declared.Spec.Service.StagedRoutes)
	require.Contains(t, said, "asks for    public POST /hooks/announce/")
	require.Contains(t, said, "asks for    upload PUT /files/{name} up to 512 MiB")
}

// A release asking for a public route the operator was not shown is refused,
// and the request survives the API's spelling.
func TestAPublicRouteIsHeldToWhatTheOperatorWasShown(t *testing.T) {
	t.Parallel()

	asked := install.ModuleAsks{PublicRoutes: []string{"POST /hooks/announce/"}}

	require.Equal(t, []string{"public POST /hooks/announce/"}, beyondAsks(asked, install.ModuleAsks{}, theNamespace))
	require.Empty(t, beyondAsks(asked, asked, theNamespace))
	require.Equal(t, asked, resource.AsksOf(resource.RequestsOf(asked)))
}

// An approved update keeps a public route the operator took away out, and
// lists one the release before did not ask for as new.
func TestAnUpdateKeepsAPublicRouteTheOperatorTookAwayOut(t *testing.T) {
	t.Parallel()

	const (
		kept    = "POST /hooks/announce/"
		removed = "GET /devices/{serial}/"
		fresh   = "POST /hooks/leave/"
	)

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	declared := resource.Declaration{Ref: instance, Spec: resource.Spec{
		Service: &install.Service{PublicRoutes: []manifest.Route{{Method: "POST", Path: "/hooks/announce/"}}},
		Module:  &install.ModuleEntry{Asked: &install.ModuleAsks{PublicRoutes: []string{kept, removed}}},
	}}

	given, added, err := decidedAsks(moduleRequest{approve: true},
		install.ModuleAsks{PublicRoutes: []string{kept, removed, fresh}}, declared, theNamespace, nil)
	require.NoError(t, err)
	require.Equal(t, []string{kept, fresh}, given.PublicRoutes)
	require.Equal(t, []string{publicRequest(fresh)}, added)
}

// A staged route the operator was not shown, or one asking a larger body than
// they were shown, is refused, and the request survives the API's spelling.
func TestAStagedRouteIsHeldToWhatTheOperatorWasShown(t *testing.T) {
	t.Parallel()

	route := install.StagedRoute{Method: "PUT", Path: "/files/{name}", MaxMiB: 1024}
	asked := install.ModuleAsks{StagedRoutes: []install.StagedRoute{route}}
	smaller := install.ModuleAsks{
		StagedRoutes: []install.StagedRoute{{Method: "PUT", Path: "/files/{name}", MaxMiB: 512}},
	}

	require.Equal(
		t,
		[]string{"upload PUT /files/{name} up to 1024 MiB"},
		beyondAsks(asked, install.ModuleAsks{}, theNamespace),
	)
	require.Equal(t, []string{"upload PUT /files/{name} up to 1024 MiB"}, beyondAsks(asked, smaller, theNamespace))
	require.Empty(t, beyondAsks(smaller, asked, theNamespace))
	require.Equal(t, asked, resource.AsksOf(resource.RequestsOf(asked)))
}

// An approved update keeps a staged route the operator took away out, keeps a
// limit the operator changed, and lists a route new to the release, or one
// asking more than before, as new. A route whose limit the operator lowered
// and that now asks more than before and more than that limit is new too,
// and approving it gives the new ask.
func TestAnUpdateKeepsTheOperatorsStagedRoutes(t *testing.T) {
	t.Parallel()

	staged := func(path string, mib int) install.StagedRoute {
		return install.StagedRoute{Method: "PUT", Path: path, MaxMiB: mib}
	}

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)

	declared := resource.Declaration{Ref: instance, Spec: resource.Spec{
		Service: &install.Service{StagedRoutes: []install.StagedRoute{
			staged("/changed", 2048), staged("/kept", 100), staged("/grown", 100),
			staged("/lowered", 50), staged("/outgrown", 50),
		}},
		Module: &install.ModuleEntry{Asked: &install.ModuleAsks{StagedRoutes: []install.StagedRoute{
			staged("/changed", 1024), staged("/kept", 100), staged("/grown", 100), staged("/removed", 100),
			staged("/lowered", 100), staged("/outgrown", 100),
		}}},
	}}

	given, added, err := decidedAsks(
		moduleRequest{approve: true},
		install.ModuleAsks{StagedRoutes: []install.StagedRoute{
			staged("/changed", 1024), staged("/kept", 100), staged("/grown", 200), staged("/removed", 100),
			staged("/fresh", 300), staged("/lowered", 100), staged("/outgrown", 400),
		}},
		declared,
		theNamespace,
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, []install.StagedRoute{
		staged("/changed", 2048), staged("/kept", 100), staged("/grown", 200), staged("/fresh", 300),
		staged("/lowered", 50), staged("/outgrown", 400),
	}, given.StagedRoutes)
	require.Equal(t, []string{
		stagedRequest(staged("/fresh", 300)), stagedRequest(staged("/grown", 200)),
		stagedRequest(staged("/outgrown", 400)),
	}, added)
}
