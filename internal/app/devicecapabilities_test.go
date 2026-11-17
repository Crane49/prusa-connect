package app

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// servedStateEntry is the part of one device-state vocabulary entry the case
// below reads.
type servedStateEntry struct {
	Connectors []string `json:"connectors"`
	Guarded    bool     `json:"guarded"`
	States     []struct {
		State   string   `json:"state"`
		Accepts []string `json:"accepts"`
	} `json:"states"`
}

// TestADialledPrinterServesItsStatesAndItsArguments is the path a client takes
// from a device record to what it may send: the driver the record names leads
// to an entry of the device-state vocabulary, and the capabilities endpoint
// says what each verb takes. Both answers come from what the host mounted and
// resolved, so this runs the whole server and a simulated printer.
func TestADialledPrinterServesItsStatesAndItsArguments(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	dialIn(t, running, "SIM-0101")

	listed := printers(t, running)
	require.Len(t, listed, 1)

	device := running.get(t, "/api/v1/devices/"+listed[0].ID)
	require.Equal(t, http.StatusOK, device.status, device.body)

	var record struct {
		Driver string `json:"driver"`
	}
	require.NoError(t, json.Unmarshal([]byte(device.body), &record))

	vocabulary := running.get(t, "/api/v1/vocabularies/device-states")
	require.Equal(t, http.StatusOK, vocabulary.status, vocabulary.body)

	var states struct {
		Items []servedStateEntry `json:"items"`
	}
	require.NoError(t, json.Unmarshal([]byte(vocabulary.body), &states))

	index := slices.IndexFunc(states.Items, func(item servedStateEntry) bool {
		return slices.Contains(item.Connectors, record.Driver)
	})
	require.GreaterOrEqual(
		t,
		index,
		0,
		"no entry lists the driver %q the device names: %s",
		record.Driver,
		vocabulary.body,
	)
	require.True(t, states.Items[index].Guarded)
	require.NotEmpty(t, states.Items[index].States)

	resolved := running.get(t, "/api/v1/devices/"+listed[0].ID+"/capabilities")
	require.Equal(t, http.StatusOK, resolved.status, resolved.body)

	var capabilities struct {
		Capabilities []struct {
			Key       string `json:"key"`
			Arguments []struct {
				Name    string   `json:"name"`
				Type    string   `json:"type"`
				Minimum *float64 `json:"minimum"`
				Maximum *float64 `json:"maximum"`
				Unit    string   `json:"unit"`
			} `json:"arguments"`
		} `json:"capabilities"`
	}
	require.NoError(t, json.Unmarshal([]byte(resolved.body), &capabilities))

	for _, capability := range capabilities.Capabilities {
		if capability.Key != "core.command.set_bed_temperature" {
			continue
		}

		require.Len(t, capability.Arguments, 1)
		argument := capability.Arguments[0]
		require.Equal(t, "bed_temperature", argument.Name)
		require.Equal(t, "integer", argument.Type)
		require.Equal(t, "celsius", argument.Unit)
		require.NotNil(t, argument.Minimum)
		require.NotNil(t, argument.Maximum)
		require.InDelta(t, 0, *argument.Minimum, 0)
		require.LessOrEqual(t, *argument.Maximum, 120.0)

		return
	}

	t.Fatalf("the printer serves no bed temperature verb: %s", resolved.body)
}

func TestTheCapabilitiesOfAnUnknownIDAreAbsent(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	missing := running.get(t, "/api/v1/devices/00000000-0000-4000-8000-00000dead000/capabilities")
	require.Equal(t, http.StatusNotFound, missing.status, missing.body)
}
