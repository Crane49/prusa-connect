package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// memoryModuleState is a record store that keeps what it was written, keyed the
// way the real one is: by namespace, by principal and by key. A test that could
// not tell one principal from another would pass for a server that told nobody
// apart.
type memoryModuleState struct {
	mu   sync.Mutex
	held map[string][]byte
}

func newMemoryModuleState() *memoryModuleState {
	return &memoryModuleState{held: map[string][]byte{}}
}

func (s *memoryModuleState) at(namespace, principal, key string) string {
	return namespace + "\x00" + principal + "\x00" + key
}

func (s *memoryModuleState) Keys(_ context.Context, namespace, principal string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prefix := s.at(namespace, principal, "")

	var keys []string

	for at := range s.held {
		if rest, inside := strings.CutPrefix(at, prefix); inside {
			keys = append(keys, rest)
		}
	}

	return keys, nil
}

func (s *memoryModuleState) Value(_ context.Context, namespace, principal, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	value, held := s.held[s.at(namespace, principal, key)]
	if !held {
		return nil, ErrNoStateRecord
	}

	return value, nil
}

func (s *memoryModuleState) Write(
	_ context.Context, namespace, principal, key string, value []byte,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.held[s.at(namespace, principal, key)] = value

	return nil
}

func (s *memoryModuleState) Remove(_ context.Context, namespace, principal, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	at := s.at(namespace, principal, key)
	if _, held := s.held[at]; !held {
		return ErrNoStateRecord
	}

	delete(s.held, at)

	return nil
}

// settingsServer is the real router with a module claiming one namespace, the
// record store behind it, and a caller holding the actions named.
func settingsServer(
	t *testing.T, store ModuleStateResolver, who string, actions ...authz.Action,
) *Server {
	t.Helper()

	instance, err := ref.Parse(moduleInstance)
	require.NoError(t, err)

	access := &fakeModuleAccess{who: who, instance: instance, actions: actions}
	plane := fakeModulePlane{namespace: moduleNamespace, instance: instance, handler: moduleEcho()}

	server, err := New(Config{
		Version:            "test",
		Principals:         fixedPrincipal(who),
		Resources:          seededStore(),
		Presets:            seededPresets(),
		Endpoints:          seededEndpoints(),
		Grants:             &fakeGrantStore{},
		Sessions:           newFakeSessions(),
		Accounts:           &fakeAccounts{info: AuthInfo{Provider: "local", Credentials: true}},
		Readiness:          &fakeReadiness{report: readyReport()},
		Devices:            noPrinters{},
		Stores:             noStorages{},
		Events:             noEvents{},
		Modules:            plane,
		ModuleAccess:       access,
		ModuleState:        store,
		MaxStateValueBytes: 64,
	})
	require.NoError(t, err)

	return server
}

// bothActions is the caller a settings pane runs as: they may read the module
// and they may write its configuration.
func bothActions() []authz.Action {
	return []authz.Action{authz.ActionViewResource, authz.ActionConfigureResource}
}

// askJSON sends one request with the media type a JSON body is decoded under,
// which ask leaves off because most of its cases send none.
func askJSON(t *testing.T, server *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	var request *http.Request
	if reader == nil {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = httptest.NewRequest(method, target, reader)
		request.Header.Set("Content-Type", jsonMedia)
	}

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	return recorder
}

// statePath is the address of one record under the example module.
func statePath(key string) string {
	return ModuleMount(moduleNamespace) + "/state/" + key
}

// TestAModuleRecordSurvivesTheRoundTripThroughTheRouter drives the four
// operations through the real router in the order a settings pane uses them:
// write, read back, list, remove, and read what is no longer there.
func TestAModuleRecordSurvivesTheRoundTripThroughTheRouter(t *testing.T) {
	t.Parallel()

	held := newMemoryModuleState()
	server := settingsServer(t, func() (ModuleStateStore, error) { return held, nil },
		"user:operator", bothActions()...)

	written := askJSON(t, server, http.MethodPut, statePath(exampleStateKey),
		`{"value":`+string(exampleStateValue())+`}`)
	require.Equal(t, http.StatusOK, written.Code, written.Body.String())
	require.JSONEq(t, `{"key":"units","value":{"length":"mm","weight":"g"}}`, written.Body.String())

	read := askJSON(t, server, http.MethodGet, statePath(exampleStateKey), "")
	require.Equal(t, http.StatusOK, read.Code, read.Body.String())
	require.JSONEq(t, written.Body.String(), read.Body.String(),
		"a record read back is not the record written")

	listed := askJSON(t, server, http.MethodGet, ModuleMount(moduleNamespace)+"/state", "")
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.JSONEq(t, `{"items":["units"]}`, listed.Body.String())

	removed := askJSON(t, server, http.MethodDelete, statePath(exampleStateKey), "")
	require.Equal(t, http.StatusNoContent, removed.Code, removed.Body.String())

	gone := askJSON(t, server, http.MethodGet, statePath(exampleStateKey), "")
	require.Equal(t, http.StatusNotFound, gone.Code)
	require.Contains(t, gone.Body.String(), "ROUTE_NOT_FOUND")

	emptied := askJSON(t, server, http.MethodGet, ModuleMount(moduleNamespace)+"/state", "")
	require.JSONEq(t, `{"items":[]}`, emptied.Body.String(),
		"a listing with nothing in it is an empty list and never null")
}

// TestOnePersonsModuleRecordIsNotAnothers is the half of ADR-20260911-4p6 rule 18 that
// a single-caller test cannot show: the namespace is shared and the record is
// not.
func TestOnePersonsModuleRecordIsNotAnothers(t *testing.T) {
	t.Parallel()

	held := newMemoryModuleState()
	resolver := func() (ModuleStateStore, error) { return held, nil }

	alice := settingsServer(t, resolver, "user:alice", bothActions()...)
	bob := settingsServer(t, resolver, "user:bob", bothActions()...)

	require.Equal(t, http.StatusOK,
		askJSON(t, alice, http.MethodPut, statePath("theme"), `{"value":"dark"}`).Code)

	require.Equal(t, http.StatusNotFound,
		askJSON(t, bob, http.MethodGet, statePath("theme"), "").Code,
		"one person read another's record")

	require.JSONEq(t, `{"items":[]}`,
		askJSON(t, bob, http.MethodGet, ModuleMount(moduleNamespace)+"/state", "").Body.String(),
		"one person's listing named another's key")
}

// TestAModuleRecordKeyIsOneLabel is the grammar ADR-20260911-4p6 rule 14 states, held
// on the path this surface reads a key from.
func TestAModuleRecordKeyIsOneLabel(t *testing.T) {
	t.Parallel()

	held := newMemoryModuleState()
	server := settingsServer(t, func() (ModuleStateStore, error) { return held, nil },
		"user:operator", bothActions()...)

	cases := []struct {
		name     string
		key      string
		accepted bool
	}{
		{name: "a plain word", key: "units", accepted: true},
		{name: "digits", key: "v2", accepted: true},
		{name: "an inner hyphen", key: "last-opened-tab", accepted: true},
		{name: "sixty three characters", key: strings.Repeat("a", 63), accepted: true},
		{name: "sixty four characters", key: strings.Repeat("a", 64)},
		{name: "an upper case letter", key: "Units"},
		{name: "a leading hyphen", key: "-units"},
		{name: "a trailing hyphen", key: "units-"},
		{name: "an underscore", key: "last_tab"},
		{name: "a dot", key: "units.metric"},
		{name: "an escaped space", key: "two%20words"},
		{name: "an escaped percent sign", key: "50%25"},
	}

	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			t.Parallel()

			answered := askJSON(t, server, http.MethodPut, statePath(one.key), `{"value":1}`)

			if one.accepted {
				require.Equal(t, http.StatusOK, answered.Code, answered.Body.String())

				return
			}

			require.Equal(t, http.StatusUnprocessableEntity, answered.Code, answered.Body.String())
			require.Contains(t, answered.Body.String(), "REQUEST_INVALID")
			require.Contains(t, answered.Body.String(), "path.key")
		})
	}
}

// TestAModuleRecordOverTheCeilingIsRefusedWithTheLimit holds the bound and the
// sentence a settings pane shows for it. The refusal names the limit, because a
// pane that cannot say how much is too much can only say that saving failed.
func TestAModuleRecordOverTheCeilingIsRefusedWithTheLimit(t *testing.T) {
	t.Parallel()

	held := newMemoryModuleState()
	server := settingsServer(t, func() (ModuleStateStore, error) { return held, nil },
		"user:operator", bothActions()...)

	// The server above is built with a 64-byte ceiling, so the two cases are
	// one character apart across it.
	fits := `"` + strings.Repeat("x", 62) + `"`
	require.Len(t, fits, 64)

	require.Equal(t, http.StatusOK,
		askJSON(t, server, http.MethodPut, statePath("fits"), `{"value":`+fits+`}`).Code)

	over := `"` + strings.Repeat("x", 63) + `"`

	refused := askJSON(t, server, http.MethodPut, statePath("over"), `{"value":`+over+`}`)
	require.Equal(t, http.StatusRequestEntityTooLarge, refused.Code, refused.Body.String())
	require.Contains(t, refused.Body.String(), "STATE_VALUE_TOO_LARGE")
	require.Contains(t, refused.Body.String(), `"limit_bytes"`)

	stored, err := held.Value(t.Context(), moduleNamespace, "user:operator", "over")
	require.ErrorIs(t, err, ErrNoStateRecord, "a refused write reached the store")
	require.Nil(t, stored)
}

// TestAModuleRecordTakesTheSameVerbsAsTheInstance is ADR-20260827-wmv rule 13 on this
// surface: reading takes view_resource and writing takes configure_resource, so
// a person who may look at a module cannot rewrite what it remembers.
func TestAModuleRecordTakesTheSameVerbsAsTheInstance(t *testing.T) {
	t.Parallel()

	held := newMemoryModuleState()
	resolver := func() (ModuleStateStore, error) { return held, nil }

	require.NoError(t, held.Write(t.Context(), moduleNamespace, "user:reader", exampleStateKey,
		[]byte(exampleStateValue())))

	reader := settingsServer(t, resolver, "user:reader", authz.ActionViewResource)

	cases := []struct {
		name   string
		method string
		target string
		body   string
		status int
	}{
		{
			name: "listing", method: http.MethodGet,
			target: ModuleMount(moduleNamespace) + "/state", status: http.StatusOK,
		},
		{
			name: "reading one record", method: http.MethodGet,
			target: statePath(exampleStateKey), status: http.StatusOK,
		},
		{
			name: "writing one record", method: http.MethodPut,
			target: statePath(exampleStateKey), body: `{"value":1}`, status: http.StatusForbidden,
		},
		{
			name: "removing one record", method: http.MethodDelete,
			target: statePath(exampleStateKey), status: http.StatusForbidden,
		},
	}

	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			t.Parallel()

			answered := askJSON(t, reader, one.method, one.target, one.body)
			require.Equal(t, one.status, answered.Code, answered.Body.String())
		})
	}

	writer := settingsServer(t, resolver, "user:writer", authz.ActionConfigureResource)
	require.Equal(t, http.StatusOK,
		askJSON(t, writer, http.MethodPut, statePath("theme"), `{"value":"dark"}`).Code,
		"configure_resource did not carry a write")
}

// TestAModuleRecordUnderAnUnclaimedNamespaceIsUnrouted keeps this surface from
// saying which modules a deployment runs. A namespace no running instance
// claims answers exactly as a path nothing routes.
func TestAModuleRecordUnderAnUnclaimedNamespaceIsUnrouted(t *testing.T) {
	t.Parallel()

	held := newMemoryModuleState()
	server := settingsServer(t, func() (ModuleStateStore, error) { return held, nil },
		"user:operator", bothActions()...)

	other := modulesPathPrefix + "/com.github.aux.acme.other/state"

	cases := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "listing", method: http.MethodGet, target: other},
		{name: "reading", method: http.MethodGet, target: other + "/units"},
		{name: "writing", method: http.MethodPut, target: other + "/units", body: `{"value":1}`},
		{name: "removing", method: http.MethodDelete, target: other + "/units"},
		{
			name: "a namespace outside the grammar", method: http.MethodGet,
			target: modulesPathPrefix + "/NOT-A-NAMESPACE/state",
		},
	}

	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			t.Parallel()

			answered := askJSON(t, server, one.method, one.target, one.body)
			require.Equal(t, http.StatusNotFound, answered.Code, answered.Body.String())
			require.Contains(t, answered.Body.String(), "ROUTE_NOT_FOUND")
		})
	}
}

// TestAServerWithNoRecordBackendSaysSo keeps the address behaving the same
// however the process was wired. The document publishes these four operations
// on every build, and a deployment running no record backend says that rather
// than pretending a write landed.
func TestAServerWithNoRecordBackendSaysSo(t *testing.T) {
	t.Parallel()

	server := settingsServer(t, nil, "user:operator", bothActions()...)

	answered := askJSON(t, server, http.MethodPut, statePath(exampleStateKey), `{"value":1}`)
	require.Equal(t, http.StatusNotImplemented, answered.Code, answered.Body.String())
	require.Contains(t, answered.Body.String(), "STATE_NOT_CONFIGURED")
}

// TestAModuleRecordIsReturnedAsItWasWritten is the promise ADR-20260827-rwq rule 2
// rests on: the core keeps a module's document and reads no member of it.
func TestAModuleRecordIsReturnedAsItWasWritten(t *testing.T) {
	t.Parallel()

	held := newMemoryModuleState()
	server := settingsServer(t, func() (ModuleStateStore, error) { return held, nil },
		"user:operator", bothActions()...)

	documents := []string{
		`{"nested":{"deep":[1,2,3]}}`,
		`[1,"two",null,true]`,
		`"a plain string"`,
		`17`,
		`null`,
	}

	for _, document := range documents {
		t.Run(document, func(t *testing.T) {
			t.Parallel()

			key := "doc" + strings.Map(onlyLabelRunes, document)

			written := askJSON(t, server, http.MethodPut, statePath(key), `{"value":`+document+`}`)
			require.Equal(t, http.StatusOK, written.Code, written.Body.String())

			var record struct {
				Value json.RawMessage `json:"value"`
			}

			require.NoError(t, json.Unmarshal(written.Body.Bytes(), &record))
			require.JSONEq(t, document, string(record.Value))
		})
	}
}

// onlyLabelRunes keeps the characters a key may carry, so a case name can be
// turned into one.
func onlyLabelRunes(r rune) rune {
	if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
		return r
	}

	return -1
}
