package script_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/script"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// TestASinkSettingsBlockIsRefusedBeforeAnythingRuns keeps the destinations to
// what the host can send to. Each case is refused at construction, and the
// supervision plane reports it as a failed start. Nothing runs and drops every
// event.
func TestASinkSettingsBlockIsRefusedBeforeAnythingRuns(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"no config at all":                 ``,
		"no endpoints":                     `{"timeout":"2s"}`,
		"an empty endpoint table":          `{"endpoints":{}}`,
		"an endpoint with no host":         `{"endpoints":{"a":"http://"}}`,
		"an endpoint that is not http":     `{"endpoints":{"a":"file:///etc/passwd"}}`,
		"a key nothing reads":              `{"endpoints":{"a":"http://x/y"},"retries":3}`,
		"a timeout that is not a duration": `{"endpoints":{"a":"http://x/y"},"timeout":"soon"}`,
		"a timeout that removes the bound": `{"endpoints":{"a":"http://x/y"},"timeout":"0s"}`,
	}

	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := script.NewSink(`msg("a", "k", "b")`, json.RawMessage(config), nil, nil)
			require.ErrorIs(t, err, script.ErrInvalidSettings)
		})
	}
}

// TestASinkSendsWhatTheScriptBuilt covers one delivery. The script supplies the
// body and the routing key, and the operator supplies the address.
func TestASinkSendsWhatTheScriptBuilt(t *testing.T) {
	t.Parallel()

	destination := newRecorder(t)

	sink, err := script.NewSink(
		`msg("main", "printer." + event.device_id, toJSON({"state": event.state}))`,
		json.RawMessage(`{"endpoints":{"main":"`+destination.URL()+`"},"timeout":"2s"}`), nil, nil)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, sink.Close()) })

	sink.PrinterStateChanged(eventsapi.PrinterState{ID: "prn-42", State: "PRINTING"})

	got := destination.received()
	require.Len(t, got, 1)
	require.JSONEq(t, `{"state":"PRINTING"}`, got[0].body)
	require.NotContains(t, got[0].body, " ", "the body reached the wire indented")
	require.Equal(t, "printer.prn-42", got[0].key)
}

// TestASinkSendsNothingToAnEndpointNobodyDeclared checks the one boundary a
// script has. A name the operator did not write reaches nothing.
func TestASinkSendsNothingToAnEndpointNobodyDeclared(t *testing.T) {
	t.Parallel()

	destination := newRecorder(t)

	sink, err := script.NewSink(`msg("elsewhere", "k", "b")`,
		json.RawMessage(`{"endpoints":{"main":"`+destination.URL()+`"}}`), nil, nil)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, sink.Close()) })

	sink.PrinterStateChanged(eventsapi.PrinterState{ID: "prn-42", State: "PRINTING"})

	require.Empty(t, destination.received())
}

// TestEveryDomainFactReachesTheScript covers the mapping between the sink
// contract and the environment an author writes against. A fact with no case
// here is a fact a script would never see.
func TestEveryDomainFactReachesTheScript(t *testing.T) {
	t.Parallel()

	destination := newRecorder(t)

	sink, err := script.NewSink(`msg("main", event.kind, event.device_id + "|" + event.label_id)`,
		json.RawMessage(`{"endpoints":{"main":"`+destination.URL()+`"}}`), nil, nil)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, sink.Close()) })

	sink.PrinterStateChanged(eventsapi.PrinterState{ID: "prn-1", State: "PRINTING"})
	sink.PrinterLabelsChanged(eventsapi.PrinterLabels{ID: "prn-2", Mode: "replace"})
	sink.PrinterDeleted("prn-3")
	sink.JobUpdated(eventsapi.JobInfo{ID: "job-1", PrinterID: "prn-4", State: "running"})
	sink.LabelDeleted("label-1")

	keys := make([]string, 0, 5)
	for _, got := range destination.received() {
		keys = append(keys, got.key)
	}

	require.Equal(t, []string{
		"printer.state_changed",
		"printer.labels_changed",
		"printer.deleted",
		"job.updated",
		"label.deleted",
	}, keys)
}

// TestARunTimeFailureCostsOneEventAndNotTheInstance covers the containment a
// sink needs. A script that fails on one event stays compiled and pooled, and
// runs again on the next one. An instance that stopped at the first bad payload
// would send nothing for the life of the process.
func TestARunTimeFailureCostsOneEventAndNotTheInstance(t *testing.T) {
	t.Parallel()

	destination := newRecorder(t)

	// A division by zero yields an infinity, which no JSON encoder writes, so
	// the failure comes from the script and only for the event that causes it.
	source := `event.state == "ERROR" ? msg("main", "k", toJSON({"x": 1 / 0})) : msg("main", "k", "fine")`

	sink, err := script.NewSink(source,
		json.RawMessage(`{"endpoints":{"main":"`+destination.URL()+`"}}`), nil, nil)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, sink.Close()) })

	sink.PrinterStateChanged(eventsapi.PrinterState{ID: "prn-1", State: "ERROR"})
	sink.PrinterStateChanged(eventsapi.PrinterState{ID: "prn-1", State: "PRINTING"})

	got := destination.received()
	require.Len(t, got, 1, "the failing event was published or the good one was lost")
	require.Equal(t, "fine", got[0].body)
}

// message is one delivery as a destination received it.
type message struct {
	key  string
	body string
}

// recorder is a destination that keeps what it was sent.
type recorder struct {
	server *httptest.Server

	mu   sync.Mutex
	seen []message
}

func newRecorder(t *testing.T) *recorder {
	t.Helper()

	kept := &recorder{}

	kept.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		kept.mu.Lock()
		kept.seen = append(kept.seen, message{key: r.Header.Get("X-Prusa-Routing-Key"), body: string(body)})
		kept.mu.Unlock()

		w.WriteHeader(http.StatusNoContent)
	}))

	t.Cleanup(kept.server.Close)

	return kept
}

func (r *recorder) URL() string { return r.server.URL + "/publish" }

func (r *recorder) received() []message {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]message(nil), r.seen...)
}
