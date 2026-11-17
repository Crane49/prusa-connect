package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// TestAProviderDoesNotStoreItsOwnLifecycle is the rule collect applies to the
// frames published under the instance's own origin.
//
// Measured on 2026-09-10, TestASecondRestartWritesNothingMore on CI: a restart
// over a store with nothing to sweep counted one fact more than the run before
// it, and the row was the provider's own ready transition, stored because the
// feed had attached it before the hook put that frame on the bus. Whether the
// row existed depended on which goroutine ran first, and a store whose rows
// depend on scheduling is not a record. The frames of every other resource
// are stored as before.
func TestAProviderDoesNotStoreItsOwnLifecycle(t *testing.T) {
	t.Parallel()

	own, err := event.NewOrigin(event.Kind(ref.KindHistory), "past")
	require.NoError(t, err)

	other, err := event.NewOrigin(event.Kind(ref.KindHistory), "audit")
	require.NoError(t, err)

	printer, err := event.NewOrigin(event.KindDevice, "mk4-017")
	require.NoError(t, err)

	feed := &historyFeed{}
	feeding := &historyStream{
		own:      own,
		caps:     historyapi.CapFacts,
		consumes: []event.Class{event.ClassState},
	}

	batch := stream.Batch{Events: []stream.Frame{
		commandFrame(own, event.TypeResourceStateChanged, event.ClassState, "3", "",
			`{"resource":"history:past","state":"ready"}`),
		commandFrame(other, event.TypeResourceStateChanged, event.ClassState, "4", "",
			`{"resource":"history:audit","state":"ready"}`),
		commandFrame(printer, event.TypeDeviceArrived, event.ClassState, "5", "", `{}`),
	}}

	pending := historyBatch{}
	feed.collect(feeding, batch, &pending)

	stored := make([]string, 0, len(pending.facts))
	for _, fact := range pending.facts {
		stored = append(stored, fact.Ref.String())
	}

	require.Equal(t, []string{other.String(), printer.String()}, stored,
		"the provider's own transition reached the batch, or another origin's did not")

	t.Logf("observed: a batch of three origins stored %v and left %s out", stored, own)
}
