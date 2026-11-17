package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/bridge"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalnode "github.com/prusa3d/prusa-connect/sdk/fractal/node"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
)

// TestTheOpenAPIFragmentIsOneTheHostPublishes parses the document this module
// ships. The host refuses a fragment it cannot publish at the launch of the
// module, so a mistake in it stops the link rather than being found by a
// client.
func TestTheOpenAPIFragmentIsOneTheHostPublishes(t *testing.T) {
	t.Parallel()

	fragment, err := routesapi.ParseFragment(openAPI)
	if err != nil {
		t.Fatalf("parse openapi.json: %v", err)
	}

	want := []string{
		"/files/shares",
		"/files/shares/{storage}/{path}",
		"/files/stores",
		"/files/stores/{node}/{instance}/{identity}",
		"/nodes",
		"/nodes/{path}",
		"/nodes/{path}/interfaces/{interface}/{instance}/methods/{method}",
	}

	got := fragment.Paths()
	if len(got) != len(want) {
		t.Fatalf("the fragment describes %v and this module serves %v", got, want)
	}

	for index, path := range want {
		if got[index] != path {
			t.Errorf("path %d of the fragment is %q and this module serves %q", index, got[index], path)
		}
	}
}

// TestTheRoutesAnswerBeforeTheSettingsArrive checks every route of a module
// whose settings have not arrived. An operator reads one code and the reason,
// instead of a page that answers nothing.
func TestTheRoutesAnswerBeforeTheSettingsArrive(t *testing.T) {
	t.Parallel()

	handler := routes(&tree{})

	for _, row := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "the list of nodes", method: http.MethodGet, path: "/nodes"},
		{name: "the interfaces of one node", method: http.MethodGet, path: "/nodes/hq/brno"},
		{
			name:   "a method call",
			method: http.MethodPost,
			path:   "/nodes/hq/brno/interfaces/PrintJob1/DEMO_01/methods/pause",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			recorded := ask(t, handler, row.method, row.path, "")

			if recorded.Code != statusUnavailable {
				t.Fatalf("the status is %d and this route answers %d", recorded.Code, statusUnavailable)
			}

			if code := refusalCode(t, recorded.Body.Bytes()); code != TransportUnavailable.Code().String() {
				t.Errorf("the code is %q and this route answers %q",
					code, TransportUnavailable.Code().String())
			}
		})
	}
}

// TestTheNodeListNamesEveryNodeBelow reads the copy of the state below through
// GET /nodes. A node whose interfaces are stale is reported as not connected,
// which is what a person at the top server reads when a server in the middle
// stopped.
func TestTheNodeListNamesEveryNodeBelow(t *testing.T) {
	t.Parallel()

	state := &tree{}
	state.serving(shadowOf(t), stubCaller{}, pathOf("hq"))

	recorded := ask(t, routes(state), http.MethodGet, "/nodes", "")
	if recorded.Code != http.StatusOK {
		t.Fatalf("the status is %d and this route answers 200: %s", recorded.Code, recorded.Body)
	}

	var listing nodeListing
	if err := json.Unmarshal(recorded.Body.Bytes(), &listing); err != nil {
		t.Fatalf("read the answer: %v", err)
	}

	if listing.Node != "hq" {
		t.Errorf("the answer names %q as this server and it is hq", listing.Node)
	}

	if listing.Total != len(listing.Items) {
		t.Errorf("the answer counts %d entries and lists %d", listing.Total, len(listing.Items))
	}

	held := map[fractal.NodePath]bridge.ShadowNode{}
	for _, one := range listing.Items {
		held[one.Path] = one
	}

	if one, ok := held["hq/brno/workshop"]; !ok || !one.Connected {
		t.Errorf("workshop is %+v and its interfaces are current", one)
	}

	if one, ok := held["hq/garage"]; !ok || one.Connected {
		t.Errorf("garage is %+v and its interfaces are stale", one)
	}
}

// TestTheNodeListNamesThePathOfLater: a server that joins, or is moved, after
// the routes were built answers the Node Path it holds now.
func TestTheNodeListNamesThePathOfLater(t *testing.T) {
	t.Parallel()

	var held atomic.Value
	held.Store(fractal.NodePath(""))

	state := &tree{}
	state.serving(shadowOf(t), stubCaller{}, func() fractal.NodePath {
		path, _ := held.Load().(fractal.NodePath)

		return path
	})

	var named []fractal.NodePath

	for _, now := range []fractal.NodePath{"hq/brno", "hq/praha"} {
		held.Store(now)

		var listing nodeListing
		recorded := ask(t, routes(state), http.MethodGet, "/nodes", "")
		if err := json.Unmarshal(recorded.Body.Bytes(), &listing); err != nil {
			t.Fatalf("read the answer: %v", err)
		}

		named = append(named, listing.Node)
	}

	if want := []fractal.NodePath{"hq/brno", "hq/praha"}; !slices.Equal(named, want) {
		t.Errorf("the answers named %v, want %v", named, want)
	}
}

// TestTheInterfaceListCarriesTheValues reads one node below through
// GET /nodes/{path...}. A person at the top server reads the state and the
// nozzle temperature of a printer three servers down from this answer.
func TestTheInterfaceListCarriesTheValues(t *testing.T) {
	t.Parallel()

	state := &tree{}
	state.serving(shadowOf(t), stubCaller{}, pathOf("hq"))

	recorded := ask(t, routes(state), http.MethodGet, "/nodes/hq/brno/workshop", "")
	if recorded.Code != http.StatusOK {
		t.Fatalf("the status is %d and this route answers 200: %s", recorded.Code, recorded.Body)
	}

	var listing interfaceListing
	if err := json.Unmarshal(recorded.Body.Bytes(), &listing); err != nil {
		t.Fatalf("read the answer: %v", err)
	}

	if listing.Node != "hq/brno/workshop" {
		t.Errorf("the answer names %q and the request named hq/brno/workshop", listing.Node)
	}

	if len(listing.Items) != 1 {
		t.Fatalf("the answer lists %d interfaces and the copy holds one for that node", len(listing.Items))
	}

	one := listing.Items[0]
	if one.Status != "OK" {
		t.Errorf("the status is %q and the copy holds the values the node reported", one.Status)
	}

	if string(one.Props["nozzle_temp"]) != "215" {
		t.Errorf("the nozzle temperature is %q and the node reported 215", one.Props["nozzle_temp"])
	}
}

// TestTheInterfaceListKeepsToItsPageSize asks for one interface of a node that
// has more. A module route answers at most a mebibyte, so a listing that fills
// says it left entries out rather than growing.
func TestTheInterfaceListKeepsToItsPageSize(t *testing.T) {
	t.Parallel()

	state := &tree{}
	state.serving(shadowOf(t), stubCaller{}, pathOf("hq"))

	recorded := ask(t, routes(state), http.MethodGet, "/nodes/hq", "limit=1")
	if recorded.Code != http.StatusOK {
		t.Fatalf("the status is %d and this route answers 200: %s", recorded.Code, recorded.Body)
	}

	var listing interfaceListing
	if err := json.Unmarshal(recorded.Body.Bytes(), &listing); err != nil {
		t.Fatalf("read the answer: %v", err)
	}

	if len(listing.Items) != 1 || !listing.Truncated {
		t.Fatalf("the answer lists %d interfaces and says truncated is %v",
			len(listing.Items), listing.Truncated)
	}
}

// TestTheRoutesRefuseAnAddressTheyCannotRead checks the refusals of both
// routes. Each one carries the code a page keys its own sentence by.
func TestTheRoutesRefuseAnAddressTheyCannotRead(t *testing.T) {
	t.Parallel()

	state := &tree{}
	state.serving(shadowOf(t), stubCaller{}, pathOf("hq"))

	handler := routes(state)

	for _, row := range []struct {
		name   string
		method string
		path   string
		query  string
		body   string
		want   string
	}{
		{
			name: "a node path with a space", method: http.MethodGet, path: "/nodes/hq/not+a+path",
			want: RouteNodeInvalid.Code().String(),
		},
		{
			name: "a page size that is not a number", method: http.MethodGet, path: "/nodes/hq",
			query: "limit=many", want: RouteLimitInvalid.Code().String(),
		},
		{
			name: "an address that names no method", method: http.MethodPost,
			path: "/nodes/hq/brno/interfaces/PrintJob1/DEMO_01",
			want: RouteCallInvalid.Code().String(),
		},
		{
			name: "parameters that are not a JSON array", method: http.MethodPost,
			path: "/nodes/hq/brno/interfaces/PrintJob1/DEMO_01/methods/pause",
			body: `{"speed":1}`, want: RouteParamsInvalid.Code().String(),
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			recorded := ask(t, handler, row.method, row.path, row.query, row.body)

			if recorded.Code != statusBadRequest {
				t.Fatalf("the status is %d and this route answers %d: %s",
					recorded.Code, statusBadRequest, recorded.Body)
			}

			if code := refusalCode(t, recorded.Body.Bytes()); code != row.want {
				t.Errorf("the code is %q and this route answers %q", code, row.want)
			}
		})
	}
}

// ask runs one request against the handler and returns what it wrote.
func ask(t *testing.T, handler http.Handler, method, path, query string, body ...string) *httptest.ResponseRecorder {
	t.Helper()

	address := path
	if query != "" {
		address += "?" + query
	}

	sent := ""
	if len(body) > 0 {
		sent = body[0]
	}

	request := httptest.NewRequest(method, address, strings.NewReader(sent))
	recorded := httptest.NewRecorder()

	handler.ServeHTTP(recorded, request)

	return recorded
}

// refusalCode reads the code out of one RFC 9457 body.
func refusalCode(t *testing.T, body []byte) string {
	t.Helper()

	var held struct {
		Code string `json:"code"`
	}

	if err := json.Unmarshal(body, &held); err != nil {
		t.Fatalf("read the refusal: %v", err)
	}

	return held.Code
}

// shadowOf builds a copy of the state below with three nodes: a printer of
// workshop with values, one more interface of brno, and a node whose
// interfaces are stale.
func shadowOf(t *testing.T) *bridge.ShadowView {
	t.Helper()

	store := newShadow(t)

	report(t, store, "hq/brno/workshop", "Printer1", "DEMO_01", `{"nozzle_temp":215}`)
	report(t, store, "hq/brno", "Printer1", "DEMO_02", `{"nozzle_temp":30}`)
	report(t, store, "hq/garage", "Device1", "DEMO_03", `{"nozzle_temp":25}`)

	store.MarkStale("hq/garage")

	return bridge.NewShadowView(store)
}

// newShadow returns an empty copy of the state below that keeps every
// interface.
func newShadow(t *testing.T) *shadow.Store {
	t.Helper()

	store, err := shadow.New(shadow.Config{Selection: shadow.Selection{Patterns: []fractal.Pattern{"*"}}})
	if err != nil {
		t.Fatalf("build the copy of the state below: %v", err)
	}

	return store
}

// report writes one state report into the copy, the way the Fractal node does
// when a report arrives from a node below.
func report(t *testing.T, store *shadow.Store, path, name, instance, props string) {
	t.Helper()

	res := store.ApplyREP(fractal.REP{
		Version:   fractal.Version,
		Sender:    fractal.NodePath(path),
		Interface: fractal.InterfaceName(name),
		Instance:  fractal.InstanceID(instance),
		Props:     propsOf(t, props),
		Timestamp: 1,
	})
	if res.Outcome != shadow.OutcomeApplied {
		t.Fatalf("a state report for %s ended as %v", path, res.Outcome)
	}
}

// propsOf reads one JSON object as a set of property values.
func propsOf(t *testing.T, text string) fractal.Props {
	t.Helper()

	var held fractal.Props
	if err := json.Unmarshal([]byte(text), &held); err != nil {
		t.Fatalf("read the property values: %v", err)
	}

	return held
}

// stubCaller stands in for the Fractal node in a test that never reaches the
// network. It answers every call with the result it was built with. With
// silent it answers nothing at all, the way a node below that stopped does.
type stubCaller struct {
	result json.RawMessage
	failed *fractal.Error
	err    error
	silent bool
}

// Call records nothing and answers what the stub was built with.
func (s stubCaller) Call(ctx context.Context, _ fractalnode.CallRequest) (fractal.RES, error) {
	if s.silent {
		<-ctx.Done()

		return fractal.RES{}, ctx.Err()
	}

	if s.err != nil {
		return fractal.RES{}, s.err
	}

	return fractal.RES{Result: s.result, Error: s.failed}, nil
}

// TestAMethodCallCarriesTheAnswerOfTheNodeBelow sends one command down and
// reads the answer. A refused command carries the code of the Fractal
// specification, which is what a person at the top server reads when the
// server below has no grant for the printer.
func TestAMethodCallCarriesTheAnswerOfTheNodeBelow(t *testing.T) {
	t.Parallel()

	address := "/nodes/hq/brno/workshop/interfaces/PrintJob1/DEMO_01/methods/pause"

	t.Run("the command ran", func(t *testing.T) {
		t.Parallel()

		state := &tree{}
		state.serving(shadowOf(t), stubCaller{result: json.RawMessage(`{"paused":true}`)}, pathOf("hq"))

		recorded := ask(t, routes(state), http.MethodPost, address, "", `[]`)
		if recorded.Code != http.StatusOK {
			t.Fatalf("the status is %d and this route answers 200: %s", recorded.Code, recorded.Body)
		}

		var answered callAnswer
		if err := json.Unmarshal(recorded.Body.Bytes(), &answered); err != nil {
			t.Fatalf("read the answer: %v", err)
		}

		if answered.Node != "hq/brno/workshop" {
			t.Errorf("the answer names %q and the call went to hq/brno/workshop", answered.Node)
		}

		if string(answered.Result) != `{"paused":true}` {
			t.Errorf("the result is %s and the node below returned {\"paused\":true}", answered.Result)
		}
	})

	t.Run("the node below refused the command", func(t *testing.T) {
		t.Parallel()

		refused := &fractal.Error{Code: bridge.CodeCallNotAllowed, Message: "this server refused the call"}

		state := &tree{}
		state.serving(shadowOf(t), stubCaller{failed: refused}, pathOf("hq"))

		recorded := ask(t, routes(state), http.MethodPost, address, "", `[]`)
		if recorded.Code != http.StatusOK {
			t.Fatalf("the status is %d and this route answers 200: %s", recorded.Code, recorded.Body)
		}

		var answered callAnswer
		if err := json.Unmarshal(recorded.Body.Bytes(), &answered); err != nil {
			t.Fatalf("read the answer: %v", err)
		}

		if answered.Error == nil || answered.Error.Code != bridge.CodeCallNotAllowed {
			t.Fatalf("the answer carries %+v and the node below refused with %d",
				answered.Error, bridge.CodeCallNotAllowed)
		}
	})

	t.Run("the call did not leave this server", func(t *testing.T) {
		t.Parallel()

		state := &tree{}
		state.serving(shadowOf(t), stubCaller{err: fractalnode.ErrCallExpired}, pathOf("hq"))

		recorded := ask(t, routes(state), http.MethodPost, address, "", `[]`)
		if recorded.Code != statusUnavailable {
			t.Fatalf("the status is %d and this route answers %d", recorded.Code, statusUnavailable)
		}

		if code := refusalCode(t, recorded.Body.Bytes()); code != CallFailed.Code().String() {
			t.Errorf("the code is %q and this route answers %q", code, CallFailed.Code().String())
		}
	})
}

// TestACallToANodeThatNeverAnswersEndsHere: the host waits a fixed time for
// one call and counts a call it gave up on against this module. A few of those
// stop the host calling this module at all, so the route answers its own error
// before the host gives up.
func TestACallToANodeThatNeverAnswersEndsHere(t *testing.T) {
	t.Parallel()

	if callBudget >= hostCallBudget {
		t.Fatalf("this route waits %s and the host waits %s", callBudget, hostCallBudget)
	}

	state := &tree{}
	state.serving(shadowOf(t), stubCaller{silent: true}, pathOf("hq"))

	address := "/nodes/hq/brno/workshop/interfaces/Printer1/DEMO_01/methods/Beep"

	started := time.Now()
	recorded := ask(t, routes(state), http.MethodPost, address, "", `[]`)
	took := time.Since(started)

	if recorded.Code != statusUnavailable {
		t.Fatalf("the status is %d and this route answers %d", recorded.Code, statusUnavailable)
	}

	if code := refusalCode(t, recorded.Body.Bytes()); code != CallFailed.Code().String() {
		t.Errorf("the code is %q and this route answers %q", code, CallFailed.Code().String())
	}

	if took >= hostCallBudget {
		t.Errorf("the answer took %s and the host gives up after %s", took, hostCallBudget)
	}
}

// hostCallBudget is how long the host waits for the answer to one call before
// it counts the call as a failure of this module. It repeats
// supervisor.DefaultCallTimeout, which this module cannot import.
const hostCallBudget = 10 * time.Second

// TestAResultLargerThanOneAnswerIsRefused: a method may return more than a
// module route may answer. The route says so, with the size, rather than
// writing a body the host refuses.
func TestAResultLargerThanOneAnswerIsRefused(t *testing.T) {
	t.Parallel()

	huge, err := json.Marshal(strings.Repeat("x", listBudget+1))
	if err != nil {
		t.Fatalf("build a large result: %v", err)
	}

	state := &tree{}
	state.serving(shadowOf(t), stubCaller{result: huge}, pathOf("hq"))

	address := "/nodes/hq/brno/workshop/interfaces/Printer1/DEMO_01/methods/Beep"

	recorded := ask(t, routes(state), http.MethodPost, address, "", `[]`)
	if recorded.Code != statusInternal {
		t.Fatalf("the status is %d and this route answers %d: %s",
			recorded.Code, statusInternal, recorded.Body)
	}

	if code := refusalCode(t, recorded.Body.Bytes()); code != ResultTooLarge.Code().String() {
		t.Errorf("the code is %q and this route answers %q", code, ResultTooLarge.Code().String())
	}
}

// TestTheListsAreReadPageByPage: both listings answer one page and name the
// cursor of the page that follows, so a caller reads a tree larger than one
// answer.
func TestTheListsAreReadPageByPage(t *testing.T) {
	t.Parallel()

	state := &tree{}
	state.serving(shadowOf(t), stubCaller{}, pathOf("hq"))

	handler := routes(state)

	t.Run("the nodes", func(t *testing.T) {
		t.Parallel()

		first := nodesOf(t, handler, "limit=1")
		if len(first.Items) != 1 || !first.Truncated || first.Next == "" {
			t.Fatalf("the first page is %+v, and it holds one node of three", first)
		}

		second := nodesOf(t, handler, "limit=10&after="+url.QueryEscape(first.Next))
		if len(second.Items) == 0 {
			t.Fatal("the page after the first is empty")
		}

		for _, one := range second.Items {
			if string(one.Path) == first.Next {
				t.Errorf("%s is on both pages", one.Path)
			}
		}
	})

	t.Run("the interfaces", func(t *testing.T) {
		t.Parallel()

		first := interfacesOf(t, handler, "/nodes/hq", "limit=1")
		if len(first.Items) != 1 || !first.Truncated || first.Next == "" {
			t.Fatalf("the first page is %+v, and it holds one interface of three", first)
		}

		second := interfacesOf(t, handler, "/nodes/hq", "limit=10&after="+url.QueryEscape(first.Next))
		if len(second.Items) != 2 || second.Truncated {
			t.Fatalf("the page after the first is %+v, and it holds the other two", second)
		}
	})
}

// TestAnInterfaceLargerThanOneAnswerLosesItsValues: one interface whose
// property values are larger than an answer may be is listed without them and
// marked, so the page is never empty because of it.
func TestAnInterfaceLargerThanOneAnswerLosesItsValues(t *testing.T) {
	t.Parallel()

	store := newShadow(t)
	report(t, store, "hq/brno/workshop", "Printer1", "DEMO_01",
		`{"nozzle_temp":`+strconv.Quote(strings.Repeat("x", propsBudget+1))+`}`)

	state := &tree{}
	state.serving(bridge.NewShadowView(store), stubCaller{}, pathOf("hq"))

	listing := interfacesOf(t, routes(state), "/nodes/hq/brno/workshop", "")

	if len(listing.Items) != 1 {
		t.Fatalf("the answer lists %d interfaces and the copy holds one", len(listing.Items))
	}

	one := listing.Items[0]
	if !one.PropsOmitted || one.Props != nil {
		t.Fatalf("the interface is %+v, and its values are larger than one answer", one)
	}
}

// nodesOf reads GET /nodes with one query.
func nodesOf(t *testing.T, handler http.Handler, query string) nodeListing {
	t.Helper()

	recorded := ask(t, handler, http.MethodGet, "/nodes", query)
	if recorded.Code != http.StatusOK {
		t.Fatalf("the status is %d and this route answers 200: %s", recorded.Code, recorded.Body)
	}

	var listing nodeListing
	if err := json.Unmarshal(recorded.Body.Bytes(), &listing); err != nil {
		t.Fatalf("read the answer: %v", err)
	}

	return listing
}

// interfacesOf reads GET /nodes/{path...} with one query.
func interfacesOf(t *testing.T, handler http.Handler, path, query string) interfaceListing {
	t.Helper()

	recorded := ask(t, handler, http.MethodGet, path, query)
	if recorded.Code != http.StatusOK {
		t.Fatalf("the status is %d and this route answers 200: %s", recorded.Code, recorded.Body)
	}

	var listing interfaceListing
	if err := json.Unmarshal(recorded.Body.Bytes(), &listing); err != nil {
		t.Fatalf("read the answer: %v", err)
	}

	return listing
}

// pathOf is a Node Path that does not change.
func pathOf(path fractal.NodePath) func() fractal.NodePath {
	return func() fractal.NodePath { return path }
}
