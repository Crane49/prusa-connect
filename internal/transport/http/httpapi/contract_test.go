package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

const modulePath = "github.com/prusa3d/prusa-connect"

// sealedPackage is the one package allowed to name the router type. Everything
// else registers through huma.API, which cannot mount a route without also
// publishing it.
const sealedPackage = modulePath + "/internal/transport/http/httpapi"

// routerPackages are the imports that hand a caller the ability to register a
// route the OpenAPI document will never hear about.
//
// It listed two, which left the cheapest bypass of all open: a composition root
// wrapping Server.Handler() in some other mux and adding a route beside it, on
// the same origin, past every gate. Every router that can do that belongs here,
// and stdlib's is caught by muxSymbols below because net/http cannot be an
// import ban.
var routerPackages = []string{
	"github.com/go-chi/chi",
	"github.com/go-chi/chi/v5",
	"github.com/gorilla/mux",
	"github.com/gin-gonic/gin",
	"github.com/labstack/echo",
	"github.com/labstack/echo/v4",
	"github.com/julienschmidt/httprouter",
	"github.com/gofiber/fiber/v2",
	"github.com/danielgtaylor/huma/v2/adapters/humachi",
	"github.com/danielgtaylor/huma/v2/adapters/humago",
	"github.com/danielgtaylor/huma/v2/adapters/humagin",
	"github.com/danielgtaylor/huma/v2/adapters/humaecho",
	"github.com/danielgtaylor/huma/v2/adapters/humafiber",
	"github.com/danielgtaylor/huma/v2/adapters/humamux",
	"github.com/danielgtaylor/huma/v2/adapters/humaflow",
	"github.com/danielgtaylor/huma/v2/adapters/humabunrouter",
	"github.com/danielgtaylor/huma/v2/adapters/humahttprouter",
}

// muxSymbols are net/http's own route registrars. They cannot be banned as an
// import, because every package that speaks HTTP imports net/http, so the
// symbols are what is refused.
var muxSymbols = []string{
	"NewServeMux",
	"ServeMux",
	"Handle",
	"HandleFunc",
	"DefaultServeMux",
}

// muxExemptions are the files that build a net/http mux for a reason this gate
// is not about, each with the property that keeps it out of scope. The key is a
// repository-relative path and the gate fails on an entry it never reads, so an
// exemption that stopped describing a file does not quietly widen.
//
// A connectorapi.Driver's Handler returns the routes a device dials. The sdk
// contract requires the driver to build them and hands them to the host as an
// http.Handler, so a mux is what the contract asks for. It is not the bypass
// above because an adapter cannot reach httpapi.Server.Handler to wrap it:
// ADR-20260825-n55 forbids an adapter importing internal/ and
// TestAdaptersCompileAgainstTheContractAlone enforces that.
//
// The host does mount a driver handler, and it mounts it through Config.Mounts
// rather than beside this server, so one router still answers the whole origin.
// The gate on that is TestAMountThisServerWillNotServeIsRefusedAtConstruction
// and TestAMountAnswersItsOwnPathsAndNeverThisServers, next to the code that
// makes the decision.
// A module's own routes are the second entry, and the reason is where they are
// served rather than what builds them. A module runs in a process of its own and
// answers routesapi.MethodServe, so its mux is reached only through this
// server's module mount, which resolves the caller and decides the permission
// before the call is made. Nothing it registers reaches this origin's router, and
// nothing it serves may reach the OpenAPI document: publishing a module's paths
// would make uninstalling that module a breaking change to /api/v1
// (ADR-20260827-wmv rule 14). The gate on that mount is
// TestAModuleRouteIsMountedUnderItsNamespaceAndAuthorized and
// TestAModuleReachesNothingOutsideItsOwnGroup.
//
// The third entry is a fake Moonraker host, and its reason is which side of the
// wire it is on. A Moonraker machine listens and this server dials it, so the
// fake has to listen too or there is nothing to dial. Its mux answers the
// Moonraker websocket path and never this origin's, and no test reaches it
// through this server.
//
// It is not even in this module any more. The Moonraker dialect moved out to
// examples/adapters/moonraker (ADR-20260901-7t4) and took its fake machine with it, so
// the file below carries its own go.mod and `go list -deps` over
// cmd/prusa-connect cannot name it under any tag set. The gate still has
// to know about it because the walk this file does is over the filesystem
// rather than over the build graph: a nested module's directory is still a
// directory under examples/, and a fake that quietly stopped being read here
// would be an exemption nobody notices going stale.
var muxExemptions = map[string]string{
	"adapters/connector/echo/echo.go":  "connectorapi.Driver.Handler, the routes a device dials",
	"examples/service-plugin/main.go":  "routesapi.Serve, a module's own routes in a process of its own",
	"examples/database-module/main.go": "routesapi.Serve, a module's own routes in a process of its own",
	"examples/spools/main.go":          "routesapi.Serve, the spools example module's own routes in a process of its own",
	"examples/service-plugin/fleet.go": "routesapi.Serve, the same module's routes onto the host's fleet " +
		"and event stream",
	"examples/adapters/moonraker/moonrakersim/moonrakersim.go": "a fake Moonraker host, which is a " +
		"machine this server dials rather than a route it serves",
}

// bypassProbePaths are paths a route added beside this server would plausibly
// take. The gate is a smoke test and says so: chi.Walk cannot see a handler
// that is not a route, so a middleware that answers a path of its own, or a
// NotFound handler that serves content, is invisible to the diff. This probes
// for the shape rather than proving its absence.
//
// /metrics is deliberately absent: it is a published operation
// (registerMetrics), so probing for it would assert that the server does not
// serve what it serves.
var bypassProbePaths = []string{
	"/",
	"/admin",
	"/debug/pprof/",
	"/debug/vars",
	"/internal/status",
	"/.well-known/openid-configuration",
	"/api/v1",
	"/api/v1/",
	"/api/v1/resources/storage/local/secret",
}

// TestNewProducesARouterThatMatchesItsDocument is the whole point of the
// package, asserted on the real server rather than on a fixture.
func TestNewProducesARouterThatMatchesItsDocument(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	require.NoError(t, verifyDocumented(server.mux, server.api, nil))
}

// TestVerifyDocumentedRejectsARouteTheDocumentDoesNotPublish watches the gate
// refuse. A gate never seen rejecting is a gate nobody has evidence for.
func TestVerifyDocumentedRejectsARouteTheDocumentDoesNotPublish(t *testing.T) {
	t.Parallel()

	cases := map[string]func(mux *chi.Mux, api huma.API){
		"a route added straight to the router": func(mux *chi.Mux, _ huma.API) {
			mux.Get("/api/v1/secret", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		},
		"an operation registered as hidden": func(_ *chi.Mux, api huma.API) {
			huma.Register(api, huma.Operation{
				OperationID: "get-hidden",
				Method:      http.MethodGet,
				Path:        "/api/v1/hidden",
				Summary:     "hidden",
				Hidden:      true,
			}, func(_ context.Context, _ *struct{}) (*struct{}, error) { return &struct{}{}, nil })
		},
		"the adapter used directly, which registers without documenting": func(_ *chi.Mux, api huma.API) {
			api.Adapter().Handle(
				&huma.Operation{Method: http.MethodGet, Path: "/api/v1/bypass"},
				func(ctx huma.Context) { ctx.SetStatus(http.StatusOK) },
			)
		},
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			mux := chi.NewMux()
			api := humachi.New(mux, humaConfig(Config{}.withDefaults()))

			require.NoError(t, verifyDocumented(mux, api, nil))

			mutate(mux, api)

			err := verifyDocumented(mux, api, nil)
			require.ErrorIs(t, err, ErrUndocumentedRoute)
			require.Contains(t, err.Error(), "served but not in the OpenAPI document")
		})
	}
}

// TestVerifyDocumentedRejectsAnOperationNothingServes is the other direction: a
// document that promises what the router will answer 404 for.
func TestVerifyDocumentedRejectsAnOperationNothingServes(t *testing.T) {
	t.Parallel()

	mux := chi.NewMux()
	api := humachi.New(mux, humaConfig(Config{}.withDefaults()))

	api.OpenAPI().AddOperation(&huma.Operation{
		OperationID: "get-promised",
		Method:      http.MethodGet,
		Path:        "/api/v1/promised",
	})

	err := verifyDocumented(mux, api, nil)
	require.ErrorIs(t, err, ErrUndocumentedRoute)
	require.Contains(t, err.Error(), "in the OpenAPI document but not served")
}

// TestNewRefusesToBuildAServerThatFailsTheGate proves the gate is a startup
// failure and not only a test failure, which is the only form available for a
// route an extension registers after compilation.
func TestNewRefusesToBuildAServerThatFailsTheGate(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	server.mux.Get("/api/v1/added-later", func(_ http.ResponseWriter, _ *http.Request) {})
	require.ErrorIs(t, verifyDocumented(server.mux, server.api, nil), ErrUndocumentedRoute)
}

// TestNewRequiresAWayToIdentifyACaller keeps the identity seam from having a
// silent default.
func TestNewRequiresItsSeamsToBeFilled(t *testing.T) {
	t.Parallel()

	_, err := New(Config{})
	require.ErrorIs(t, err, ErrNoPrincipals)

	_, err = New(Config{Principals: fixedPrincipal("operator")})
	require.ErrorIs(t, err, ErrNoResourceStore)

	_, err = New(Config{Principals: fixedPrincipal("operator"), Resources: seededStore()})
	require.ErrorIs(t, err, ErrNoGCodePresetStore)

	_, err = New(Config{
		Principals: fixedPrincipal("operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
	})
	require.ErrorIs(t, err, ErrNoGrants)
}

// TestOnlyTheSealedPackageImportsTheRouter is the layer the diff test cannot
// reach: a route registered in another package, on a router this one never
// sees. Naming the router anywhere else is what makes that possible, so the
// import is what is refused.
func TestOnlyTheSealedPackageImportsTheRouter(t *testing.T) {
	t.Parallel()

	var violations []string

	for _, pkg := range listPackages(t) {
		if pkg.ImportPath == sealedPackage || strings.HasPrefix(pkg.ImportPath, modulePath+"/legacy/") {
			continue
		}

		for _, imp := range append(pkg.Imports, pkg.TestImports...) {
			for _, router := range routerPackages {
				if imp == router {
					violations = append(violations, pkg.ImportPath+" imports "+imp)
				}
			}
		}
	}

	require.Emptyf(t, violations, "only %s may name the router.\n"+
		"A package holding a chi.Router can register a route the OpenAPI document will never\n"+
		"publish, which is the hole the sealed constructor exists to close. Register through\n"+
		"huma.API instead: it cannot mount a route without documenting it.", sealedPackage)
}

// TestNothingOutsideTheSealedPackageMountsAServeMux closes the bypass the
// import ban could not see. net/http's mux needs no third-party import, so a
// composition root could write
//
//	mux := http.NewServeMux()
//	mux.Handle("/", server.Handler())
//	mux.HandleFunc("/internal/dump", dump)
//
// and serve an undocumented, unauthenticated route on the same origin, past
// every layer this package has. The symbols are refused instead of the import.
func TestNothingOutsideTheSealedPackageMountsAServeMux(t *testing.T) {
	t.Parallel()

	claimed := map[string]bool{}

	violations := scanTree(t, func(fset *token.FileSet, path string, file *ast.File) []string {
		where := filepath.ToSlash(path)

		if strings.Contains(where, "/internal/transport/http/httpapi/") {
			return nil
		}

		for exempt := range muxExemptions {
			if strings.HasSuffix(where, exempt) {
				claimed[exempt] = true

				return nil
			}
		}

		return muxSymbolLines(fset, file)
	})

	require.Empty(t, violations, "net/http's ServeMux registers a route that chi.Walk cannot enumerate and\n"+
		"the OpenAPI document will never publish. Serve httpapi.Server.Handler() directly, or add the\n"+
		"route through huma.Register inside the sealed package.")

	for exempt, why := range muxExemptions {
		require.Truef(t, claimed[exempt], "%s is exempted for %q and the gate never read it. "+
			"An exemption that stopped describing a file is one nobody will notice going stale.", exempt, why)
	}
}

// TestNoMiddlewareOrFallbackHandlerServesAPathOfItsOwn probes the shape of the
// bypass chi.Walk is blind to. A middleware that answers a path never becomes a
// route, and neither does a NotFound handler that serves content instead of
// refusing, so neither appears in the diff against the document.
//
// It is a smoke gate, not a proof, and the package documentation says so.
func TestNoMiddlewareOrFallbackHandlerServesAPathOfItsOwn(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t).Handler()

	for _, path := range bypassProbePaths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
				rec := call(t, handler, method, path, "")

				require.Containsf(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, rec.Code,
					"%s %s is answered by something the OpenAPI document does not publish", method, path)
				require.Equalf(t, problemMediaType, rec.Header().Get("Content-Type"),
					"%s %s answers with content rather than a refusal", method, path)
			}
		})
	}
}

// muxSymbolLines reports every reference to a net/http route registrar.
//
// The local name is read out of the file's own import list rather than assumed
// to be "http". It was assumed, and `import nethttp "net/http"` then mounted the
// exact bypass this gate's doc comment describes with the gate green. A
// dot-import is the same hole spelled shorter, so it is read too: the symbols
// arrive as bare identifiers and there is no selector to match.
func muxSymbolLines(fset *token.FileSet, file *ast.File) []string {
	local, dotted := netHTTPLocalName(file)
	if local == "" && !dotted {
		return nil
	}

	var found []string

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			ident, isIdent := node.X.(*ast.Ident)
			if local == "" || !isIdent || ident.Name != local || !slices.Contains(muxSymbols, node.Sel.Name) {
				return true
			}

			found = append(found, fset.Position(node.Pos()).String()+": "+local+"."+node.Sel.Name)
		case *ast.Ident:
			if !dotted || !slices.Contains(muxSymbols, node.Name) {
				return true
			}

			found = append(found, fset.Position(node.Pos()).String()+": "+node.Name+", dot-imported from net/http")
		}

		return true
	})

	return found
}

// netHTTPLocalName returns the name net/http is spelled under in one file. local
// is empty when the file does not import it, or imports it blank, which cannot
// reference a symbol. dotted is a dot-import, where there is no qualifier at all.
func netHTTPLocalName(file *ast.File) (local string, dotted bool) {
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != "net/http" {
			continue
		}

		if spec.Name == nil {
			return "http", false
		}

		switch spec.Name.Name {
		case ".":
			return "", true
		case "_":
			return "", false
		default:
			return spec.Name.Name, false
		}
	}

	return "", false
}

// scanTree parses every Go file in every tree this module builds and collects
// what inspect reports.
//
// The roots are read off the package list rather than written down. They were
// written down, as internal and cmd, which left adapters, tools and examples
// unwalked: real Go trees holding real packages, outside a gate whose failure
// message says "nothing outside the sealed package". Each tree is then walked on
// disk, so a file behind a build tag is read too.
//
// legacy/ is out of scope by ADR-20260824-dym. sdk/ is a separate module that imports
// nothing outside the standard library (ADR-20260824-gfr), so nothing in it can hold the
// handler there would be any point in wrapping.
func scanTree(t *testing.T, inspect func(*token.FileSet, string, *ast.File) []string) []string {
	t.Helper()

	var (
		violations []string
		fset       = token.NewFileSet()
	)

	for _, group := range goTrees(t) {
		root := filepath.Join(repoRoot(t), group)

		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return walkErr
			}

			file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return parseErr
			}

			violations = append(violations, inspect(fset, path, file)...)

			return nil
		})
		require.NoError(t, err)
	}

	return violations
}

// goTrees returns the top-level directory of every package in this module, minus
// the quarantine.
func goTrees(t *testing.T) []string {
	t.Helper()

	var trees []string

	for _, pkg := range listPackages(t) {
		relative, ok := strings.CutPrefix(pkg.ImportPath, modulePath+"/")
		if !ok {
			continue
		}

		group, _, _ := strings.Cut(relative, "/")
		if group == "" || group == "legacy" {
			continue
		}

		trees = append(trees, group)
	}

	slices.Sort(trees)
	trees = slices.Compact(trees)

	require.NotEmpty(t, trees, "this gate read no trees")

	return trees
}

// TestNoOperationIsHidden names Huma's one in-framework bypass directly. The
// diff test above already catches a hidden operation, because it reaches the
// router and leaves the document; this one fails with the word Hidden in it, so
// the next person reads the reason rather than the symptom.
//
// It walks the same trees the ServeMux gate does, through the same walker. It
// had a copy of its own that read internal/ alone, and two walkers for one
// question is the shape where one of them is the stale one.
func TestNoOperationIsHidden(t *testing.T) {
	t.Parallel()

	violations := scanTree(t, func(fset *token.FileSet, _ string, file *ast.File) []string {
		return hiddenOperationLines(fset, file)
	})

	// The gate's own fixture sets Hidden on purpose, to watch the diff test
	// reject it. Everything else is a real bypass.
	violations = withoutSelf(violations)

	require.Empty(t, violations, "huma.Operation.Hidden registers a live route and omits it from the\n"+
		"OpenAPI document. It is the only field that can do that. Remove it, or publish the\n"+
		"operation and mark it deprecated if the intent is to retire it.")
}

// hiddenOperationLines reports every huma.Operation literal setting Hidden.
func hiddenOperationLines(fset *token.FileSet, file *ast.File) []string {
	var found []string

	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}

		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Operation" {
			return true
		}

		if ident, isIdent := sel.X.(*ast.Ident); !isIdent || ident.Name != "huma" {
			return true
		}

		for _, elt := range lit.Elts {
			kv, isKV := elt.(*ast.KeyValueExpr)
			if !isKV {
				continue
			}

			if key, isIdent := kv.Key.(*ast.Ident); isIdent && key.Name == "Hidden" {
				found = append(found, fset.Position(kv.Pos()).String())
			}
		}

		return true
	})

	return found
}

func withoutSelf(violations []string) []string {
	kept := make([]string, 0, len(violations))

	for _, violation := range violations {
		if !strings.Contains(filepath.ToSlash(violation), "internal/transport/http/httpapi/contract_test.go") {
			kept = append(kept, violation)
		}
	}

	return kept
}

type listedPackage struct {
	ImportPath  string   `json:"ImportPath"`
	Imports     []string `json:"Imports"`
	TestImports []string `json:"TestImports"`
}

// listPackages asks the toolchain for the graph rather than parsing imports
// here, so a build tag that hides an edge from us hides it from the compiler
// too. Test imports count: a route registered from a test is still a route.
func listPackages(t *testing.T) []listedPackage {
	t.Helper()

	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = repoRoot(t)

	out, err := cmd.Output()
	require.NoError(t, err, "go list")

	decoder := json.NewDecoder(bytes.NewReader(out))

	var pkgs []listedPackage

	for {
		var pkg listedPackage

		decodeErr := decoder.Decode(&pkg)
		if errors.Is(decodeErr, io.EOF) {
			break
		}

		require.NoError(t, decodeErr)

		pkgs = append(pkgs, pkg)
	}

	require.NotEmpty(t, pkgs, "go list returned no packages, which means this gate is reading nothing")

	return pkgs
}

// newTestServer builds the production server with a stub identity source.
func newTestServer(t *testing.T) *Server {
	t.Helper()

	return serverWith(t, seededStore())
}

// fixedPrincipal is a Principals that always resolves to the same subject. The
// real one authenticates; this one exists so the seam is exercised.
type fixedPrincipal string

func (p fixedPrincipal) Principal(*http.Request) (string, error) {
	if p == "" {
		return "", errors.New("no credentials")
	}

	return string(p), nil
}

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "no go.mod above the working directory")

		dir = parent
	}
}

// Caller is Principal with no scope.
func (p fixedPrincipal) Caller(r *http.Request) (Caller, error) {
	return Unscoped(p.Principal(r))
}
