package mqtt

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi/eventsapitest"
)

// The published suite every event sink is held to, ours included. The broker is
// a black hole rather than mosquitto: every clause of the Processor contract is
// a property of what happens when the destination stops answering, and the
// topic contract is held against a real broker in contract_test.go.
func TestSinkContract(t *testing.T) {
	address := eventsapitest.BlackHole(t)

	eventsapitest.RunProcessorContract(t, func(t *testing.T) eventsapi.Processor {
		built, err := New(eventsapi.ProviderSpec{
			Type: Type,
			Config: map[string]any{
				"url":             "tcp://" + address,
				"connect_timeout": "1s",
				"publish_timeout": "1s",
			},
		})
		require.NoError(t, err)

		return built
	})
}
