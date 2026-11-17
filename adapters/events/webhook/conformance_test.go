package webhook_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/adapters/events/webhook"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi/eventsapitest"
)

// The published suite every event sink is held to, ours included. The endpoint
// is a black hole rather than a working server: every clause of the Processor
// contract is a property of what happens when the destination stops answering.
func TestSinkContract(t *testing.T) {
	address := eventsapitest.BlackHole(t)

	eventsapitest.RunProcessorContract(t, func(t *testing.T) eventsapi.Processor {
		built, err := webhook.New(eventsapi.ProviderSpec{
			Type:   webhook.Type,
			Config: map[string]any{"url": "http://" + address + "/events", "timeout": "1s"},
		})
		require.NoError(t, err)

		return built
	})
}
