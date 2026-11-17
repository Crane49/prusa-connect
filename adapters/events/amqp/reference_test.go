package amqp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// The bus envelope's package, aliased because this package's own wire
	// struct is called event.
	busevent "github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// publishedKeys is what a binding on the exchange will select on, written out
// here rather than read from the adapter. A gate that asks the code what it
// produces agrees with every answer including the wrong one, which is how the
// MQTT reference came to name four topics no subscriber could match.
var publishedKeys = []string{
	"printer.state_changed",
	"printer.labels_changed",
	"printer.deleted",
	"job.updated",
	"label.deleted",
	"fact.observed",
}

// TestTheReferenceListsTheKeysTheWireCarries reads one side off the messages
// bound for the socket and the other out of the registry the published
// reference is generated from, so a reference that drifts from what a consumer
// binds to fails here.
//
// It runs without a broker on purpose. Before kinds.go the two sides were
// separate string literals, and the only case that saw a changed routing key
// was the one that needs RabbitMQ: with the fixture down, this adapter could
// publish a key the reference does not list and the whole suite stayed green.
// TestEveryEventReachesTheBroker makes the same comparison with RabbitMQ on the
// wire side, so the encoder between the queue and the socket is in the chain
// too.
func TestTheReferenceListsTheKeysTheWireCarries(t *testing.T) {
	t.Parallel()

	// Built without run(), so nothing drains the queue and the case reads the
	// routing key and body that client.publish would have been handed.
	p := &Processor{
		queue:   make(chan message, len(publishedKeys)),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}

	offerOneOfEach(p)

	close(p.queue)

	keys := make([]string, 0, len(publishedKeys))

	for queued := range p.queue {
		var body struct {
			Kind string `json:"kind"`
		}

		require.NoError(t, json.Unmarshal(queued.body, &body))
		assert.Equal(t, queued.key, body.Kind, "the routing key and the body's kind are one name")

		keys = append(keys, queued.key)
	}

	assert.Equal(t, publishedKeys, keys, "the routing keys bound for the exchange")

	assert.Equal(t, publishedKeys, referenceAddresses(t), "the addresses the published reference lists")

	t.Logf("routing keys bound for the exchange: %v", keys)
}

// offerOneOfEach drives every Processor method once, in the order the reference
// lists them, so a mismatch names the entry rather than the set.
func offerOneOfEach(p *Processor) {
	p.PrinterStateChanged(eventsapi.PrinterState{ID: "printer-1", State: "PRINTING"})
	p.PrinterLabelsChanged(eventsapi.PrinterLabels{ID: "printer-1", Mode: "replace", Labels: []string{"floor-2"}})
	p.PrinterDeleted("printer-2")
	p.JobUpdated(eventsapi.JobInfo{ID: "job-1", ExternalID: 42, PrinterID: "printer-1", State: "PRINTING"})
	p.LabelDeleted("label-1")
	p.FactObserved(eventsapi.Fact{
		Origin:      referenceOrigin(),
		Type:        "com.prusa.connect.device.command.settled",
		Class:       busevent.ClassState,
		Sequence:    1,
		CausationID: "01JC000000000000000000000C",
		Payload:     json.RawMessage(`{"status":"succeeded"}`),
	})
}

// referenceOrigin is the printer every fact in this file is about. A fact with
// no origin is not published, so the gate needs a real one to have anything to
// compare.
func referenceOrigin() busevent.Origin {
	origin, _ := busevent.NewOrigin(busevent.KindDevice, "printer-1")

	return origin
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
		if descriptor.Name() == Type {
			return descriptor.Doc()
		}
	}

	t.Fatalf("%q did not register, so this gate is comparing against nothing", Type)

	return extension.Doc{}
}

// TestEverySummaryInTheReferenceSaysWhichKeyItIs keeps the table honest in the
// half a mismatched address cannot catch: five entries carrying one summary
// would publish a reference that reads as five copies of the same line.
func TestEverySummaryInTheReferenceSaysWhichKeyItIs(t *testing.T) {
	t.Parallel()

	doc := registeredDoc(t)
	require.Len(t, doc.Publishes, len(publishedKeys))

	seen := make(map[string]string, len(doc.Publishes))

	for _, destination := range doc.Publishes {
		require.NotEmpty(t, destination.Summary, "%s has no summary", destination.Address)

		if first, repeated := seen[destination.Summary]; repeated {
			t.Errorf("%s and %s carry the same summary %q", first, destination.Address, destination.Summary)
		}

		seen[destination.Summary] = destination.Address
	}
}
