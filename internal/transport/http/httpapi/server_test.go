package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
	"github.com/prusa3d/prusa-connect/sdk/fault/faulttest"
)

// problemBody is what a client reads off an error response. It is decoded with
// the sdk's own types, so a change to how a Fault marshals fails here.
type problemBody struct {
	Type      string       `json:"type"`
	Status    int          `json:"status"`
	Code      fault.Code   `json:"code"`
	Title     string       `json:"title"`
	Detail    string       `json:"detail"`
	Params    fault.Params `json:"params"`
	Retryable bool         `json:"retryable"`
	TraceID   string       `json:"trace_id"`
	Instance  string       `json:"instance"`
}

// testInstanceID is the id of this server the test servers are built with.
const testInstanceID = "8f14e45f-ceea-4e7a-9c2b-3d1f6a5b7c90"

// TestHealthAnswersWithoutAnIdentity reads the version and the id of this
// server with no credential, which is how a client that found the server on the
// network tells it from another one before it signs in (ADR-20261025-gve rule 5).
func TestHealthAnswersWithoutAnIdentity(t *testing.T) {
	t.Parallel()

	rec := call(t, newTestServer(t).Handler(), http.MethodGet, healthPath, "")

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Status     string `json:"status"`
		Version    string `json:"version"`
		InstanceID string `json:"instance_id"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "ok", body.Status)
	require.Equal(t, "test", body.Version)
	require.Equal(t, testInstanceID, body.InstanceID)
}

func TestPrincipalReturnsTheResolvedSubject(t *testing.T) {
	t.Parallel()

	rec := call(t, newTestServer(t).Handler(), http.MethodGet, principalPath, "")

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Principal string `json:"principal"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "operator", body.Principal)
}

func TestPrincipalNamesTheSignedInAccount(t *testing.T) {
	t.Parallel()

	accounts := &fakeAccounts{rows: []Account{{ID: "alice", Username: "alice", DisplayName: "Alice Novak"}}}

	read := func(who string) map[string]any {
		server := accountServer(t, accounts, &fakeGrantStore{}, fixedPrincipal(who))
		rec := call(t, server.Handler(), http.MethodGet, principalPath, "")
		require.Equal(t, http.StatusOK, rec.Code)

		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

		return body
	}

	require.Equal(t, map[string]any{
		"principal": "user:alice", "username": "alice", "display_name": "Alice Novak",
	}, read("user:alice"))
	require.Equal(t, map[string]any{"principal": "service_account:ci"}, read("service_account:ci"))
	require.Equal(t, map[string]any{"principal": "user:gone"}, read("user:gone"))
}

// fakeProfiles is what an identity provider said about one person.
type fakeProfiles map[string]Profile

func (f fakeProfiles) Profile(_ context.Context, id string) (Profile, bool, error) {
	found, ok := f[id]

	return found, ok, nil
}

// A person who signed in at the identity provider has no local account, and is
// named by what the provider last sent.
func TestPrincipalNamesAPersonTheProviderSignedIn(t *testing.T) {
	t.Parallel()

	read := func(who string) map[string]any {
		server, err := New(Config{
			Version:    "test",
			Principals: fixedPrincipal(who),
			Resources:  seededStore(),
			Presets:    seededPresets(),
			Endpoints:  seededEndpoints(),
			Grants:     &fakeGrantStore{},
			Sessions:   newFakeSessions(),
			Accounts:   &fakeAccounts{},
			Profiles: fakeProfiles{
				"oidc-wyj7c7bxbvgxunmhhus7ehtwaoxuz7r3": {DisplayName: "Ada Admin", Email: "ada@example.com"},
			},
			Readiness: &fakeReadiness{report: readyReport()},
			Devices:   noPrinters{},
			Stores:    noStorages{},
			Events:    noEvents{},
		})
		require.NoError(t, err)

		rec := call(t, server.Handler(), http.MethodGet, principalPath, "")
		require.Equal(t, http.StatusOK, rec.Code)

		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

		return body
	}

	require.Equal(t, map[string]any{
		"principal":    "user:oidc-wyj7c7bxbvgxunmhhus7ehtwaoxuz7r3",
		"display_name": "Ada Admin",
		"email":        "ada@example.com",
	}, read("user:oidc-wyj7c7bxbvgxunmhhus7ehtwaoxuz7r3"))
	require.Equal(t, map[string]any{"principal": "user:nobody"}, read("user:nobody"))
}

// TestPrincipalRefusesAnUnidentifiableCaller is the refusal half of the pair.
// The permit test above without it would be half a test.
func TestPrincipalRefusesAnUnidentifiableCaller(t *testing.T) {
	t.Parallel()

	rec := call(t, serverWithoutIdentity(t).Handler(), http.MethodGet, principalPath, "")

	body := requireProblem(t, rec, http.StatusUnauthorized)
	require.Equal(t, fault.AuthTokenInvalid.Code(), body.Code)
	require.Equal(t, principalPath, body.Instance)
	require.NotEmpty(t, body.TraceID, "a client must be able to quote something an operator can grep")
}

// TestHealthStaysPublicWhenIdentityIsRefused pins the rule the middleware reads
// out of the document: an operation declaring OpenAPI's empty security
// requirement takes no identity.
func TestHealthStaysPublicWhenIdentityIsRefused(t *testing.T) {
	t.Parallel()

	rec := call(t, serverWithoutIdentity(t).Handler(), http.MethodGet, healthPath, "")
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestTheRouterRefusesInProblemJSON(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t).Handler()

	t.Run("an unknown path", func(t *testing.T) {
		t.Parallel()

		rec := call(t, handler, http.MethodGet, "/api/v1/nothing-here", "")

		body := requireProblem(t, rec, http.StatusNotFound)
		require.Equal(t, httpfault.CodeRouteNotFound(), body.Code)
		require.Equal(t, "/api/v1/nothing-here", paramString(t, body.Params, "path"))
		require.Equal(t, http.MethodGet, paramString(t, body.Params, "method"))
	})

	t.Run("a method the path does not publish", func(t *testing.T) {
		t.Parallel()

		rec := call(t, handler, http.MethodPost, healthPath, "")

		body := requireProblem(t, rec, http.StatusMethodNotAllowed)
		require.Equal(t, httpfault.CodeMethodNotAllowed(), body.Code)
	})
}

// TestTheValidatorRefusesInProblemJSON drives Huma's own validation failure
// through the replaced error model. The operation is registered on a harness
// rather than on the production router: the error model is what is under test,
// not the route set.
func TestTheValidatorRefusesInProblemJSON(t *testing.T) {
	t.Parallel()

	handler := newHarness(t, defaultBodyLimit, registerProbe)

	rec := call(t, handler, http.MethodPost, "/probe", `{"name":"a"}`)

	body := requireProblem(t, rec, http.StatusUnprocessableEntity)
	require.Equal(t, httpfault.CodeRequestInvalid(), body.Code)
	require.Equal(t, "body.name", paramString(t, body.Params, "location"))
	require.Contains(t, paramString(t, body.Params, "reason"), "length")
}

// TestABodyOverTheLimitRefusesInProblemJSON exercises the bound rather than
// asserting it exists, at the default and above it.
//
// A 32-byte limit is the only regime where the router's reader wins the race
// with Huma's own, so exercising that one alone proves nothing about the other
// two. Huma bounds every operation at 1 MiB whatever the configuration says and
// reports the breach with an error carrying no limit, which is how a configured
// 2 MiB comes to be refused at 1 MiB and tell the client to send at most zero
// bytes.
func TestABodyOverTheLimitRefusesInProblemJSON(t *testing.T) {
	t.Parallel()

	for name, limit := range map[string]int64{
		"a small configured limit": 32,
		"the default limit":        defaultMaxBodySize,
		"a limit above Huma's own": 2 * defaultMaxBodySize,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			handler := newHarness(t, limit, registerProbe)

			rec := call(t, handler, http.MethodPost, "/probe", probeBodyOf(int(limit)+1))

			body := requireProblem(t, rec, http.StatusRequestEntityTooLarge)
			require.Equal(t, httpfault.CodeBodyTooLarge(), body.Code)
			require.Equal(t, strconv.FormatInt(limit, 10), paramInt(t, body.Params, "limit_bytes"),
				"a refusal naming a limit the server does not enforce tells the client nothing it can act on")
		})
	}
}

// TestABodyUnderTheConfiguredLimitIsAccepted is the other half, and the one the
// defect actually broke: a body inside the configured bound was refused because
// Huma's 1 MiB default fired first.
func TestABodyUnderTheConfiguredLimitIsAccepted(t *testing.T) {
	t.Parallel()

	const limit = 4 * defaultMaxBodySize

	handler := newHarness(t, limit, registerProbe)

	rec := call(t, handler, http.MethodPost, "/probe", probeBodyOf(2*defaultMaxBodySize))

	require.Equal(t, http.StatusNoContent, rec.Code, "body: %s", rec.Body.String())
}

// probeBodyOf builds a probe body of exactly size bytes.
func probeBodyOf(size int) string {
	const envelope = `{"name":""}`

	return `{"name":"` + strings.Repeat("x", size-len(envelope)) + `"}`
}

// TestEveryOperationDefersTheBodyLimit is the gate on the fix. Huma bounds a
// body per operation and defaults that bound to 1 MiB; the bound this transport
// enforces is the configured one, held once by bodyLimitMiddleware, so an
// operation that let Huma's default back in would silently reintroduce it.
func TestEveryOperationDefersTheBodyLimit(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	checked := 0

	for path, item := range server.api.OpenAPI().Paths {
		for method, op := range operationsOfPath(item) {
			if op == nil {
				continue
			}

			checked++

			require.Equalf(t, int64(unlimitedBody), op.MaxBodyBytes,
				"%s %s was registered with huma.Register rather than register, so Huma bounds it at its own "+
					"1 MiB default and reports the breach without a limit the client can read", method, path)
		}
	}

	require.NotZero(t, checked, "this gate read no operations")
}

// TestARawBodyKeepsTheServerReadTimeout pins the read deadline of the largest
// raw body this transport takes. Huma would bound a camera frame at the 5 s it
// gives a typed body, and a frame of 8 MiB from a mobile network can take longer.
func TestARawBodyKeepsTheServerReadTimeout(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	for _, item := range server.api.OpenAPI().Paths {
		for _, op := range operationsOfPath(item) {
			if op != nil && op.OperationID == "put-camera-snapshot" {
				require.Equal(t, readTimeout, op.BodyReadTimeout)

				return
			}
		}
	}

	t.Fatal("no operation called put-camera-snapshot is registered")
}

// TestOnlyAStateChangingOperationRefusesAnUnpublishedQueryParameter reads the
// registered operations against the rule, the way the body-limit gate above
// does, and catches the same thing: an operation mounted with huma.Register
// rather than register, which would carry Huma's default instead of this
// transport's decision.
//
// It does not pin the rule itself, because it asks changesState what the answer
// is. TestAPublicProbeToleratesACacheBuster and
// TestAMisspeltQueryParameterIsRefused are the two that fail when the rule
// changes, one on each side of it.
func TestOnlyAStateChangingOperationRefusesAnUnpublishedQueryParameter(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	checked := 0

	for path, item := range server.api.OpenAPI().Paths {
		for method, op := range operationsOfPath(item) {
			if op == nil {
				continue
			}

			checked++

			require.Equalf(t, changesState(method), op.RejectUnknownQueryParameters,
				"%s %s does not match the rule in changesState", method, path)
		}
	}

	require.NotZero(t, checked, "this gate read no operations")
}

// TestAPublicProbeToleratesACacheBuster is the same rule on the wire, because
// the field above is not what an operator's monitoring sees.
func TestAPublicProbeToleratesACacheBuster(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t).Handler()

	for _, query := range []string{"?_=1724580000", "?utm_source=uptime", "?_=1&cb=2"} {
		rec := call(t, handler, http.MethodGet, healthPath+query, "")

		require.Equalf(t, http.StatusOK, rec.Code, "GET %s%s: %s", healthPath, query, rec.Body.String())
	}
}

// TestHeadIsAnsweredFromTheGetHandler covers what an uptime probe sends and
// what RFC 9110 §9.3.2 requires of a general-purpose server. It runs over a
// real socket because net/http, not the handler, is what drops the body.
func TestHeadIsAnsweredFromTheGetHandler(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(newTestServer(t).Handler())
	t.Cleanup(srv.Close)

	head, err := srv.Client().Head(srv.URL + healthPath)
	require.NoError(t, err)

	t.Cleanup(func() { _ = head.Body.Close() })

	require.Equal(t, http.StatusOK, head.StatusCode)
	require.Equal(t, "application/json", head.Header.Get("Content-Type"))

	body, err := io.ReadAll(head.Body)
	require.NoError(t, err)
	require.Empty(t, body, "HEAD is GET without the body")

	t.Run("a path with no GET is still refused", func(t *testing.T) {
		t.Parallel()

		refused, headErr := srv.Client().Head(srv.URL + "/api/v1/nothing-here")
		require.NoError(t, headErr)

		t.Cleanup(func() { _ = refused.Body.Close() })

		require.Equal(t, http.StatusNotFound, refused.StatusCode)
	})
}

// TestTheTraceIdIsValidatedRatherThanTrusted covers the header an
// unauthenticated caller controls. Accepting the all-zero trace-id W3C forbids
// would let anyone collapse every log line they produce onto one id.
func TestTheTraceIdIsValidatedRatherThanTrusted(t *testing.T) {
	t.Parallel()

	const good = "4bf92f3577b34da6a3ce929d0e0e4736"

	handler := newTestServer(t).Handler()

	for name, test := range map[string]struct {
		traceparent string
		want        string
	}{
		"a well-formed traceparent is continued": {
			traceparent: "00-" + good + "-00f067aa0ba902b7-01", want: good,
		},
		"the all-zero trace id is refused": {
			traceparent: "00-00000000000000000000000000000000-00f067aa0ba902b7-01",
		},
		"an all-zero parent id is refused": {
			traceparent: "00-" + good + "-0000000000000000-01",
		},
		"version ff is refused": {
			traceparent: "ff-" + good + "-00f067aa0ba902b7-01",
		},
		"uppercase hex is refused": {
			traceparent: "00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01",
		},
		"a short trace id is refused": {
			traceparent: "00-4bf92f35-00f067aa0ba902b7-01",
		},
		"a missing field is refused": {
			traceparent: "00-" + good + "-01",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/nothing-here", nil)
			req.Header.Set("Traceparent", test.traceparent)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			body := requireProblem(t, rec, http.StatusNotFound)

			require.Len(t, body.TraceID, 32)

			if test.want != "" {
				require.Equal(t, test.want, body.TraceID)

				return
			}

			require.NotEqual(t, "00000000000000000000000000000000", body.TraceID)
			require.NotContains(t, strings.ToLower(test.traceparent), strings.ToLower(body.TraceID),
				"a refused traceparent must not decide the id anyway")
		})
	}
}

// TestADuplicateMemberIsRefusedAnywhereInTheBody covers the opaque block this
// transport passes through verbatim as well as the members it publishes.
func TestADuplicateMemberIsRefusedAnywhereInTheBody(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		body    string
		refused bool
	}{
		"a duplicate at the top level":     {body: `{"name":"abc","name":"xyz"}`, refused: true},
		"a value equal to its own name":    {body: `{"name":"name"}`},
		"a duplicate inside a nested type": {body: `{"name":"abc","nested":{"a":1,"a":2}}`, refused: true},
		"a duplicate inside an array":      {body: `{"name":"abc","list":[{"a":1,"a":2}]}`, refused: true},
		"the same name at two depths":      {body: `{"name":"abc","nested":{"name":"x"}}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := unmarshalStrictJSON([]byte(test.body), &map[string]any{})

			if test.refused {
				require.ErrorIs(t, err, errDuplicateMember)

				return
			}

			require.NoError(t, err)
		})
	}
}

// TestAPanicRefusesInProblemJSONAndSaysNothing checks the redaction as well as
// the status: a panic message is an internal detail and the code is registered
// to withhold it.
func TestAPanicRefusesInProblemJSONAndSaysNothing(t *testing.T) {
	t.Parallel()

	handler := newHarness(t, defaultBodyLimit, func(api huma.API) {
		register(api, huma.Operation{
			OperationID: "get-panic",
			Method:      http.MethodGet,
			Path:        "/panic",
			Summary:     "panics",
			Security:    noSecurity,
		}, func(_ context.Context, _ *struct{}) (*struct{}, error) {
			panic("the connection string is postgres://user:hunter2@db/printers")
		})
	})

	rec := call(t, handler, http.MethodGet, "/panic", "")

	body := requireProblem(t, rec, http.StatusInternalServerError)
	require.Equal(t, httpfault.CodeUnexpected(), body.Code)
	require.Empty(t, body.Detail, "a redacted code must not send its occurrence detail")
	require.NotContains(t, rec.Body.String(), "hunter2")
}

// TestADomainFaultReachesTheClientUnreshaped is the property the whole error
// path exists for: the code a domain package raised is the code the client
// branches on.
func TestADomainFaultReachesTheClientUnreshaped(t *testing.T) {
	t.Parallel()

	handler := newHarness(t, defaultBodyLimit, func(api huma.API) {
		register(api, huma.Operation{
			OperationID: "get-missing-printer",
			Method:      http.MethodGet,
			Path:        "/missing",
			Summary:     "always missing",
			Security:    noSecurity,
			Errors:      []int{http.StatusNotFound},
		}, func(_ context.Context, _ *struct{}) (*struct{}, error) {
			return nil, fault.PrinterNotFound.New("no printer with that id")
		})
	})

	rec := call(t, handler, http.MethodGet, "/missing", "")

	body := requireProblem(t, rec, http.StatusNotFound)
	require.Equal(t, fault.PrinterNotFound.Code(), body.Code)
	require.Equal(t, fault.PrinterNotFound.Code().TypeURI(), body.Type)
}

// probeInput is a body worth validating: the shortest thing that makes Huma
// produce a located validation error.
type probeInput struct {
	Body struct {
		Name string `json:"name" minLength:"3" doc:"A name of at least three characters."`
	}
}

const defaultBodyLimit = 1 << 20

func registerProbe(api huma.API) {
	register(api, huma.Operation{
		OperationID: "post-probe",
		Method:      http.MethodPost,
		Path:        "/probe",
		Summary:     "accepts a body",
		Security:    noSecurity,
	}, func(_ context.Context, _ *probeInput) (*struct{}, error) {
		return &struct{}{}, nil
	})
}

// newHarness builds the production middleware stack, error model and adapter
// around an operation set a test chooses, so error handling can be exercised
// without inventing production routes for it.
func newHarness(t *testing.T, bodyLimit int64, register func(huma.API)) http.Handler {
	t.Helper()

	return newLoggingHarness(t, bodyLimit, slog.New(slog.DiscardHandler), register)
}

// newLoggingHarness is newHarness with the logger the stack writes its failure
// lines through, so a test can read what an operator would read.
func newLoggingHarness(
	t *testing.T,
	bodyLimit int64,
	logger *slog.Logger,
	register func(huma.API),
) http.Handler {
	t.Helper()

	installErrorModel()

	cfg := Config{
		Principals:   fixedPrincipal("operator"),
		MaxBodyBytes: bodyLimit,
		Logger:       logger,
	}.withDefaults()

	mux := chi.NewMux()
	mux.Use(middlewareStack(cfg)...)
	mux.NotFound(notFound)
	mux.MethodNotAllowed(methodNotAllowed)

	api := humachi.New(mux, humaConfig(cfg))
	registerErrorSchemas(api.OpenAPI().Components.Schemas)
	api.UseMiddleware(principalMiddleware(api, cfg.Principals, cfg.Logger))

	register(api)

	return http.HandlerFunc(mux.ServeHTTP)
}

// serverWithoutIdentity is a server whose Principals refuses every caller.
func serverWithoutIdentity(t *testing.T) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal(""),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
	})
	require.NoError(t, err)

	return server
}

func call(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	var req *http.Request
	if reader == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

// requireProblem decodes an error response and asserts the framing every one of
// them shares, including that the code it carries is in the registry.
func requireProblem(t *testing.T, rec *httptest.ResponseRecorder, status int) problemBody {
	t.Helper()

	require.Equal(t, status, rec.Code, "body: %s", rec.Body.String())
	require.Equal(t, problemMediaType, rec.Header().Get("Content-Type"))

	faulttest.AssertBodyRegistered(t, fault.Global(), rec.Body.Bytes())

	var body problemBody

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, status, body.Status)
	require.NotEmpty(t, body.Title)
	require.Equal(t, body.Code.TypeURI(), body.Type)

	return body
}

func paramString(t *testing.T, params fault.Params, key string) string {
	t.Helper()

	value, ok := params.Get(key)
	require.Truef(t, ok, "no param %q in %v", key, params.Keys())
	require.Equal(t, fault.KindString, value.Kind())

	return value.String()
}

func paramInt(t *testing.T, params fault.Params, key string) string {
	t.Helper()

	value, ok := params.Get(key)
	require.Truef(t, ok, "no param %q in %v", key, params.Keys())
	require.Equal(t, fault.KindInt, value.Kind())

	return value.String()
}

func TestUISettingsNamesTheReferencePageWhenTheBuildServesIt(t *testing.T) {
	t.Parallel()

	rec := call(t, newTestServer(t).Handler(), http.MethodGet, appSettingsPath, "")
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	page, served := ReferencePage()
	if served {
		require.Equal(t, page, body["api_reference"])
	} else {
		require.NotContains(t, body, "api_reference")
	}
}
