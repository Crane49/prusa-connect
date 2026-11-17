package busapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// TestTheWireCarriesNoEventTypeAndNoOrigin is the contract's whole containment,
// asserted against the bytes rather than against prose.
//
// A module that could put a type or an origin on this wire could shadow a core
// type, publish under another module's namespace, or send an event nothing can
// route. There is no field for either, so there is nothing to check at the far
// end.
func TestTheWireCarriesNoEventTypeAndNoOrigin(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(busapi.EmitParams{
		Endpoint: "changed",
		Key:      "spool/petg",
		Data:     json.RawMessage(`{"grams":840}`),
	})
	if err != nil {
		t.Fatalf("marshal the params: %v", err)
	}

	var members map[string]json.RawMessage
	if err = json.Unmarshal(encoded, &members); err != nil {
		t.Fatalf("read the params back: %v", err)
	}

	for _, forbidden := range []string{"type", "origin", "source", "subject", "namespace", "class"} {
		if _, carried := members[forbidden]; carried {
			t.Errorf("the emit params carry %q, which is the host's to decide", forbidden)
		}
	}

	for _, expected := range []string{"endpoint", "key", "data"} {
		if _, carried := members[expected]; !carried {
			t.Errorf("the emit params do not carry %q", expected)
		}
	}
}

// TestTheBoundsAreCheckedBeforeARoundTripIsSpent holds every limit this
// contract publishes, on the side that can answer without a wire.
//
// The host checks them again, because a limit checked only in a client is a
// limit a peer that is not this client never made. This is the half an author
// meets first.
func TestTheBoundsAreCheckedBeforeARoundTripIsSpent(t *testing.T) {
	t.Parallel()

	for name, params := range map[string]struct {
		emission busapi.EmitParams
		want     error
	}{
		"no endpoint": {
			emission: busapi.EmitParams{Data: json.RawMessage(`{}`)},
			want:     busapi.ErrNoEndpoint,
		},
		"an endpoint over the length": {
			emission: busapi.EmitParams{Endpoint: strings.Repeat("e", busapi.MaxEndpointBytes+1)},
			want:     busapi.ErrNoEndpoint,
		},
		"an endpoint that would forge a log line": {
			emission: busapi.EmitParams{Endpoint: "changed\nlevel=INFO"},
			want:     busapi.ErrNoEndpoint,
		},
		"a key over the length": {
			emission: busapi.EmitParams{Endpoint: "changed", Key: strings.Repeat("k", busapi.MaxKeyBytes+1)},
			want:     busapi.ErrDataTooLarge,
		},
		"a key that would forge a log line": {
			emission: busapi.EmitParams{Endpoint: "changed", Key: "spool\npetg"},
			want:     busapi.ErrInvalidData,
		},
		"a payload over the bound": {
			emission: busapi.EmitParams{
				Endpoint: "changed",
				Data:     json.RawMessage(`"` + strings.Repeat("a", busapi.MaxDataBytes) + `"`),
			},
			want: busapi.ErrDataTooLarge,
		},
		"a payload that is not json": {
			emission: busapi.EmitParams{Endpoint: "changed", Data: json.RawMessage(`{grams:840`)},
			want:     busapi.ErrInvalidData,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := params.emission.Validate(); !errors.Is(err, params.want) {
				t.Errorf("Validate() = %v, want %v", err, params.want)
			}
		})
	}

	// And the shape an author writes is accepted, so the cases above are a
	// grammar and not a closed door.
	ok := busapi.EmitParams{Endpoint: "changed", Key: "spool/petg", Data: json.RawMessage(`{"grams":840}`)}
	if err := ok.Validate(); err != nil {
		t.Errorf("Validate() refused an ordinary emission: %v", err)
	}

	// An emission with no payload at all is legal. The host publishes it as an
	// empty document, because the bus carries a data member.
	if err := (busapi.EmitParams{Endpoint: "changed"}).Validate(); err != nil {
		t.Errorf("Validate() refused an emission with no payload: %v", err)
	}
}

// TestARefusalCrossesAsThisPackagesOwnSentinel is what an author branches on.
//
// A code with no sentinel would leave a module parsing the host's sentence, and
// a sentinel with no sentence would leave it unable to read the limit. Both
// halves cross, which is the shape sdk/stateapi already has.
func TestARefusalCrossesAsThisPackagesOwnSentinel(t *testing.T) {
	t.Parallel()

	for _, refusal := range []error{
		busapi.ErrNoEndpoint,
		busapi.ErrDataTooLarge,
		busapi.ErrInvalidData,
		busapi.ErrNotReady,
		busapi.ErrBusy,
	} {
		host := busapi.Handlers(refusingPublisher{err: refusal})

		client := busapi.NewClient(direct{methods: host})

		err := client.Emit(t.Context(), "changed", "", map[string]int{"grams": 840})
		if !errors.Is(err, refusal) {
			t.Errorf("Emit() = %v, which does not match %v", err, refusal)
		}

		if !strings.Contains(err.Error(), "the limit is 42") {
			t.Errorf("Emit() = %q, which does not carry the host's own sentence", err)
		}
	}
}

// TestTheHostsOwnFailureDoesNotCross is the other direction. A failure the
// contract numbers no code for names this host's internals, and a module can act
// on none of it.
func TestTheHostsOwnFailureDoesNotCross(t *testing.T) {
	t.Parallel()

	host := busapi.Handlers(refusingPublisher{
		err: errors.New("publish: sqlite: /var/lib/pcl/graph.db is locked"),
	})

	err := busapi.NewClient(direct{methods: host}).Emit(t.Context(), "changed", "", nil)
	if err == nil {
		t.Fatal("Emit() succeeded against a host that failed")
	}

	if strings.Contains(err.Error(), "sqlite") || strings.Contains(err.Error(), "graph.db") {
		t.Errorf("Emit() = %q, which carries the host's own internals", err)
	}

	if !strings.Contains(err.Error(), busapi.HostFailed) {
		t.Errorf("Emit() = %q, want the fixed sentence %q", err, busapi.HostFailed)
	}
}

// TestAnEmissionReachesThePublisherWhole is the round trip: what an author hands
// the client is what the host is asked to publish.
func TestAnEmissionReachesThePublisherWhole(t *testing.T) {
	t.Parallel()

	recorder := &recordingPublisher{}

	client := busapi.NewClient(direct{methods: busapi.Handlers(recorder)})

	if err := client.Emit(t.Context(), "changed", "spool/petg", map[string]any{"grams": 840}); err != nil {
		t.Fatalf("Emit() = %v", err)
	}

	if recorder.endpoint != "changed" || recorder.key != "spool/petg" {
		t.Errorf("the host was asked to publish %q/%q", recorder.endpoint, recorder.key)
	}

	if string(recorder.data) != `{"grams":840}` {
		t.Errorf("the payload arrived as %s", recorder.data)
	}
}

// refusingPublisher answers every emission with one error. The sentence carries
// a number, so a case can tell the host's own words from a sentinel with no
// message.
type refusingPublisher struct{ err error }

func (r refusingPublisher) Emit(context.Context, string, string, json.RawMessage) error {
	return fmt.Errorf("%w: the limit is 42", r.err)
}

// recordingPublisher keeps what it was asked to publish.
type recordingPublisher struct {
	endpoint string
	key      string
	data     json.RawMessage
}

func (p *recordingPublisher) Emit(_ context.Context, endpoint, key string, data json.RawMessage) error {
	p.endpoint, p.key, p.data = endpoint, key, data

	return nil
}

// direct calls a method set without a process on the other end, which is what
// busapi.Caller is stated for.
type direct struct{ methods wirerpc.Methods }

func (d direct) Call(ctx context.Context, method wirerpc.Method, params any) (json.RawMessage, error) {
	handler, served := d.methods[method]
	if !served {
		return nil, &wirerpc.Error{Code: wirerpc.CodeMethodNotFound, Message: string(method)}
	}

	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	answered, err := handler(ctx, wirerpc.Call{Method: method, Params: encoded})
	if err != nil {
		return nil, err
	}

	return json.Marshal(answered)
}
