//go:build diagnostics

package httpapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

type fakeDiagnostics struct {
	principal  string
	modules    bool
	extensions bool
	frontend   []byte
	hidden     bool
	titles     []string
}

func (f *fakeDiagnostics) Bundle(
	_ context.Context, principal string, choice DiagnosticsChoice, frontend []byte,
) ([]byte, error) {
	f.principal, f.modules, f.extensions, f.frontend = principal, choice.Modules, choice.Extensions, frontend

	return []byte("PK-fake"), nil
}

func (f *fakeDiagnostics) Served() bool { return !f.hidden }

func (f *fakeDiagnostics) Extensions() []string { return f.titles }

func serverWithDiagnostics(t *testing.T, diagnostics Diagnostics, grants GrantStore) http.Handler {
	t.Helper()

	cfg := exampleConfig(&fakeFleet{}, seededStore(), seededPresets())
	cfg.Diagnostics = diagnostics
	cfg.Grants = grants

	server, err := New(cfg)
	require.NoError(t, err)

	return server.Handler()
}

func TestDiagnosticsServesAZipToAnAdmin(t *testing.T) {
	t.Parallel()

	fake := &fakeDiagnostics{}
	handler := serverWithDiagnostics(t, fake, &fakeGrantStore{})

	rec := call(t, handler, http.MethodPost, "/api/v1/diagnostics?modules=true", `[{"message":"x"}]`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Header().Get("Content-Disposition"), `filename="pcl-diagnostics-test-`)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, "PK-fake", rec.Body.String())
	require.Equal(t, "user:alice", fake.principal)
	require.True(t, fake.modules)
	require.False(t, fake.extensions, "extension files were included without being asked for")
	require.JSONEq(t, `[{"message":"x"}]`, string(fake.frontend))
}

// The admin includes the extensions' files with extensions=true, and reads
// beforehand which extensions add any (ADR-20261019-nrq rules 8 and 9).
func TestDiagnosticsIncludesExtensionFilesOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	fake := &fakeDiagnostics{titles: []string{"Printer logs"}}
	handler := serverWithDiagnostics(t, fake, &fakeGrantStore{})

	rec := call(t, handler, http.MethodGet, "/api/v1/diagnostics", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.JSONEq(t, `{"extensions":["Printer logs"]}`, rec.Body.String())

	rec = call(t, handler, http.MethodPost, "/api/v1/diagnostics?extensions=true", "[]")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.True(t, fake.extensions)
	require.False(t, fake.modules)
}

// The list of extensions names installed modules, so it takes an admin.
func TestDiagnosticsInfoRefusesSomeoneWhoIsNotAnAdmin(t *testing.T) {
	t.Parallel()

	refused := fault.New(authz.CodeAccessDenied(), "the subject holds no admin role on this org")
	handler := serverWithDiagnostics(t, &fakeDiagnostics{titles: []string{"Printer logs"}},
		&fakeGrantStore{refuse: refused})

	rec := call(t, handler, http.MethodGet, "/api/v1/diagnostics", "")
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "Printer logs")
}

func TestDiagnosticsRefusesSomeoneWhoIsNotAnAdmin(t *testing.T) {
	t.Parallel()

	fake := &fakeDiagnostics{}
	refused := fault.New(authz.CodeAccessDenied(), "the subject holds no admin role on this org")
	handler := serverWithDiagnostics(t, fake, &fakeGrantStore{refuse: refused})

	rec := call(t, handler, http.MethodPost, "/api/v1/diagnostics", "[]")

	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Empty(t, fake.principal, "a bundle was built for a caller who is no organization admin")
}

func TestDiagnosticsIsNotFoundWhileTheBundleIsNotServed(t *testing.T) {
	t.Parallel()

	fake := &fakeDiagnostics{hidden: true}
	handler := serverWithDiagnostics(t, fake, &fakeGrantStore{})

	rec := call(t, handler, http.MethodPost, "/api/v1/diagnostics", "[]")
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Empty(t, fake.principal, "a bundle was built while it is not served")

	rec = call(t, handler, http.MethodGet, "/api/v1/ui", "")
	require.Contains(t, rec.Body.String(), `"diagnostics":false`)
}

func TestDiagnosticsIsNotFoundWhereNoBundleIsWired(t *testing.T) {
	t.Parallel()

	rec := call(t, serverWithDiagnostics(t, nil, &fakeGrantStore{}), http.MethodPost, "/api/v1/diagnostics", "[]")

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

type blockingDiagnostics struct{ started, release chan struct{} }

func (blockingDiagnostics) Served() bool { return true }

func (blockingDiagnostics) Extensions() []string { return nil }

func (b blockingDiagnostics) Bundle(context.Context, string, DiagnosticsChoice, []byte) ([]byte, error) {
	close(b.started)
	<-b.release

	return []byte("PK-fake"), nil
}

func TestDiagnosticsBuildsOneBundleAtATime(t *testing.T) {
	t.Parallel()

	slow := blockingDiagnostics{started: make(chan struct{}), release: make(chan struct{})}
	handler := serverWithDiagnostics(t, slow, &fakeGrantStore{})

	first := make(chan int)
	go func() { first <- call(t, handler, http.MethodPost, "/api/v1/diagnostics", "[]").Code }()
	<-slow.started

	rec := call(t, handler, http.MethodPost, "/api/v1/diagnostics", "[]")
	require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "core.validation/DIAGNOSTICS_BUSY")

	close(slow.release)
	require.Equal(t, http.StatusOK, <-first)
}
