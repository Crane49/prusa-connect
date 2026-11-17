package mqtt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// queuedPublisher is a Publisher with no connection, whose queue a test reads.
func queuedPublisher() *Publisher {
	return &Publisher{
		router:  NewRouter(nil, ""),
		changes: newChangeFilter(),
		queue:   make(chan message, 64),
		done:    make(chan struct{}),
	}
}

// queued drains what the publisher queued, as topic to payload.
func queued(p *Publisher) map[string]message {
	out := map[string]message{}

	for {
		select {
		case msg := <-p.queue:
			out[msg.topic] = msg
		default:
			return out
		}
	}
}

// TestEachToolNodeLandsOnItsChannel holds the map from the tool tree onto the
// tool channels (ADR-20261025-frz): a head has four channels, a feeder slot two, and
// the id in the topic is the tree's own.
func TestEachToolNodeLandsOnItsChannel(t *testing.T) {
	t.Parallel()

	p := &Processor{Publisher: queuedPublisher()}

	for path, value := range map[string]string{
		"/tools/0/active":             `true`,
		"/tools/0/nozzle_temp":        `214.6`,
		"/tools/0/nozzle_target_temp": `215`,
		"/tools/1/active":             `false`,
		"/tools/1/material_package_instance/package/material/abbreviation": `"PETG"`,
		"/tools/0.2/active": `true`,
		"/tools/0.2/material_package_instance/package/material/abbreviation": `"PLA"`,
		"/tools/0.3/material_package_instance/package/material/abbreviation": `""`,
	} {
		p.SamplesObserved(eventsapi.Samples{
			DeviceID: "abc",
			Nodes:    []eventsapi.Sample{{Path: path, Value: json.RawMessage(value)}},
		})
	}

	got := queued(p.Publisher)

	want := map[string]string{
		"v1/devices/printers/abc/data/tools/0/active":              "1",
		"v1/devices/printers/abc/data/tools/0/temp/nozzle/current": "215",
		"v1/devices/printers/abc/data/tools/0/temp/nozzle/target":  "215",
		"v1/devices/printers/abc/data/tools/1/active":              "0",
		"v1/devices/printers/abc/data/tools/1/material":            "PETG",
		"v1/devices/printers/abc/data/tools/0.2/active":            "1",
		"v1/devices/printers/abc/data/tools/0.2/material":          "PLA",
		"v1/devices/printers/abc/data/tools/0.3/material":          "",
	}

	require.Len(t, got, len(want), "%v", got)

	for topic, payload := range want {
		msg, published := got[topic]
		if assert.True(t, published, "nothing was queued on %s", topic) {
			assert.Equal(t, payload, msg.payload, "payload on %s", topic)
		}
	}

	assert.True(t, got["v1/devices/printers/abc/data/tools/0/active"].retain, "active is retained")
	assert.True(t, got["v1/devices/printers/abc/data/tools/1/material"].retain, "the material is retained")
	assert.False(t, got["v1/devices/printers/abc/data/tools/0/temp/nozzle/current"].retain,
		"a reading is not retained")
}

// TestAnOffHeaterIsATargetOfZero holds the owner's rule for the target
// (ADR-20261025-frz rule 3): a heater that is off is a target of 0 in the tree, and 0
// is published. A removed target is not known, and it is an empty payload,
// never the string "null".
func TestAnOffHeaterIsATargetOfZero(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]string{`0`: "0", `-1`: "0", `null`: ""} {
		p := queuedPublisher()
		p.PublishToolSample("abc", "/tools/0/nozzle_target_temp", json.RawMessage(value))

		msg, published := queued(p)["v1/devices/printers/abc/data/tools/0/temp/nozzle/target"]
		require.True(t, published, "a target of %s was not published", value)
		assert.Equal(t, want, msg.payload, "a target of %s", value)
	}
}

// TestAValueThatIsNotKnownClearsItsChannel holds the empty payload for a node
// the device removed (ADR-20261025-frz rule 3). On a retained channel the empty
// payload is retained, which is what removes the value from the broker.
func TestAValueThatIsNotKnownClearsItsChannel(t *testing.T) {
	t.Parallel()

	p := queuedPublisher()

	for path, value := range map[string]string{
		"/tools/0/active":      `true`,
		"/tools/0/nozzle_temp": `214`,
		"/tools/0.1/material_package_instance/package/material/abbreviation": `"PLA"`,
	} {
		p.PublishToolSample("abc", path, json.RawMessage(value))
	}

	queued(p)

	for _, path := range []string{
		"/tools/0/active", "/tools/0/nozzle_temp",
		"/tools/0.1/material_package_instance/package/material/abbreviation",
	} {
		p.PublishToolSample("abc", path, json.RawMessage(`null`))
	}

	got := queued(p)

	for topic, retained := range map[string]bool{
		"v1/devices/printers/abc/data/tools/0/active":              true,
		"v1/devices/printers/abc/data/tools/0/temp/nozzle/current": false,
		"v1/devices/printers/abc/data/tools/0.1/material":          true,
	} {
		msg, published := got[topic]
		if assert.True(t, published, "nothing was queued on %s", topic) {
			assert.Empty(t, msg.payload, "payload on %s", topic)
			assert.Equal(t, retained, msg.retain, "retain on %s", topic)
		}
	}
}

// TestADisabledToolClearsEveryChannelOfIt covers enabled false, the
// architecture's null tool: a head loses its four channels and a slot its
// two, whatever they held before.
func TestADisabledToolClearsEveryChannelOfIt(t *testing.T) {
	t.Parallel()

	p := queuedPublisher()
	p.PublishToolSample("abc", "/tools/1/enabled", json.RawMessage(`false`))
	p.PublishToolSample("abc", "/tools/0.3/enabled", json.RawMessage(`false`))
	p.PublishToolSample("abc", "/tools/0/enabled", json.RawMessage(`true`))

	got := queued(p)

	want := []string{
		"v1/devices/printers/abc/data/tools/1/active",
		"v1/devices/printers/abc/data/tools/1/material",
		"v1/devices/printers/abc/data/tools/1/temp/nozzle/current",
		"v1/devices/printers/abc/data/tools/1/temp/nozzle/target",
		"v1/devices/printers/abc/data/tools/0.3/active",
		"v1/devices/printers/abc/data/tools/0.3/material",
	}

	require.Len(t, got, len(want), "%v", got)

	for _, topic := range want {
		msg, published := got[topic]
		if assert.True(t, published, "nothing was queued on %s", topic) {
			assert.Empty(t, msg.payload, "payload on %s", topic)
		}
	}
}

// TestAWholeTreeClearsTheToolsThatLeftIt is the MMU switched off while no
// sample said so: the next whole tree names head 0 alone, and every channel
// of the slots that held a value is cleared. A channel the tree still sets is
// not cleared.
func TestAWholeTreeClearsTheToolsThatLeftIt(t *testing.T) {
	t.Parallel()

	p := &Processor{Publisher: queuedPublisher()}

	p.SamplesObserved(eventsapi.Samples{DeviceID: "abc", Whole: true, Nodes: []eventsapi.Sample{
		{Path: "/tools/0/active", Value: json.RawMessage(`true`)},
		{Path: "/tools/0.0/active", Value: json.RawMessage(`true`)},
		{Path: "/tools/0.0/material_package_instance/package/material/abbreviation", Value: json.RawMessage(`"PLA"`)},
		{Path: "/tools/0.1/active", Value: json.RawMessage(`false`)},
	}})
	queued(p.Publisher)

	p.SamplesObserved(eventsapi.Samples{DeviceID: "abc", Whole: true, Nodes: []eventsapi.Sample{
		{Path: "/tools/0/active", Value: json.RawMessage(`true`)},
	}})

	got := queued(p.Publisher)

	want := []string{
		"v1/devices/printers/abc/data/tools/0.0/active",
		"v1/devices/printers/abc/data/tools/0.0/material",
		"v1/devices/printers/abc/data/tools/0.1/active",
	}

	require.Len(t, got, len(want), "%v", got)

	for _, topic := range want {
		msg, published := got[topic]
		if assert.True(t, published, "nothing was queued on %s", topic) {
			assert.Empty(t, msg.payload, "payload on %s", topic)
			assert.True(t, msg.retain, "the clear on %s is retained", topic)
		}
	}
}

// TestANodeTheToolChannelsDoNotCarryIsNotPublished covers the paths that reach
// the sink and have no tool channel: another node of the tree, a nozzle node
// under a feeder slot, a value of the wrong type, and a path outside the tree.
func TestANodeTheToolChannelsDoNotCarryIsNotPublished(t *testing.T) {
	t.Parallel()

	p := queuedPublisher()

	for path, value := range map[string]string{
		"/tools/0/print_fan_rpm":        `3000`,
		"/tools/0.1/nozzle_temp":        `200`,
		"/tools/0.1/nozzle_target_temp": `200`,
		"/tools/2/active":               `"yes"`,
		"/tools/2/material_package_instance/package/material/abbreviation": `7`,
		"/tools/2/nozzle_temp":   `"hot"`,
		"/tools/2/enabled":       `"no"`,
		"/tools/00/active":       `true`,
		"/temperature/bed":       `60`,
		"/tool_count":            `2`,
		"/tools/0/features/type": `"x"`,
	} {
		p.PublishToolSample("abc", path, json.RawMessage(value))
	}

	assert.Empty(t, queued(p))
}
