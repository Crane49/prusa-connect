package httpapi

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// fakeEndpointStore is an in-memory stand-in for the overlay's endpoint
// section. It answers the two grants the driver kind separates, keyed by
// principal, so a test can prove that a caller who may read is still refused a
// write.
type fakeEndpointStore struct {
	mu sync.Mutex

	endpoints map[string]ConnectorEndpoint
	version   string

	mayRead  map[string]bool
	mayWrite map[string]bool
}

var _ EndpointStore = (*fakeEndpointStore)(nil)

func seededEndpoints() *fakeEndpointStore {
	return &fakeEndpointStore{
		version:  "v1",
		mayRead:  map[string]bool{"user:operator": true, "operator": true},
		mayWrite: map[string]bool{"user:operator": true, "operator": true},
		endpoints: map[string]ConnectorEndpoint{
			"bench-printer": {
				EndpointSpec: EndpointSpec{
					Connector: "moonraker",
					Address:   "192.168.1.42:7125",
					Enabled:   true,
				},
				Name:    "bench-printer",
				Version: "v1",
			},
		},
	}
}

func (s *fakeEndpointStore) ListEndpoints(
	_ context.Context, principal string,
) (EndpointListing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	listing := EndpointListing{Version: s.version, Items: []ConnectorEndpoint{}}
	if !s.mayRead[principal] {
		return EndpointListing{Items: []ConnectorEndpoint{}}, nil
	}

	for _, name := range slices.Sorted(maps.Keys(s.endpoints)) {
		listing.Items = append(listing.Items, s.endpoints[name])
	}

	return listing, nil
}

func (s *fakeEndpointStore) ApplyEndpoint(
	_ context.Context, principal, name string, desired EndpointSpec, pre Precondition,
) (EndpointResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.endpoints[name]

	if !s.mayWrite[principal] {
		if s.mayRead[principal] {
			return EndpointResult{}, fault.New(authz.CodeAccessDenied(),
				"this caller may read endpoints and may not write them")
		}

		return EndpointResult{}, httpfault.ResourceNotFound(httpfault.Reference("connector_endpoint", name))
	}

	if !exists && !pre.MustNotExist && len(pre.IfNoneMatch) == 0 {
		return EndpointResult{}, httpfault.ResourceNotFound(httpfault.Reference("connector_endpoint", name))
	}

	if !pre.Holds(exists, s.version) {
		return EndpointResult{}, httpfault.VersionConflict(httpfault.Reference("connector_endpoint", name), s.version)
	}

	stored := ConnectorEndpoint{EndpointSpec: desired, Name: name, Version: s.version + "+"}

	if pre.DryRun {
		return EndpointResult{Endpoint: stored, Created: !exists}, nil
	}

	s.version += "+"
	stored.Version = s.version
	s.endpoints[name] = stored

	return EndpointResult{Endpoint: stored, Created: !exists}, nil
}

func (s *fakeEndpointStore) DeleteEndpoint(
	_ context.Context, principal, name string, pre Precondition,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.endpoints[name]

	if !s.mayWrite[principal] || !exists {
		return httpfault.ResourceNotFound(httpfault.Reference("connector_endpoint", name))
	}

	if !pre.Holds(exists, s.version) {
		return httpfault.VersionConflict(httpfault.Reference("connector_endpoint", name), s.version)
	}

	if pre.DryRun {
		return nil
	}

	delete(s.endpoints, name)
	s.version += "+"

	return nil
}

func endpointServer(t *testing.T, store *fakeEndpointStore) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  store,
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

func decodeEndpoint(t *testing.T, body []byte) ConnectorEndpoint {
	t.Helper()

	var endpoint ConnectorEndpoint
	require.NoError(t, json.Unmarshal(body, &endpoint))

	return endpoint
}

// TestAnEndpointIsAddedListedChangedAndRemoved is the story the demo needs: a
// printer that listens is added over HTTP, appears in the listing, is switched
// off, and is taken away again.
func TestAnEndpointIsAddedListedChangedAndRemoved(t *testing.T) {
	t.Parallel()

	store := seededEndpoints()
	server := endpointServer(t, store)

	rec := write(t, server.Handler(), http.MethodPut, "/api/v1/connector-endpoints/shop-floor",
		http.Header{"If-None-Match": {anyTag}},
		`{"connector":"moonraker","address":"10.0.0.7:7125","enabled":true}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, "/api/v1/connector-endpoints/shop-floor", rec.Header().Get("Location"))

	created := decodeEndpoint(t, rec.Body.Bytes())
	require.Equal(t, "shop-floor", created.Name)
	require.Equal(t, "10.0.0.7:7125", created.Address)
	require.True(t, created.Enabled)

	etag := rec.Header().Get("ETag")
	require.NotEmpty(t, etag)

	rec = call(t, server.Handler(), http.MethodGet, "/api/v1/connector-endpoints", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var listing struct {
		Items []ConnectorEndpoint `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listing))
	require.Len(t, listing.Items, 2)
	require.Equal(t, etag, rec.Header().Get("ETag"))

	rec = write(t, server.Handler(), http.MethodPut, "/api/v1/connector-endpoints/shop-floor",
		http.Header{"If-Match": {etag}},
		`{"connector":"moonraker","address":"10.0.0.7:7125","enabled":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.False(t, decodeEndpoint(t, rec.Body.Bytes()).Enabled)
	require.Empty(t, rec.Header().Get("Location"))

	rec = write(t, server.Handler(), http.MethodDelete, "/api/v1/connector-endpoints/shop-floor",
		http.Header{"If-Match": {rec.Header().Get("ETag")}}, "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	store.mu.Lock()
	defer store.mu.Unlock()
	require.NotContains(t, store.endpoints, "shop-floor")
}

// TestAnEndpointWriteWithNoTokenIsRefused holds this surface to the fencing
// rule the resource surface already has. The overlay has another writer, so a
// client that read it a minute ago must not overwrite what landed since.
func TestAnEndpointWriteWithNoTokenIsRefused(t *testing.T) {
	t.Parallel()

	server := endpointServer(t, seededEndpoints())

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		body := ""
		if method == http.MethodPut {
			body = `{"connector":"moonraker","address":"10.0.0.7:7125","enabled":true}`
		}

		rec := call(t, server.Handler(), method, "/api/v1/connector-endpoints/bench-printer", body)
		require.Equal(t, http.StatusPreconditionRequired, rec.Code, "%s: %s", method, rec.Body.String())
	}
}

// TestADryRunEndpointWriteChangesNothingAndQuotesNoToken is the rehearsal an
// interface runs before it commits: the declaration is validated, and the
// answer carries no version because no state that version could name exists.
func TestADryRunEndpointWriteChangesNothingAndQuotesNoToken(t *testing.T) {
	t.Parallel()

	store := seededEndpoints()
	server := endpointServer(t, store)

	rec := write(t, server.Handler(),
		http.MethodPut, "/api/v1/connector-endpoints/rehearsed?dry_run=true",
		http.Header{"If-None-Match": {anyTag}},
		`{"connector":"moonraker","address":"10.0.0.9:7125","enabled":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Empty(t, rec.Header().Get("ETag"))
	require.Empty(t, decodeEndpoint(t, rec.Body.Bytes()).Version)

	store.mu.Lock()
	defer store.mu.Unlock()
	require.NotContains(t, store.endpoints, "rehearsed")
}

// TestAnEndpointBodyNamingAnotherEndpointIsRefused is the same rule the
// resource and preset writes carry: the body is echoed back from a listing, and
// a disagreement with the path is a client bug rather than a silent choice.
func TestAnEndpointBodyNamingAnotherEndpointIsRefused(t *testing.T) {
	t.Parallel()

	server := endpointServer(t, seededEndpoints())

	rec := write(t, server.Handler(), http.MethodPut, "/api/v1/connector-endpoints/bench-printer",
		http.Header{"If-Match": {`"v1"`}},
		`{"name":"other","connector":"moonraker","address":"10.0.0.7:7125","enabled":true}`)
	requireProblem(t, rec, http.StatusUnprocessableEntity)
}

// TestAnEndpointDeclarationWithNoAddressIsRefused holds the schema the
// generated client reads. A row with no address is one the dial loop could
// never dial.
func TestAnEndpointDeclarationWithNoAddressIsRefused(t *testing.T) {
	t.Parallel()

	server := endpointServer(t, seededEndpoints())

	rec := write(t, server.Handler(), http.MethodPut, "/api/v1/connector-endpoints/bench-printer",
		http.Header{"If-Match": {`"v1"`}},
		`{"connector":"moonraker","enabled":true}`)
	requireProblem(t, rec, http.StatusUnprocessableEntity)
}

// TestACallerWhoMayReadEndpointsIsStillRefusedAWrite is the refusal half of the
// two grants the driver kind separates. A permit test without it is half a
// test.
func TestACallerWhoMayReadEndpointsIsStillRefusedAWrite(t *testing.T) {
	t.Parallel()

	store := seededEndpoints()
	store.mayWrite = map[string]bool{}
	server := endpointServer(t, store)

	rec := call(t, server.Handler(), http.MethodGet, "/api/v1/connector-endpoints", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = write(t, server.Handler(), http.MethodPut, "/api/v1/connector-endpoints/bench-printer",
		http.Header{"If-Match": {`"v1"`}},
		`{"connector":"moonraker","address":"10.0.0.7:7125","enabled":true}`)
	requireProblem(t, rec, http.StatusForbidden)
}

// TestACallerWhoMaySeeNoEndpointIsAnsweredAnEmptyListAndNoToken is the oracle
// rule the preset listing already holds: the version covers the whole overlay,
// so handing it to somebody who may read none of it reports on a configuration
// they cannot see.
func TestACallerWhoMaySeeNoEndpointIsAnsweredAnEmptyListAndNoToken(t *testing.T) {
	t.Parallel()

	store := seededEndpoints()
	store.mayRead = map[string]bool{}
	server := endpointServer(t, store)

	rec := call(t, server.Handler(), http.MethodGet, "/api/v1/connector-endpoints", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Empty(t, rec.Header().Get("ETag"))

	var listing struct {
		Items []ConnectorEndpoint `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listing))
	require.Empty(t, listing.Items)
	require.NotNil(t, listing.Items)
}

// TestAServerWithNoEndpointStoreDoesNotStart holds the seam every other store
// on this surface is held to. The routes are published whatever a deployment
// declared, so something has to answer them.
func TestAServerWithNoEndpointStoreDoesNotStart(t *testing.T) {
	t.Parallel()

	_, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
	})
	require.ErrorIs(t, err, ErrNoEndpointStore)
}
