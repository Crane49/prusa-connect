package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// exampleLinks is the links plane as the samples describe it: one link, read
// back from the published example so the sample and the fake cannot drift.
type exampleLinks struct {
	mu sync.Mutex

	items    []LinkInstance
	approved []string
	rejected []string
	revoked  []string
	// mayOperate is the principals allowed to decide, so the refusal has a
	// test of its own beside the permit.
	mayOperate map[string]bool
}

var _ LinkStore = (*exampleLinks)(nil)

func (s *exampleLinks) ListLinks(_ context.Context, _ string) (LinkListing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return LinkListing{Items: s.items}, nil
}

func (s *exampleLinks) GetLink(_ context.Context, _, name string) (LinkInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.find(name)
}

func (s *exampleLinks) ApproveLinkNode(
	_ context.Context, principal, name, node string,
) (LinkInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	link, err := s.find(name)
	if err != nil {
		return LinkInstance{}, err
	}

	if s.mayOperate != nil && !s.mayOperate[principal] {
		return LinkInstance{}, httpfault.ResourceNotFound(httpfault.Reference("node", node))
	}

	s.approved = append(s.approved, name+"/"+node)

	return link, nil
}

func (s *exampleLinks) RejectLinkNode(
	_ context.Context, principal, name, node string,
) (LinkInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	link, err := s.find(name)
	if err != nil {
		return LinkInstance{}, err
	}

	if s.mayOperate != nil && !s.mayOperate[principal] {
		return LinkInstance{}, httpfault.ResourceNotFound(httpfault.Reference("node", node))
	}

	s.rejected = append(s.rejected, name+"/"+node)

	return link, nil
}

func (s *exampleLinks) RevokeLinkNode(
	_ context.Context, principal, name, node string,
) (LinkInstance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	link, err := s.find(name)
	if err != nil {
		return LinkInstance{}, err
	}

	if s.mayOperate != nil && !s.mayOperate[principal] {
		return LinkInstance{}, httpfault.ResourceNotFound(httpfault.Reference("node", node))
	}

	s.revoked = append(s.revoked, name+"/"+node)

	return link, nil
}

// find is the lookup both reads share. The caller holds the lock.
func (s *exampleLinks) find(name string) (LinkInstance, error) {
	for _, item := range s.items {
		if item.Name == name {
			return item, nil
		}
	}

	return LinkInstance{}, httpfault.ResourceNotFound(httpfault.Reference("link", name))
}

// seededLinks is the fake the tests below drive, holding the published sample.
func seededLinks() *exampleLinks {
	return &exampleLinks{
		items:      []LinkInstance{exampleLink()},
		mayOperate: map[string]bool{"user:operator": true},
	}
}

func serverWithLinks(t *testing.T, links LinkStore) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Links:      links,
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

// ask sends one request through the router and returns the recorder.
func askLinks(t *testing.T, server *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(method, path, nil))

	return recorder
}

// TestTheLinkListingCarriesEveryNodeTheLinkMeasured is the shape the fractal
// page reads: this site's own identity, the site above it, the listeners it
// opened, and one site under it with its liveness and the round trip.
func TestTheLinkListingCarriesEveryNodeTheLinkMeasured(t *testing.T) {
	t.Parallel()

	recorder := askLinks(t, serverWithLinks(t, seededLinks()), http.MethodGet, "/api/v1/links")
	require.Equal(t, http.StatusOK, recorder.Code)

	var body LinkListing
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)

	link := body.Items[0]
	require.Equal(t, "link", link.Kind)
	require.True(t, link.Enabled)
	require.True(t, link.Running)
	require.Equal(t, "parent", link.Mode)
	require.Equal(t, "inherit", link.Enrolment)
	require.True(t, link.Accepting)
	require.NotEmpty(t, link.NodeID)
	require.NotEmpty(t, link.NodePath)
	require.NotEmpty(t, link.PublicKey)

	require.NotNil(t, link.Uplink)
	require.Equal(t, "connected", link.Uplink.State)
	require.NotNil(t, link.Uplink.ConnectedAt)

	require.Len(t, link.Listeners, 1)
	require.Equal(t, 4222, link.Listeners[0].Port)

	require.Len(t, link.Nodes, 1)
	require.Equal(t, "child", link.Nodes[0].Role)
	require.NotEmpty(t, link.Nodes[0].Path)
	require.True(t, link.Nodes[0].Alive)
	require.NotNil(t, link.Nodes[0].LastHeartbeat)
	require.NotNil(t, link.Nodes[0].LatencyMS)
}

// TestOneLinkIsReadByTheNameTheOperatorGaveIt pins the lookup and the refusal
// beside it, which are the same answer for a name that is not declared.
func TestOneLinkIsReadByTheNameTheOperatorGaveIt(t *testing.T) {
	t.Parallel()

	server := serverWithLinks(t, seededLinks())

	found := askLinks(t, server, http.MethodGet, "/api/v1/links/"+exampleLinkName)
	require.Equal(t, http.StatusOK, found.Code)

	missing := askLinks(t, server, http.MethodGet, "/api/v1/links/nowhere")
	require.Equal(t, http.StatusNotFound, missing.Code)
}

// TestApprovingANodeReachesTheLink is the operator's half of explicit
// enrolment, and the refusal for a node no link holds.
func TestApprovingANodeReachesTheLink(t *testing.T) {
	t.Parallel()

	links := seededLinks()
	server := serverWithLinks(t, links)

	approved := askLinks(t, server, http.MethodPost,
		"/api/v1/links/"+exampleLinkName+"/nodes/"+exampleNodeID+"/approve")
	require.Equal(t, http.StatusOK, approved.Code)
	require.Equal(t, []string{exampleLinkName + "/" + exampleNodeID}, links.approved)

	missing := askLinks(t, server, http.MethodPost,
		"/api/v1/links/nowhere/nodes/"+exampleNodeID+"/approve")
	require.Equal(t, http.StatusNotFound, missing.Code)
}

// TestRejectingANodeReachesTheLink is the decision that clears a name nobody
// wants to admit, on the same path shape and with the same refusal for a link
// nobody declared.
func TestRejectingANodeReachesTheLink(t *testing.T) {
	t.Parallel()

	links := seededLinks()
	server := serverWithLinks(t, links)

	rejected := askLinks(t, server, http.MethodPost,
		"/api/v1/links/"+exampleLinkName+"/nodes/"+exampleNodeID+"/reject")
	require.Equal(t, http.StatusOK, rejected.Code)
	require.Equal(t, []string{exampleLinkName + "/" + exampleNodeID}, links.rejected)

	missing := askLinks(t, server, http.MethodPost,
		"/api/v1/links/nowhere/nodes/"+exampleNodeID+"/reject")
	require.Equal(t, http.StatusNotFound, missing.Code)
}

// TestRevokingANodeReachesTheLink is the other operator decision, on the same
// path shape and with the same refusal for a link nobody declared.
func TestRevokingANodeReachesTheLink(t *testing.T) {
	t.Parallel()

	links := seededLinks()
	server := serverWithLinks(t, links)

	revoked := askLinks(t, server, http.MethodPost,
		"/api/v1/links/"+exampleLinkName+"/nodes/"+exampleNodeID+"/revoke")
	require.Equal(t, http.StatusOK, revoked.Code)
	require.Equal(t, []string{exampleLinkName + "/" + exampleNodeID}, links.revoked)

	missing := askLinks(t, server, http.MethodPost,
		"/api/v1/links/nowhere/nodes/"+exampleNodeID+"/revoke")
	require.Equal(t, http.StatusNotFound, missing.Code)
}

// TestABuildWithNoLinkPlaneStillPublishesTheLinkPaths is why the operations are
// registered whether or not a link type is compiled in: the document is a
// contract, so the paths answer rather than 404 as unrouted.
func TestABuildWithNoLinkPlaneStillPublishesTheLinkPaths(t *testing.T) {
	t.Parallel()

	server := serverWithLinks(t, nil)

	listed := askLinks(t, server, http.MethodGet, "/api/v1/links")
	require.Equal(t, http.StatusOK, listed.Code)

	var body LinkListing
	require.NoError(t, json.Unmarshal(listed.Body.Bytes(), &body))
	require.Empty(t, body.Items)

	named := askLinks(t, server, http.MethodGet, "/api/v1/links/"+exampleLinkName)
	require.Equal(t, http.StatusNotFound, named.Code)
}
