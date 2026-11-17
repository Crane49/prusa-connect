package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// storageGrants is the path the cases below drive: the grants written on the
// storage resource kind, which is the object a resource grant is held on.
const storageGrants = "/api/v1/grants/resource_kind/storage"

// TestTheObjectTypeEnumMatchesWhatCanBeAdministered reads the published enum
// against the vocabulary. A struct tag cannot hold a call, so the enum is a
// second copy of a closed set, and a type the graph gains an administrator for
// while this list stays put would be administrable by the graph and refused by
// the schema.
func TestTheObjectTypeEnumMatchesWhatCanBeAdministered(t *testing.T) {
	t.Parallel()

	var administrable []string

	for _, typ := range authz.Types() {
		if authz.Administrable(typ) {
			administrable = append(administrable, typ.String())
		}
	}

	require.Equal(t, administrable, strings.Split(administrableTypes, ","))

	document := map[string]any{}
	require.NoError(t, json.Unmarshal(renderedDocument(t, newTestServer(t)), &document))

	require.Equal(t, administrable, publishedEnum(t, document, "object_type"),
		"the document publishes a different set of object types from the vocabulary")
}

// TestAGrantIsWrittenFromThePathAndTheBody is the permit half: the tuple the
// graph is handed is the one the request named, and the status separates the
// write that added an edge from the one that found it already there.
func TestAGrantIsWrittenFromThePathAndTheBody(t *testing.T) {
	t.Parallel()

	graph := &fakeGrantStore{wrote: true}
	handler := serverWithGrants(t, graph).Handler()

	rec := call(t, handler, http.MethodPost, storageGrants,
		`{"relation":"viewer","subject":"user:alice"}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, "resource_kind:storage#viewer@user:alice", graph.written[0].String())
	require.Equal(t, "user:operator", graph.callers[0].String(),
		"the graph was handed a caller the middleware did not resolve")

	var grant Grant
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &grant))
	require.Equal(t, Grant{
		Object: "resource_kind:storage", Relation: "viewer", Subject: "user:alice",
	}, grant)

	graph.wrote = false

	rec = call(t, handler, http.MethodPost, storageGrants,
		`{"relation":"viewer","subject":"user:alice"}`)
	require.Equal(t, http.StatusOK, rec.Code,
		"a grant the graph already held reported that it had created one")
}

// TestAGrantIsWithdrawnFromThePathAndTheQuery is the same for the delete, which
// carries the other two coordinates of the tuple as query parameters.
func TestAGrantIsWithdrawnFromThePathAndTheQuery(t *testing.T) {
	t.Parallel()

	graph := &fakeGrantStore{}
	handler := serverWithGrants(t, graph).Handler()

	rec := call(t, handler, http.MethodDelete,
		storageGrants+"?relation=viewer&subject=user:alice", "")

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Equal(t, "resource_kind:storage#viewer@user:alice", graph.withdrawn[0].String())
}

// TestTheListReturnsTheTuplesTheGraphHolds is the read half. The object is
// echoed on every item, so a client collecting grants from several objects has
// items that say what they are.
func TestTheListReturnsTheTuplesTheGraphHolds(t *testing.T) {
	t.Parallel()

	graph := &fakeGrantStore{held: []authz.Tuple{
		grantTuple(t, authz.TypeResourceKind, "storage", authz.RelViewer, "user:alice"),
		grantTuple(t, authz.TypeResourceKind, "storage", authz.RelOrg, "org:local"),
	}}

	rec := call(t, serverWithGrants(t, graph).Handler(), http.MethodGet, storageGrants, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Items []Grant `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, []Grant{
		{Object: "resource_kind:storage", Relation: "viewer", Subject: "user:alice"},
		{Object: "resource_kind:storage", Relation: "org", Subject: "org:local"},
	}, body.Items)
	require.Equal(t, "resource_kind:storage", graph.read[0].String())
}

// TestTheGraphsRefusalReachesTheClientUnreshaped is the property the whole
// surface rests on: what the graph decided is what the caller is told, with the
// registered code and the status that code declares.
func TestTheGraphsRefusalReachesTheClientUnreshaped(t *testing.T) {
	t.Parallel()

	instance, err := ref.New(ref.KindStorage, "local")
	require.NoError(t, err)

	graph := &fakeGrantStore{refuse: authz.Denied(authz.ActionConfigureResource, instance)}
	handler := serverWithGrants(t, graph).Handler()

	for name, request := range map[string]struct {
		method, path, body string
	}{
		"list":     {method: http.MethodGet, path: storageGrants},
		"write":    {method: http.MethodPost, path: storageGrants, body: `{"relation":"viewer","subject":"user:alice"}`},
		"withdraw": {method: http.MethodDelete, path: storageGrants + "?relation=viewer&subject=user:alice"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := call(t, handler, request.method, request.path, request.body)
			body := requireProblem(t, rec, http.StatusForbidden)

			require.Equal(t, authz.CodeAccessDenied(), body.Code)
		})
	}
}

// TestTheVocabularyRefusesAGrantTheGraphWouldNotTake keeps the constructors on
// the request path. A relation that does not exist on this object type is
// refused before the graph is asked, with the code that names which, and no
// call reaches the store.
func TestTheVocabularyRefusesAGrantTheGraphWouldNotTake(t *testing.T) {
	t.Parallel()

	for name, request := range map[string]struct {
		body   string
		status int
		code   string
	}{
		"unknown relation": {
			body:   `{"relation":"editor","subject":"user:alice"}`,
			status: http.StatusBadRequest,
			code:   authz.CodeUnknownRelation().String(),
		},
		"subject shape the relation does not accept": {
			body:   `{"relation":"org","subject":"user:alice"}`,
			status: http.StatusBadRequest,
			code:   authz.CodeInvalidSubject().String(),
		},
		"subject that is not type:id": {
			body:   `{"relation":"viewer","subject":"alice"}`,
			status: http.StatusBadRequest,
			code:   authz.CodeInvalidID().String(),
		},
		"body naming another object": {
			body:   `{"object":"resource_kind:sink","relation":"viewer","subject":"user:alice"}`,
			status: http.StatusUnprocessableEntity,
			code:   "core.validation/REQUEST_INVALID",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			graph := &fakeGrantStore{}

			rec := call(t, serverWithGrants(t, graph).Handler(), http.MethodPost, storageGrants, request.body)

			require.Equal(t, request.code, requireProblem(t, rec, request.status).Code.String())
			require.Empty(t, graph.written, "the graph was asked to write a tuple it would have refused")
		})
	}
}

// TestAWithdrawalRefusesAParameterItDoesNotPublish is what stops a caller
// believing a parameter this operation never read narrowed what it deleted.
//
// Every published coordinate is present and correct, so the only thing left to
// refuse is the parameter that is not published. Leaving one of the required
// ones out instead would pass against a server with no strictness at all.
func TestAWithdrawalRefusesAParameterItDoesNotPublish(t *testing.T) {
	t.Parallel()

	graph := &fakeGrantStore{}

	rec := call(t, serverWithGrants(t, graph).Handler(), http.MethodDelete,
		storageGrants+"?relation=viewer&subject=user:alice&subjekt=user:bob", "")

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Empty(t, graph.withdrawn, "a parameter the operation does not publish withdrew a grant")
}

// TestAWithdrawalNamesTheWholeTuple refuses a delete that leaves a coordinate
// out, rather than treating the missing one as a wildcard.
func TestAWithdrawalNamesTheWholeTuple(t *testing.T) {
	t.Parallel()

	graph := &fakeGrantStore{}

	rec := call(t, serverWithGrants(t, graph).Handler(), http.MethodDelete,
		storageGrants+"?relation=viewer", "")

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Empty(t, graph.withdrawn, "a delete with no subject removed something")
}

// TestGrantsOnATypeThisSurfaceCannotDecideAreRefused holds the one rule that
// decides which objects this surface governs. A group and a printer group carry
// grants and no owning level, so nothing can authorize a write on one and the
// request is refused by the schema before the graph is asked.
//
// An org is on the other side of that line and is checked here too, because a
// path the schema refuses and a path it admits are the two halves of one enum:
// whoever administers the org decides that request, in the graph.
func TestGrantsOnATypeThisSurfaceCannotDecideAreRefused(t *testing.T) {
	t.Parallel()

	graph := &fakeGrantStore{}
	handler := serverWithGrants(t, graph).Handler()

	for _, path := range []string{"/api/v1/grants/group/maintenance", "/api/v1/grants/device_group/floor-one"} {
		rec := call(t, handler, http.MethodGet, path, "")

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
		require.Empty(t, graph.read, "the graph was asked about an object type it cannot administer")
	}

	rec := call(t, handler, http.MethodGet, "/api/v1/grants/org/local", "")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "org:local", graph.read[0].String(),
		"an org reached the schema and not the graph, so nobody can write a fleet-wide grant")
}

// TestEveryGrantRouteRefusesACallerWithNoIdentity is the refusal half of the
// identity seam on this surface. A permit test without it is half a test.
func TestEveryGrantRouteRefusesACallerWithNoIdentity(t *testing.T) {
	t.Parallel()

	graph := &fakeGrantStore{}

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal(""),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     graph,
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
	})
	require.NoError(t, err)

	handler := server.Handler()

	for name, request := range map[string]struct {
		method, path, body string
	}{
		"list":     {method: http.MethodGet, path: storageGrants},
		"write":    {method: http.MethodPost, path: storageGrants, body: `{"relation":"viewer","subject":"user:alice"}`},
		"withdraw": {method: http.MethodDelete, path: storageGrants + "?relation=viewer&subject=user:alice"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			requireProblem(t, call(t, handler, request.method, request.path, request.body),
				http.StatusUnauthorized)
		})
	}

	require.Empty(t, graph.read)
	require.Empty(t, graph.written)
	require.Empty(t, graph.withdrawn)
}

// publishedEnum reads the enum of a path parameter out of the served document.
func publishedEnum(t *testing.T, document map[string]any, name string) []string {
	t.Helper()

	paths, _ := document["paths"].(map[string]any)
	path, _ := paths[grantsPath].(map[string]any)
	get, _ := path["get"].(map[string]any)
	parameters, _ := get["parameters"].([]any)

	for _, entry := range parameters {
		parameter, _ := entry.(map[string]any)
		if parameter["name"] != name {
			continue
		}

		schema, _ := parameter["schema"].(map[string]any)
		values, _ := schema["enum"].([]any)

		out := make([]string, 0, len(values))
		for _, value := range values {
			out = append(out, value.(string))
		}

		return out
	}

	t.Fatalf("the document publishes no %s parameter", name)

	return nil
}

func grantTuple(t *testing.T, typ authz.Type, id string, relation authz.Relation, subject string) authz.Tuple {
	t.Helper()

	object, err := authz.NewObject(typ, id)
	require.NoError(t, err)

	who, err := authz.ParseSubject(subject)
	require.NoError(t, err)

	tuple, err := authz.NewTuple(object, relation, who)
	require.NoError(t, err)

	return tuple
}

// fakeGrantStore records what the surface handed the graph. It decides nothing:
// the permission to administer permissions is the graph's, and a stand-in that
// made that decision here would be asserting against a second copy of it.
type fakeGrantStore struct {
	mu        sync.Mutex
	held      []authz.Tuple
	wrote     bool
	refuse    error
	read      []authz.Object
	written   []authz.Tuple
	withdrawn []authz.Tuple
	roles     []authz.Tuple
	callers   []authz.Subject
}

var _ GrantStore = (*fakeGrantStore)(nil)

// CanVerb holds nothing, for the reason Can does.
func (g *fakeGrantStore) CanVerb(
	_ context.Context, _ authz.Subject, _ authz.Object, _ string,
) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	return false, g.refuse
}

// Can holds nothing. A test that needs a caller who holds an action embeds this
// and answers the question itself, so a surface reaching the graph by accident
// is refused rather than allowed.
func (g *fakeGrantStore) Can(
	_ context.Context, _ authz.Subject, _ authz.Action, _ authz.Object,
) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	return false, g.refuse
}

func (g *fakeGrantStore) Grants(
	_ context.Context, who authz.Subject, object authz.Object,
) ([]authz.Tuple, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.refuse != nil {
		return nil, g.refuse
	}

	g.callers = append(g.callers, who)
	g.read = append(g.read, object)

	return g.held, nil
}

func (g *fakeGrantStore) WriteGrant(
	_ context.Context, who authz.Subject, grant authz.Tuple,
) (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.refuse != nil {
		return false, g.refuse
	}

	g.callers = append(g.callers, who)
	g.written = append(g.written, grant)

	return g.wrote, nil
}

func (g *fakeGrantStore) WithdrawGrant(_ context.Context, who authz.Subject, grant authz.Tuple) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.refuse != nil {
		return g.refuse
	}

	g.callers = append(g.callers, who)
	g.withdrawn = append(g.withdrawn, grant)

	return nil
}

// SetOrgRole records the role a surface asked for, as the tuple the real store
// would write, so a test reads one shape whichever call put it there.
func (g *fakeGrantStore) SetOrgRole(
	_ context.Context, who authz.Subject, account authz.Subject, role authz.Relation,
) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.refuse != nil {
		return g.refuse
	}

	org, err := authz.NewObject(authz.TypeOrg, "local")
	if err != nil {
		return err
	}

	grant, err := authz.NewTuple(org, role, account)
	if err != nil {
		return err
	}

	g.callers = append(g.callers, who)
	g.roles = append(g.roles, grant)

	return nil
}

// serverWithGrants is a server whose authorization graph is the caller's.
func serverWithGrants(t *testing.T, graph GrantStore) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Principals: fixedPrincipal("user:operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     graph,
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

// TestAGrantOnAServerThatJoinedThisOneIsWrittenAndListed is the route half of
// enrolment. An administrator writes what a server above this one may do to one
// printer, so the subject has to cross this surface as it is written and come
// back in the listing as it was stored.
func TestAGrantOnAServerThatJoinedThisOneIsWrittenAndListed(t *testing.T) {
	t.Parallel()

	const printerGrants = "/api/v1/grants/device/p1"

	graph := &fakeGrantStore{wrote: true}
	handler := serverWithGrants(t, graph).Handler()

	rec := call(t, handler, http.MethodPost, printerGrants,
		`{"relation":"operator","subject":"node:hq"}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, "device:p1#operator@node:hq", graph.written[0].String())

	graph.held = []authz.Tuple{grantTuple(t, authz.TypeDevice, "p1", authz.RelOperator, "node:hq")}

	rec = call(t, handler, http.MethodGet, printerGrants, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Items []Grant `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, []Grant{
		{Object: "device:p1", Relation: "operator", Subject: "node:hq"},
	}, body.Items)
}
