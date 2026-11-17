package webhook_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/events/webhook"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// publishedKinds is what an integrator reading the published reference will
// filter on, written out here rather than read from the adapter. A gate that
// asks the code what it produces agrees with every answer including the wrong
// one, which is how the MQTT reference came to name four topics no subscriber
// could match.
var publishedKinds = []string{
	"printer.state_changed",
	"printer.labels_changed",
	"printer.deleted",
	"job.updated",
	"label.deleted",
	"fact.observed",
}

// TestTheReferenceListsTheKindsTheWireCarries reads one side off the wire and
// the other out of the registry the published reference is generated from, so a
// reference that drifts from the bodies this sink posts fails here.
//
// Before kinds.go the two sides were separate string literals that agreed by
// hand. Nothing in this package compared them: changing an offer call and its
// wire assertion together left the reference wrong and every gate green.
func TestTheReferenceListsTheKindsTheWireCarries(t *testing.T) {
	t.Parallel()

	got := &received{}
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got.add(r)
	}))

	defer server.Close()

	p := newProcessor(t, map[string]any{"url": server.URL})

	// Driven in the order the reference lists them, so a mismatch names the
	// entry rather than the set.
	p.PrinterStateChanged(eventsapi.PrinterState{ID: "printer-1", State: "PRINTING"})
	p.PrinterLabelsChanged(eventsapi.PrinterLabels{ID: "printer-1", Mode: "replace", Labels: []string{"floor-2"}})
	p.PrinterDeleted("printer-2")
	p.JobUpdated(eventsapi.JobInfo{ID: "job-1", PrinterID: "printer-1", State: "PRINTING"})
	p.LabelDeleted("label-1")
	p.FactObserved(eventsapi.Fact{
		Origin:      referenceOrigin(),
		Type:        "com.prusa.connect.device.command.settled",
		Class:       event.ClassState,
		Sequence:    1,
		CausationID: "01JC000000000000000000000C",
		Payload:     json.RawMessage(`{"status":"succeeded"}`),
	})

	require.NoError(t, p.Close())

	bodies := got.all()
	require.Len(t, bodies, len(publishedKinds))

	onTheWire := make([]string, 0, len(bodies))

	for _, body := range bodies {
		kind, ok := body["kind"].(string)
		require.True(t, ok, "every posted body carries a string kind, got %#v", body["kind"])

		onTheWire = append(onTheWire, kind)
	}

	assert.Equal(t, publishedKinds, onTheWire, "the kind the endpoint received")

	assert.Equal(t, publishedKinds, referenceAddresses(t), "the addresses the published reference lists")

	t.Logf("kinds the endpoint received: %v", onTheWire)
}

// referenceAddresses is what the published extension reference says this sink
// publishes to, read out of the registry the generator drains rather than out
// of doc(), because the registry is what the artefact is made from.
func referenceAddresses(t *testing.T) []string {
	t.Helper()

	doc := registeredDoc(t)

	assert.Empty(t, doc.Emits, "a sink puts nothing on the host's event bus, so it emits nothing")

	out := make([]string, 0, len(doc.Publishes))
	for _, destination := range doc.Publishes {
		out = append(out, destination.Address)
	}

	return out
}

func registeredDoc(t *testing.T) extension.Doc {
	t.Helper()

	for _, descriptor := range eventsapi.Descriptors() {
		if descriptor.Name() == webhook.Type {
			return descriptor.Doc()
		}
	}

	t.Fatalf("%q did not register, so this gate is comparing against nothing", webhook.Type)

	return extension.Doc{}
}

// TestEverySummaryInTheReferenceSaysWhichKindItIs keeps the table honest in the
// half a mismatched address cannot catch: a table of entries carrying one
// summary would publish a reference that reads as copies of the same line.
func TestEverySummaryInTheReferenceSaysWhichKindItIs(t *testing.T) {
	t.Parallel()

	doc := registeredDoc(t)
	require.Len(t, doc.Publishes, len(publishedKinds))

	seen := make(map[string]string, len(doc.Publishes))

	for _, destination := range doc.Publishes {
		require.NotEmpty(t, destination.Summary, "%s has no summary", destination.Address)

		if first, repeated := seen[destination.Summary]; repeated {
			t.Errorf("%s and %s carry the same summary %q", first, destination.Address, destination.Summary)
		}

		seen[destination.Summary] = destination.Address
	}
}

// referenceOrigin is the printer every fact in this file is about. A fact with
// no origin is not published, so the gate needs a real one to have anything to
// compare.
func referenceOrigin() event.Origin {
	origin, _ := event.NewOrigin(event.KindDevice, "printer-1")

	return origin
}
