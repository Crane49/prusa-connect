package stateapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/stateapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// TestNoWireCallCarriesANamespace is the containment as a property of the
// contract rather than of any implementation of it.
//
// A namespace cannot be smuggled onto this wire because there is no field on
// it to put one in. The case proves that by sending the params a hostile module
// would send: every published params object with a namespace member bolted on.
// The handler reads what it publishes and nothing else, so the extra member is
// not refused, it is not there.
func TestNoWireCallCarriesANamespace(t *testing.T) {
	t.Parallel()

	// Every field name a caller might reach for. If one of them ever appears in
	// a params object this contract publishes, this case starts failing, which
	// is the point of listing them rather than testing one.
	forged := []string{"namespace", "ns", "tenant", "owner", "scope", "prefix_namespace"}

	for _, method := range []wirerpc.Method{
		stateapi.MethodGet,
		stateapi.MethodPut,
		stateapi.MethodCompareAndPut,
		stateapi.MethodDelete,
		stateapi.MethodList,
		stateapi.MethodVersion,
		stateapi.MethodUsage,
	} {
		t.Run(string(method), func(t *testing.T) {
			t.Parallel()

			held := &recording{}
			handler, served := stateapi.Handlers(held)[method]

			if !served {
				t.Fatalf("%s is published and Handlers does not serve it", method)
			}

			params := map[string]any{"key": "spools/petg", "prefix": "spools/", "expected": 0}
			for _, name := range forged {
				params[name] = "com.github.aux.acme.other"
			}

			body, err := json.Marshal(params)
			if err != nil {
				t.Fatalf("rendering the params: %v", err)
			}

			if _, err = handler(t.Context(), wirerpc.Call{Method: method, Params: body}); err != nil {
				t.Fatalf("%s refused the call: %v", method, err)
			}

			if held.calls == 0 {
				t.Fatalf("%s reached no store", method)
			}

			// The store was reached with the key and nothing else. There is no
			// argument on stateapi.Records that could carry a namespace either,
			// which is what makes the store's own binding the only one.
			if held.namespace != "" {
				t.Errorf("%s carried the namespace %q into the store", method, held.namespace)
			}
		})
	}
}

// TestAParamsObjectThatCannotBeReadIsTheCallersFault holds a refusal to the
// code that says who fixes it. A module that sent nothing is told its params
// are invalid, not that the host failed.
func TestAParamsObjectThatCannotBeReadIsTheCallersFault(t *testing.T) {
	t.Parallel()

	handler := stateapi.Handlers(&recording{})[stateapi.MethodGet]

	for name, params := range map[string]json.RawMessage{
		"nothing at all":  nil,
		"not an object":   json.RawMessage(`["spools/petg"]`),
		"a key of a type": json.RawMessage(`{"key": 17}`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := handler(t.Context(), wirerpc.Call{Method: stateapi.MethodGet, Params: params})

			var coded *wirerpc.Error
			if !errors.As(err, &coded) || coded.Code != wirerpc.CodeInvalidParams {
				t.Fatalf("the refusal was %v, and invalid params is the answer", err)
			}
		})
	}
}

// TestARefusalKeepsItsMeaningAcrossTheWire is the round trip that makes
// errors.Is worth writing in a module: the host raises a sentinel, the wire
// carries a number, and the client hands back the same sentinel.
func TestARefusalKeepsItsMeaningAcrossTheWire(t *testing.T) {
	t.Parallel()

	for name, refusal := range map[string]error{
		"no record":      stateapi.ErrNoRecord,
		"wrong version":  stateapi.ErrVersion,
		"bad key":        stateapi.ErrKey,
		"value too big":  stateapi.ErrValueTooLarge,
		"namespace full": stateapi.ErrNamespaceFull,
		"busy":           stateapi.ErrBusy,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := stateapi.NewClient(&loopback{records: &refusing{err: refusal}})

			if _, err := client.Get(t.Context(), "spools/petg"); !errors.Is(err, refusal) {
				t.Errorf("Get answered %v and the host raised %v", err, refusal)
			}

			if _, err := client.Put(t.Context(), "spools/petg", []byte("x")); !errors.Is(err, refusal) {
				t.Errorf("Put answered %v and the host raised %v", err, refusal)
			}
		})
	}
}

// TestAModuleReadsTheLimitItWasGranted is state.usage end to end: what the
// host's store answers is what the module's client reads, so a module can say
// how close it is to the limit the operator granted rather than to the default.
func TestAModuleReadsTheLimitItWasGranted(t *testing.T) {
	t.Parallel()

	client := stateapi.NewClient(&loopback{records: &recording{}})

	usage, err := client.Usage(t.Context())
	if err != nil {
		t.Fatalf("Usage refused: %v", err)
	}

	if usage.Bytes != 42 || usage.Limit != 128*stateapi.NamespaceMiB {
		t.Errorf("Usage read %+v", usage)
	}
}

// TestAClientRefusesABadCallBeforeItSpendsARoundTrip holds the client's own
// bounds. They are the same bounds the host applies, checked here so a module
// learns about a key it cannot use without a process boundary in the way, and
// applied there because a peer that is not this client never made them.
func TestAClientRefusesABadCallBeforeItSpendsARoundTrip(t *testing.T) {
	t.Parallel()

	counted := &loopback{records: &refusing{err: stateapi.ErrNoRecord}}
	client := stateapi.NewClient(counted)

	if _, err := client.Get(t.Context(), ""); !errors.Is(err, stateapi.ErrKey) {
		t.Errorf("an empty key answered %v", err)
	}

	over := stateapi.Key(strings.Repeat("k", stateapi.MaxKeyBytes+1))
	if _, err := client.Put(t.Context(), over, nil); !errors.Is(err, stateapi.ErrKey) {
		t.Errorf("an oversize key answered %v", err)
	}

	big := make([]byte, stateapi.MaxValueBytes+1)
	if _, err := client.Put(t.Context(), "ok", big); !errors.Is(err, stateapi.ErrValueTooLarge) {
		t.Errorf("an oversize value answered %v", err)
	}

	if counted.calls != 0 {
		t.Errorf("%d calls crossed the wire, and each one was refusable here", counted.calls)
	}
}

// recording is a Records that says what it was asked for. It has a namespace
// field so the case above can prove nothing ever writes it.
type recording struct {
	calls     int
	namespace string
	key       stateapi.Key
}

func (r *recording) Get(context.Context, stateapi.Key) (stateapi.Value, error) {
	r.calls++

	return stateapi.Value{}, nil
}

func (r *recording) Put(_ context.Context, key stateapi.Key, _ []byte) (uint64, error) {
	r.calls++
	r.key = key

	return 1, nil
}

func (r *recording) CompareAndPut(
	_ context.Context, key stateapi.Key, _ uint64, _ []byte,
) (uint64, error) {
	r.calls++
	r.key = key

	return 1, nil
}

func (r *recording) Delete(context.Context, stateapi.Key) error {
	r.calls++

	return nil
}

func (r *recording) List(context.Context, stateapi.Key) ([]stateapi.Key, error) {
	r.calls++

	return nil, nil
}

func (r *recording) Version(context.Context, stateapi.Key) (uint64, error) {
	r.calls++

	return 1, nil
}

func (r *recording) Usage(context.Context) (stateapi.Usage, error) {
	r.calls++

	return stateapi.Usage{Bytes: 42, Limit: 128 * stateapi.NamespaceMiB}, nil
}

// refusing is a Records that answers one refusal to everything.
type refusing struct{ err error }

func (r *refusing) Get(context.Context, stateapi.Key) (stateapi.Value, error) {
	return stateapi.Value{}, r.err
}

func (r *refusing) Put(context.Context, stateapi.Key, []byte) (uint64, error) { return 0, r.err }

func (r *refusing) CompareAndPut(context.Context, stateapi.Key, uint64, []byte) (uint64, error) {
	return 0, r.err
}

func (r *refusing) Delete(context.Context, stateapi.Key) error { return r.err }

func (r *refusing) List(context.Context, stateapi.Key) ([]stateapi.Key, error) {
	return nil, r.err
}

func (r *refusing) Version(context.Context, stateapi.Key) (uint64, error) { return 0, r.err }

func (r *refusing) Usage(context.Context) (stateapi.Usage, error) { return stateapi.Usage{}, r.err }

// loopback is the two halves joined with no process in between: a client's call
// runs the host's handler and the answer comes back the way the wire renders
// it. It proves the encoding, and it proves nothing about a process boundary,
// which internal/app's case does with a real one.
type loopback struct {
	records stateapi.Records
	calls   int
}

func (l *loopback) Call(
	ctx context.Context, method wirerpc.Method, params any,
) (json.RawMessage, error) {
	l.calls++

	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	handler, served := stateapi.Handlers(l.records)[method]
	if !served {
		return nil, &wirerpc.Error{Code: wirerpc.CodeMethodNotFound, Message: string(method)}
	}

	result, err := handler(ctx, wirerpc.Call{Method: method, Params: body})
	if err != nil {
		return nil, err
	}

	return json.Marshal(result)
}
