package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// The script every case here runs, unless it declares its own. It reshapes a
// state change into a body of the operator's own choosing and computes the
// routing key from the event, which is the whole point of the tier.
const transformScript = `msg("audit", "printer." + event.device_id + "." + lower(event.state), ` +
	`toJSON({"machine": event.device_id, "status": event.state, "fact": event.kind}))`

// TestAScriptTransformsAnEventOnItsWayToItsDestination is the script tier end
// to end on the real composition root: the operator's file, the real registry,
// the real supervision plane, the real hub, the real authorization graph and a
// printer dialling in over the frozen wire.
//
// Nothing in this test names the runtime. An operator writes an expression and
// events arrive at the destination shaped by it.
func TestAScriptTransformsAnEventOnItsWayToItsDestination(t *testing.T) {
	t.Parallel()

	endpoint := newScriptEndpoint(t)
	running := start(t, t.TempDir(), scriptConfig(endpoint.URL(), transformScript))

	requireSinkReady(t, running)
	dialIn(t, running, "SIM-SCRIPT-1")

	printer := onlyPrinter(t, running)
	grantSink(t, running, printer.ID)

	delivered := requireTransformed(t, endpoint, printer.ID)

	require.NotEmpty(t, delivered.body["status"], "the fact arrived with no state in it")
	require.Equal(t, "printer.state_changed", delivered.body["fact"])
	require.Equal(t, "printer."+printer.ID+".", delivered.key[:len("printer."+printer.ID+".")],
		"the routing key the script computed did not arrive")
}

// TestABrokenScriptFailsItsOwnInstanceAndNotTheProcess is ADR-20260826-w3y rule 5. A
// script the host cannot compile is the instance's failed start result carrying
// the runtime's own message, so an operator reads the line, the column and the
// token rather than "the sink did not start".
//
// The printer half of the case is the other assertion: the process serves, and
// a broken expression in one instance's block takes nothing else with it.
func TestABrokenScriptFailsItsOwnInstanceAndNotTheProcess(t *testing.T) {
	t.Parallel()

	endpoint := newScriptEndpoint(t)
	broken := `msg("audit", event.jbo, "body")`
	running := start(t, t.TempDir(), scriptConfig(endpoint.URL(), broken))

	var status httpapi.ResourceStatus

	require.Eventually(t, func() bool {
		status = instanceStatus(t, running, "sink/"+sinkName)

		return status.Result == "start-error"
	}, settle, 20*time.Millisecond, "a script that cannot compile started an instance anyway")

	require.False(t, status.Serving, "an instance whose script will not compile is serving")

	// The runtime's own message, unchanged: the token, the line and column, and
	// the caret under the token. Rewriting it loses the only part an author can
	// act on.
	reported := conditionMessages(status)
	require.Contains(t, reported, "no field jbo", "the operator is not told which token is wrong")
	require.Contains(t, reported, "(1:20)", "the operator is not told where in the script it is")
	require.Contains(t, reported, "^", "the caret under the token did not survive")

	dialIn(t, running, "SIM-SCRIPT-2")
	require.Len(t, printers(t, running), 1, "a broken script stopped the rest of the process")
}

// TestAScriptThatReturnsNothingDropsTheEvent is the drop, which is one of the
// four shapes a script may return. The printer is proved to be publishing by
// the same process's own API, so the silence at the endpoint is the script's
// decision and not an absent printer.
func TestAScriptThatReturnsNothingDropsTheEvent(t *testing.T) {
	t.Parallel()

	endpoint := newScriptEndpoint(t)
	dropping := `event.state == "NEVER" ? msg("audit", "k", "b") : nil`
	running := start(t, t.TempDir(), scriptConfig(endpoint.URL(), dropping))

	requireSinkReady(t, running)
	dialIn(t, running, "SIM-SCRIPT-3")

	printer := onlyPrinter(t, running)
	grantSink(t, running, printer.ID)

	require.Contains(t, printer.State, statePath, "the printer never reported a state to drop")

	// Ten times the coalescing window and ten times the sink's re-authorization
	// interval, so silence here is a decision rather than a wait.
	time.Sleep(2 * time.Second)

	require.Empty(t, endpoint.posted(), "a script that returned nothing published something")
}

// TestAScriptNamingAnEndpointNobodyDeclaredPublishesNothing is ADR-20260826-w3y rule 8
// from the other side. The operator names the addresses and the script names
// one of them, so a script that invents a name reaches nothing rather than
// reaching wherever the name happens to resolve.
func TestAScriptNamingAnEndpointNobodyDeclaredPublishesNothing(t *testing.T) {
	t.Parallel()

	endpoint := newScriptEndpoint(t)
	elsewhere := `msg("somewhere-else", "k", "b")`
	running := start(t, t.TempDir(), scriptConfig(endpoint.URL(), elsewhere))

	requireSinkReady(t, running)
	dialIn(t, running, "SIM-SCRIPT-4")

	printer := onlyPrinter(t, running)
	grantSink(t, running, printer.ID)

	time.Sleep(2 * time.Second)

	require.Empty(t, endpoint.posted(), "a script reached an endpoint the operator never declared")
}

// instanceStatus reads one instance's whole status through the API.
func instanceStatus(t *testing.T, running *process, path string) httpapi.ResourceStatus {
	t.Helper()

	read := running.get(t, "/api/v1/resources/"+path)
	require.Equal(t, http.StatusOK, read.status, read.body)

	var instance httpapi.ResourceInstance
	require.NoError(t, json.Unmarshal([]byte(read.body), &instance))

	return instance.Status
}

// conditionMessages joins what the instance reported, so a case can assert on
// the text an operator reads without depending on which condition carries it.
func conditionMessages(status httpapi.ResourceStatus) string {
	reported := make([]string, 0, len(status.Conditions))
	for _, held := range status.Conditions {
		reported = append(reported, held.Message)
	}

	return strings.Join(reported, "\n")
}

// scriptConfig declares one script sink pointed at the endpoint. The script is
// indented as a YAML block scalar, which is how an operator writes more than
// one line of it.
func scriptConfig(url, source string) string {
	return lines(
		"core:", "  log:", "    level: error",
		"resources:", "  sink:", "    "+sinkName+":",
		"      type: script",
		"      runtime: script",
		"      enabled: true",
		"      script: |",
		"        "+source,
		"      config:",
		"        endpoints:",
		"          audit: "+url,
		"        timeout: 2s")
}

// requireTransformed waits for the printer to reach the endpoint through the
// script and returns the first delivery that did.
func requireTransformed(t *testing.T, endpoint *scriptEndpoint, printer string) delivery {
	t.Helper()

	var found delivery

	require.Eventually(t, func() bool {
		for _, got := range endpoint.posted() {
			if got.body["machine"] == printer {
				found = got

				return true
			}
		}

		return false
	}, settle, 50*time.Millisecond, "no transformed event reached the destination")

	return found
}

// delivery is one message as the destination received it: the body the script
// built and the routing key it computed.
type delivery struct {
	key  string
	body map[string]string
}

// scriptEndpoint is a listener standing in for whatever an operator points a
// script sink's endpoint at. It records the raw body rather than a decoded
// contract shape, because the body is the script author's and this side has no
// contract with it.
type scriptEndpoint struct {
	server *httptest.Server

	mu       sync.Mutex
	received []delivery
}

func newScriptEndpoint(t *testing.T) *scriptEndpoint {
	t.Helper()

	endpoint := &scriptEndpoint{}

	endpoint.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		got := delivery{key: r.Header.Get("X-Prusa-Routing-Key")}
		if err = json.Unmarshal(body, &got.body); err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		endpoint.mu.Lock()
		endpoint.received = append(endpoint.received, got)
		endpoint.mu.Unlock()

		w.WriteHeader(http.StatusNoContent)
	}))

	t.Cleanup(endpoint.server.Close)

	return endpoint
}

func (e *scriptEndpoint) URL() string { return e.server.URL + "/publish" }

func (e *scriptEndpoint) posted() []delivery {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]delivery(nil), e.received...)
}
