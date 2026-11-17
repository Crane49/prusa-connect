package busapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// A subscription that names nothing follows everything the module may observe,
// which is the subscription a module opens first.
func TestASubscriptionThatNarrowsNothingIsValid(t *testing.T) {
	t.Parallel()

	if err := (busapi.SubscribeParams{}).Validate(); err != nil {
		t.Fatalf("the empty subscription was refused: %v", err)
	}
}

// The bounds are the hub's own, published so a module reads what it may ask for
// rather than discovering it in a refusal.
func TestASubscriptionOutsideTheBoundsIsRefused(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		params busapi.SubscribeParams
	}{
		{"too many origins", busapi.SubscribeParams{Origins: make([]string, busapi.MaxOrigins+1)}},
		{"too many types", busapi.SubscribeParams{Types: make([]string, busapi.MaxTypes+1)}},
		{"window under the floor", busapi.SubscribeParams{IntervalMS: busapi.MinIntervalMS - 1}},
		{"window over the ceiling", busapi.SubscribeParams{IntervalMS: busapi.MaxIntervalMS + 1}},
		{"cursor too large", busapi.SubscribeParams{Resume: string(make([]byte, busapi.MaxCursorBytes+1))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := tc.params.Validate(); !errors.Is(err, busapi.ErrInvalidSubscription) {
				t.Fatalf("Validate = %v, want ErrInvalidSubscription", err)
			}
		})
	}
}

// bus.subscribe writes a stream of windows, so it is opened on a stream the
// module owns. Made as a plain call it has nowhere to write, and saying so is
// better than writing nothing and reporting success.
func TestSubscribeMadeWithoutAStreamIsRefused(t *testing.T) {
	t.Parallel()

	methods := busapi.SubscribeHandler(feedOf(nil))

	handler, ok := methods[busapi.MethodSubscribe]
	if !ok {
		t.Fatal("bus.subscribe is not in the method set")
	}

	_, err := handler(t.Context(), wirerpc.Call{Method: busapi.MethodSubscribe})

	var coded *wirerpc.Error
	if !errors.As(err, &coded) || coded.Code != busapi.CodeNoStream {
		t.Fatalf("the handler answered %v, want code %d", err, busapi.CodeNoStream)
	}
}

// A host that serves no stream publishes nothing, rather than binding a nil
// that panics on the first call inside a supervised start.
func TestAHostWithNoFeedPublishesNoMethod(t *testing.T) {
	t.Parallel()

	if methods := busapi.SubscribeHandler(nil); len(methods) != 0 {
		t.Fatalf("SubscribeHandler(nil) published %d methods, want none", len(methods))
	}
}

// The envelopes stay encoded end to end. The hub marshalled each one once for
// every reader, and decoding to re-encode would be the cost its fan-out exists
// to avoid.
func TestTheEnvelopesAreCarriedAsTheyWereMarshalled(t *testing.T) {
	t.Parallel()

	envelope := json.RawMessage(`{"type":"com.prusa.connect.device.arrived","id":"01M"}`)

	window := busapi.Window{
		Events: []json.RawMessage{envelope},
		Cursor: "origin:4",
	}

	encoded, err := json.Marshal(window)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var read busapi.Window
	if err = json.Unmarshal(encoded, &read); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(read.Events) != 1 || string(read.Events[0]) != string(envelope) {
		t.Fatalf("the envelope changed crossing the wire: %s", read.Events[0])
	}

	if read.Cursor != "origin:4" {
		t.Fatalf("cursor = %q, want the one the window carried", read.Cursor)
	}
}

// A resync says continuity is gone, and it travels beside the cursor the reader
// carries on from. There is no partial recovery, so the flag has to survive.
func TestAResyncSurvivesTheWire(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(busapi.Window{Resync: true, Cursor: "origin:9"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var read busapi.Window
	if err = json.Unmarshal(encoded, &read); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !read.Resync || read.Cursor != "origin:9" {
		t.Fatalf("read = %+v, want the resync and its cursor", read)
	}
}

// feedOf is a host feed that writes the windows it was given.
type fakeFeed struct {
	windows []busapi.Window
	err     error
}

func feedOf(windows []busapi.Window) busapi.Feed { return &fakeFeed{windows: windows} }

func (f *fakeFeed) Subscribe(
	_ context.Context, _ busapi.SubscribeParams, write func(busapi.Window) error,
) error {
	for _, window := range f.windows {
		if err := write(window); err != nil {
			return err
		}
	}

	return f.err
}
