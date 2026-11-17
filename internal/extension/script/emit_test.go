package script_test

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/bus"
	"github.com/prusa3d/prusa-connect/internal/extension/script"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// TestAScriptEmitsTheOperatorsTypeAndItsOwnBody checks who chooses what. The
// operator writes the event type beside the endpoint name, the script writes
// the key and the body, and neither side can take the other's part.
func TestAScriptEmitsTheOperatorsTypeAndItsOwnBody(t *testing.T) {
	t.Parallel()

	bus := &busRecorder{}

	sink, err := script.NewSink(
		`msg("bus", "printer." + event.device_id, toJSON({"state": event.state}))`,
		json.RawMessage(`{"endpoints":{"bus":"event:com.example.fleet.observed"}}`), nil, bus)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, sink.Close()) })

	sink.PrinterStateChanged(eventsapi.PrinterState{ID: "prn-42", State: "PRINTING"})

	emitted := bus.emitted()
	require.Len(t, emitted, 1, "the script emitted nothing on the bus the operator declared")
	require.Equal(t, "com.example.fleet.observed", emitted[0].Type,
		"the type on the wire is not the one the operator declared")
	require.Equal(t, "printer.prn-42", emitted[0].Key)
	require.JSONEq(t, `{"state":"PRINTING"}`, emitted[0].Body)
}

// TestOneScriptReachesABusEndpointAndAnHTTPEndpointTogether is why both kinds
// of destination live in one table. A script sends to both by naming two keys,
// and nothing in the expression says which kind each name is.
func TestOneScriptReachesABusEndpointAndAnHTTPEndpointTogether(t *testing.T) {
	t.Parallel()

	bus := &busRecorder{}
	remote := newRecorder(t)

	sink, err := script.NewSink(
		`fan(msg("bus", "k", toJSON({"id": event.device_id})), msg("out", "k", "sent"))`,
		json.RawMessage(`{"endpoints":{"bus":"event:com.example.fleet.observed","out":"`+
			remote.URL()+`"}}`), nil, bus)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, sink.Close()) })

	sink.PrinterStateChanged(eventsapi.PrinterState{ID: "prn-7", State: "PRINTING"})

	require.Len(t, bus.emitted(), 1, "the bus half of the fan-out did not arrive")
	require.Len(t, remote.received(), 1, "the HTTP half of the fan-out did not arrive")
}

// TestAnEventEndpointIsRefusedBeforeAnythingRuns covers what an operator may
// not write beside an endpoint name, and a host that offers no bus. Each case
// fails the instance at start, so nothing runs and logs on every event.
func TestAnEventEndpointIsRefusedBeforeAnythingRuns(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		config string
		bus    bus.Emitter
		says   string
	}{
		"a type in the core namespace": {
			config: `{"endpoints":{"bus":"event:` + event.CoreNamespace + `printer.arrived"}}`,
			bus:    &busRecorder{},
			says:   "closed namespace",
		},
		"a type of one segment": {
			config: `{"endpoints":{"bus":"event:observed"}}`,
			bus:    &busRecorder{},
			says:   "reverse-DNS",
		},
		"no type at all": {
			config: `{"endpoints":{"bus":"event:"}}`,
			bus:    &busRecorder{},
			says:   "empty",
		},
		"a type carrying a query": {
			config: `{"endpoints":{"bus":"event:com.example.fleet.observed?to=elsewhere"}}`,
			bus:    &busRecorder{},
			says:   "a segment is lowercase letters",
		},
		"a host that offers no bus": {
			config: `{"endpoints":{"bus":"event:com.example.fleet.observed"}}`,
			bus:    nil,
			says:   "offers none",
		},
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := script.NewSink(`msg("bus", "k", "{}")`, json.RawMessage(want.config), nil, want.bus)
			require.ErrorIs(t, err, script.ErrInvalidSettings)
			require.Contains(t, err.Error(), want.says,
				"the refusal does not say what the operator has to change")
		})
	}
}

// TestABodyLargerThanTheHostSendsIsNotEmitted covers the body cap on the bus
// side. One check covers both destinations, so a body the host refuses to post
// is also a body it refuses to publish.
func TestABodyLargerThanTheHostSendsIsNotEmitted(t *testing.T) {
	t.Parallel()

	bus := &busRecorder{}

	sink, err := script.NewSink(`msg("bus", "k", event.device_id)`,
		json.RawMessage(`{"endpoints":{"bus":"event:com.example.fleet.observed"}}`), nil, bus)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, sink.Close()) })

	oversize := `"` + strings.Repeat("x", 1<<20) + `"`
	sink.PrinterStateChanged(eventsapi.PrinterState{ID: oversize})

	require.Empty(t, bus.emitted(), "a body over the cap was published")

	sink.PrinterStateChanged(eventsapi.PrinterState{ID: `"small"`})
	require.Len(t, bus.emitted(), 1, "the instance stopped emitting after one oversized body")
}

// TestABusRefusalCostsOneEventAndNotTheInstance covers the containment an
// emitting script needs. The usual refusal is a body that is not JSON, because
// the bus carries it as a data member, and the next event still has to run.
func TestABusRefusalCostsOneEventAndNotTheInstance(t *testing.T) {
	t.Parallel()

	bus := &busRecorder{refuse: errors.New("data is not json")}

	sink, err := script.NewSink(`msg("bus", "k", event.state)`,
		json.RawMessage(`{"endpoints":{"bus":"event:com.example.fleet.observed"}}`), nil, bus)
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, sink.Close()) })

	sink.PrinterStateChanged(eventsapi.PrinterState{ID: "prn-1", State: "not json"})
	sink.PrinterStateChanged(eventsapi.PrinterState{ID: "prn-2", State: `"json"`})

	require.Len(t, bus.emitted(), 2,
		"a refused emission stopped the instance instead of costing one event")
}

// busRecorder stands in for the local event stream. It keeps what it was given
// and can refuse, the way a hub refuses a body that is not JSON.
type busRecorder struct {
	refuse error

	mu   sync.Mutex
	seen []bus.Emission
}

var _ bus.Emitter = (*busRecorder)(nil)

func (b *busRecorder) Emit(emission bus.Emission) error {
	b.mu.Lock()
	b.seen = append(b.seen, emission)
	b.mu.Unlock()

	return b.refuse
}

func (b *busRecorder) emitted() []bus.Emission {
	b.mu.Lock()
	defer b.mu.Unlock()

	return append([]bus.Emission(nil), b.seen...)
}
