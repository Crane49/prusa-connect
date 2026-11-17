package arch_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// The SSE document is a capture: a committed data file that stands in for
// behaviour, so the code it describes can be deleted. A capture nothing compares against drifts in silence, and a client
// written from a document that omits an event never learns the event exists.
// Its MQTT twin is held by TestTheTopicTableMatchesTheShippingAsyncAPIDocument.
//
// Both sides are captures under testdata/ here. The document was read out of
// legacy/transport/sse/asyncapi.yaml and the event types out of the `EventType`
// constants in legacy/printer/state/broadcaster.go, and neither gate reaches
// into the quarantine any more. So the pair survives the deletion of the server
// they describe, and the gate fails the day somebody edits one of the two
// without the other.
//
// The comparison is over channel names. A name is what a client subscribes on
// and it is the half a reader can check without running the server. Four things
// this cannot catch, written down so the next person does not infer them:
//
//   - a payload shape that drifts from the Go type it describes. Only the name
//     is compared
//   - a description or a summary that stops describing its channel
//   - a constant declared and never published. The declaration was the
//     authority when this was frozen, so a dead one is required in the document
//   - a channel the server published without a constant, built from a string
//     literal at the call site
const (
	sseCaptureDocument  = "testdata/shipping-sse-asyncapi.yaml"
	sseEventTypeFixture = "legacy-sse-event-types.tsv"
)

// sseEventTypeCount is how many event types the shipping server published. It
// is the floor under both directions of the comparison: two empty sets agree,
// and a reader that stopped finding anything would otherwise pass.
const sseEventTypeCount = 11

// TestTheSSEDocumentDescribesEveryEventTypeTheServerPublishes is the divergence
// test. It fails in both directions: an event type with no channel, and a
// channel with no event type.
func TestTheSSEDocumentDescribesEveryEventTypeTheServerPublishes(t *testing.T) {
	t.Parallel()

	declared := sseEventTypes(t)
	described := sseChannels(t, sseCaptureDocument)

	require.Lenf(t, declared, sseEventTypeCount,
		"%s/%s carries %d event types and the floor here is %d",
		captureDirectory, sseEventTypeFixture, len(declared), sseEventTypeCount)

	require.Equal(t, declared, described,
		"every event type the shipping server published is a channel in %s, and every channel is one it published",
		sseCaptureDocument)
}

// sseEventTypes is every event type the frozen capture carries, sorted. The
// second column is the value a client reads; the first names the constant it
// was declared as.
func sseEventTypes(t *testing.T) []string {
	t.Helper()

	rows := captureRows(t, sseEventTypeFixture)

	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, row[1])
	}

	sort.Strings(values)

	return values
}

// sseChannels is every channel name the document declares, sorted.
func sseChannels(t *testing.T, path string) []string {
	t.Helper()

	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var document struct {
		Channels map[string]yaml.Node `yaml:"channels"`
	}

	if err = yaml.Unmarshal(content, &document); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	names := make([]string, 0, len(document.Channels))
	for name := range document.Channels {
		names = append(names, strings.TrimSpace(name))
	}

	sort.Strings(names)

	return names
}
