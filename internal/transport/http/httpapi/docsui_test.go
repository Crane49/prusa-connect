//go:build !noreference

package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
)

// rawBundleBytes is the uncompressed size of @scalar/api-reference 1.66.1
// dist/browser/standalone.js, which is the file the gzip beside this package
// was made from.
const rawBundleBytes = 3803403

func TestTheReferencePageStaysOnThisOrigin(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	newTestServer(t).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, docsPath, nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))

	csp := rec.Header().Get("Content-Security-Policy")
	require.Contains(t, csp, "default-src 'none'")
	require.Contains(t, csp, "connect-src 'self'")
	require.NotContains(t, csp, "unpkg.com", "the renderer is served from this binary, not from a CDN")

	// The bundle builds a module worker from a blob URL, and worker-src falls
	// back to script-src, which does not carry blob:. Without this the feature
	// is blocked on a page nobody had loaded.
	require.Contains(t, csp, "worker-src 'self' blob:")

	// Every URL the page names must be same-origin. Huma's own docs route fails
	// this: it hardcodes an unpkg script tag that its CSP is written around.
	for _, url := range regexp.MustCompile(`(?:src|href|data-url)="([^"]+)"`).FindAllStringSubmatch(rec.Body.String(), -1) {
		require.Truef(t, strings.HasPrefix(url[1], "/"), "%s is not same-origin", url[1])
	}
}

// TestTheReferencePageDoesNotGrantUnsafeEval pins the relaxation that was taken
// on an unevidenced claim. TestTheEmbeddedBundleIsWhatTheCSPWasWrittenFor is
// the half that reads the bytes.
func TestTheReferencePageDoesNotGrantUnsafeEval(t *testing.T) {
	t.Parallel()

	require.NotContains(t, referenceCSP, "unsafe-eval",
		"the page renders operator-supplied OpenAPI content, and the bundle detects the refusal and falls back")
}

// TestTheEmbeddedBundleIsWhatTheCSPWasWrittenFor reads the shipped bytes rather
// than trusting a claim about them. It is what makes dropping 'unsafe-eval'
// evidence instead of an opinion, and it fails if a future bundle starts
// needing either relaxation back.
func TestTheEmbeddedBundleIsWhatTheCSPWasWrittenFor(t *testing.T) {
	t.Parallel()

	body, err := scalarBundle()
	require.NoError(t, err)

	source := string(body)

	require.NotContains(t, source, "eval(", "a call to eval would need 'unsafe-eval' back")
	require.NotContains(t, source, "new Function(", "a Function constructor would need 'unsafe-eval' back")

	// The two Function( hits are the validator probing for the relaxation
	// inside a try/catch and falling back when it is refused, which is the
	// evidence that refusing it is safe.
	require.Contains(t, source, "try{return Function(``),!0}catch{return!1}")

	// The worker branch the CSP's blob: is for.
	require.Contains(t, source, "URL.createObjectURL")
}

// TestTheReferenceTellsScalarNotToCallHome pins the two shipped defaults that
// reach the public internet: fonts from fonts.scalar.com and a "try it" proxy
// at proxy.scalar.com.
func TestTheReferenceTellsScalarNotToCallHome(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	newTestServer(t).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, docsPath, nil))

	html := rec.Body.String()

	require.Contains(t, html, `"withDefaultFonts":false`)
	require.Contains(t, html, `"proxyUrl":""`)
	require.Contains(t, html, `"telemetry":false`)
	require.Contains(t, html, `data-url="`+specJSONPath+`"`,
		"the document is fetched at page load, so an operation registered at boot appears without a rebuild")
	require.NotContains(t, html, "data-content",
		"inlining the document would freeze the reference at build time")
}

func TestTheRendererIsServedFromTheEmbeddedCopy(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t).Handler()

	t.Run("gzip is served as stored", func(t *testing.T) {
		t.Parallel()

		rec := getScript(handler, http.Header{"Accept-Encoding": {"gzip, deflate, br"}})

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
		require.Equal(t, "Accept-Encoding", rec.Header().Get("Vary"))
		require.Equal(t, scalarETag(), rec.Header().Get("ETag"))
		require.Equal(t, len(scalarBundleGzip), rec.Body.Len())
	})

	t.Run("a client without gzip gets the script anyway", func(t *testing.T) {
		t.Parallel()

		rec := getScript(handler, nil)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Empty(t, rec.Header().Get("Content-Encoding"))
		require.Equal(t, rawBundleBytes, rec.Body.Len())
		require.Contains(t, rec.Body.String()[:128], "document.createElement")
	})

	// The two representations are different byte strings, so RFC 9110 §8.8.1
	// gives them different entity-tags. One tag over both lets a cache hand the
	// gzip bytes to the client that asked for identity.
	t.Run("each representation has its own etag", func(t *testing.T) {
		t.Parallel()

		require.NotEqual(t, scalarETag(), scalarIdentityETag())
	})
}

// TestAConditionalRequestIsAnsweredTheWayTheRFCDefinesIt covers the forms a
// byte-exact comparison misses. Answering 304 only to an exact echo of the ETag
// makes every cache sending the weak form, a list or "*" refetch 3.6 MiB it
// already holds.
func TestAConditionalRequestIsAnsweredTheWayTheRFCDefinesIt(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t).Handler()
	tag := scalarETag()
	weak := "W/" + tag

	for name, test := range map[string]struct {
		ifNoneMatch string
		status      int
	}{
		"the exact tag":                {ifNoneMatch: tag, status: http.StatusNotModified},
		"the weak form of the tag":     {ifNoneMatch: weak, status: http.StatusNotModified},
		"star":                         {ifNoneMatch: anyTag, status: http.StatusNotModified},
		"a list holding the tag":       {ifNoneMatch: `"other", ` + tag, status: http.StatusNotModified},
		"a list holding no known tag":  {ifNoneMatch: `"other", "older"`, status: http.StatusOK},
		"the other representation tag": {ifNoneMatch: scalarIdentityETag(), status: http.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := getScript(handler, http.Header{
				"Accept-Encoding": {"gzip"},
				"If-None-Match":   {test.ifNoneMatch},
			})

			require.Equal(t, test.status, rec.Code)

			// Vary belongs on the 304 as well: a cache that stored the response
			// without it would serve the gzip bytes to a client that asked for
			// identity.
			require.Equal(t, "Accept-Encoding", rec.Header().Get("Vary"))

			if test.status == http.StatusNotModified {
				require.Zero(t, rec.Body.Len())
			}
		})
	}
}

// TestTheDecompressedBundleIsDecompressedOnce guards a measured cost: a
// per-request gzip.NewReader and 3.6 MiB io.ReadAll moves 145 MB in 5.7 s over
// 40 unauthenticated requests without gzip.
func TestTheDecompressedBundleIsDecompressedOnce(t *testing.T) {
	t.Parallel()

	first, err := scalarBundle()
	require.NoError(t, err)

	second, err := scalarBundle()
	require.NoError(t, err)

	require.Same(t, &first[0], &second[0], "every request allocating its own copy is the defect")
}

// TestTheEmbeddedBundleIsTheReviewedOne makes replacing the renderer a reviewed
// act. The bytes are third-party code served to every operator's browser, and a
// silent swap is the shape of a supply-chain change nobody sees.
func TestTheEmbeddedBundleIsTheReviewedOne(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256(scalarBundleGzip)

	require.Equalf(t, scalarSHA256, hex.EncodeToString(sum[:]),
		"the embedded @scalar/api-reference bundle is not the one this package documents (%s).\n"+
			"If the change is deliberate, update scalarVersion and scalarSHA256 in the same commit.",
		scalarVersion)
}

func getScript(handler http.Handler, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, docsScriptPath, nil)

	for key, values := range header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

// referenceServer is a server whose listing holds the planner module, titled the
// way its manifest titles it, with plane and graph as described says. The
// listing names the module to every caller, as a fake store does; whether the
// page names it is the graph's answer and the plane's, which is the point.
func referenceServer(t *testing.T, who string, described bool) (*Server, *fakeModuleAccess) {
	t.Helper()

	instance, err := ref.Parse(moduleInstance)
	require.NoError(t, err)

	var document []byte

	if described {
		fragment, parseErr := routesapi.ParseFragment([]byte(farmFragment))
		require.NoError(t, parseErr)

		document, err = fragment.Rebase(ModuleMount(moduleNamespace))
		require.NoError(t, err)
	}

	store := seededStore()
	store.instances[instance.Path()] = ResourceInstance{
		ResourceSpec: ResourceSpec{Type: "acme/planner", Enabled: true},
		Kind:         instance.Kind().String(),
		Name:         instance.ID(),
		Namespace:    moduleNamespace,
		Module:       &ResourceModule{Title: "Planner <beta>"},
	}

	access := &fakeModuleAccess{
		who:      "user:operator",
		instance: instance,
		actions:  []authz.Action{authz.ActionViewResource},
	}

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal(who),
		Resources:  store,
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{info: AuthInfo{Provider: "local", Credentials: true}},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
		Modules: fakeModulePlane{
			namespace: moduleNamespace, instance: instance, handler: moduleEcho(), openapi: document,
		},
		ModuleAccess: access,
	})
	require.NoError(t, err)

	return server, access
}

// readReference asks for the page as a browser would, framed or not.
func readReference(server *Server, target string, framed bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if framed {
		req.Header.Set("Sec-Fetch-Dest", "iframe")
	}

	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	return rec
}

// TestTheReferencePageRendersAModuleDocument holds ADR-20261027-zyf rules 1 to
// 3: the page names the module's served document, a page of this origin may
// frame it, and a name that is not a namespace is not written into the page at
// all.
func TestTheReferencePageRendersAModuleDocument(t *testing.T) {
	t.Parallel()

	server, _ := referenceServer(t, "user:operator", true)

	rec := readReference(server, docsPath+"?module="+moduleNamespace, false)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `data-url="/api/v1/modules/`+moduleNamespace+`/openapi.json"`)
	require.NotContains(t, rec.Body.String(), `data-url="/openapi.json"`)
	require.Contains(t, rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'")

	// The core's own page is still framed by nobody.
	core := readReference(server, docsPath, false)
	require.Contains(t, core.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")

	for _, bad := range []string{"..%2F..%2Fopenapi.json", "%22%3E%3Cscript%3E", "Com.Example"} {
		refused := readReference(server, docsPath+"?module="+bad, false)
		require.Equalf(t, http.StatusNotFound, refused.Code, "module=%s", bad)
	}
}

// TestTheReferenceSwitcherListsTheDocumentsTheReaderMayRead holds
// ADR-20261027-zyf rules 7 to 9: the switcher names the core and each module
// document the document route would serve this reader, and nothing else.
func TestTheReferenceSwitcherListsTheDocumentsTheReaderMayRead(t *testing.T) {
	t.Parallel()

	moduleLink := `href="/docs?module=` + moduleNamespace + `"`

	t.Run("a reader who may read the module", func(t *testing.T) {
		t.Parallel()

		server, access := referenceServer(t, "user:operator", true)

		core := readReference(server, docsPath, false)
		require.Equal(t, http.StatusOK, core.Code)
		require.Equal(t, "no-store", core.Header().Get("Cache-Control"))

		body := core.Body.String()
		require.Contains(t, body, `<nav class="pcl-documents"`)
		require.Contains(t, body, `href="/docs" aria-current="page"`)
		require.Contains(t, body, moduleLink)
		// The manifest's title is text, never markup.
		require.Contains(t, body, "Planner &lt;beta&gt;")
		require.NotContains(t, body, "Planner <beta>")

		require.Contains(t, access.asked, authz.ActionViewResource,
			"the page named the module without the check the document route makes")

		module := readReference(server, docsPath+"?module="+moduleNamespace, false)
		require.Contains(t, module.Body.String(), moduleLink+` aria-current="page"`)
		require.NotContains(t, module.Body.String(), `href="/docs" aria-current="page"`)
	})

	t.Run("a reader the graph refuses", func(t *testing.T) {
		t.Parallel()

		server, _ := referenceServer(t, "user:stranger", true)

		core := readReference(server, docsPath, false)
		require.Equal(t, http.StatusOK, core.Code)
		require.NotContains(t, core.Body.String(), moduleNamespace)
		require.NotContains(t, core.Body.String(), `<nav class="pcl-documents"`, "a switcher with one entry is drawn")

		// A deep link to the module says in place that it cannot be shown,
		// and the renderer is not started.
		module := readReference(server, docsPath+"?module="+moduleNamespace, false)
		require.Equal(t, http.StatusNotFound, module.Code)
		require.Contains(t, module.Header().Get("Content-Type"), "text/html")
		require.Contains(t, module.Body.String(), "This API document cannot be shown")
		require.NotContains(t, module.Body.String(), "/docs/scalar.js")
		require.Contains(t, module.Body.String(), `href="/docs"`, "the reader has no way back to the core's document")
	})

	t.Run("a reader with no identity", func(t *testing.T) {
		t.Parallel()

		server, _ := referenceServer(t, "", true)

		core := readReference(server, docsPath, false)
		require.Equal(t, http.StatusOK, core.Code)
		require.Contains(t, core.Body.String(), `data-url="/openapi.json"`)
		require.NotContains(t, core.Body.String(), moduleNamespace)
	})

	t.Run("a module with no document", func(t *testing.T) {
		t.Parallel()

		server, _ := referenceServer(t, "user:operator", false)

		core := readReference(server, docsPath, false)
		require.NotContains(t, core.Body.String(), moduleNamespace)

		module := readReference(server, docsPath+"?module="+moduleNamespace, false)
		require.Equal(t, http.StatusNotFound, module.Code)
		require.Contains(t, module.Body.String(), "This API document cannot be shown")
	})

	t.Run("a page in a frame", func(t *testing.T) {
		t.Parallel()

		server, _ := referenceServer(t, "user:operator", true)

		framed := readReference(server, docsPath+"?module="+moduleNamespace, true)
		require.Equal(t, http.StatusOK, framed.Code)
		require.NotContains(t, framed.Body.String(), `<nav class="pcl-documents"`,
			"a link to the core's page in a frame opens a page that refuses the frame")
	})

	t.Run("a page in a frame on an origin that is not secure", func(t *testing.T) {
		t.Parallel()

		server, _ := referenceServer(t, "user:operator", true)

		framed := readReference(server, docsPath+"?module="+moduleNamespace+"&embedded=1", false)
		require.Equal(t, http.StatusOK, framed.Code)
		require.NotContains(t, framed.Body.String(), `<nav class="pcl-documents"`,
			"a browser sends no Sec-Fetch-Dest over plain HTTP, so the frame says it is one")
	})
}
