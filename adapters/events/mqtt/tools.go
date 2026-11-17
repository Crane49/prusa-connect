package mqtt

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// The payloads of the tool channels.
//
// A value in the tree is published as it is. A heater that is off has a
// target of 0, and 0 is published. A value that is not known has an empty
// payload, never "null". On a retained channel an empty payload also removes
// the retained message.
//
// A value is not known when the node is removed, when the tool is disabled,
// and when the tool leaves the tree. A tool with nothing loaded has an empty
// material too.

// toolMessage is one tool channel and the payload one node puts on it.
type toolMessage struct {
	topic    Topic
	resolved string
	payload  string
}

// toolMessages maps one node of a printer's tool tree onto the tool channels
// it sets. It returns nothing for a path no tool channel carries, and for a
// value of the wrong type.
func toolMessages(printerID, path string, value json.RawMessage) []toolMessage {
	id, member, ok := connectorapi.SplitToolPath(path)
	if !ok || printerID == "" {
		return nil
	}

	on := func(topic Topic, payload string) []toolMessage {
		return []toolMessage{{topic: topic, resolved: topic.ForTool(printerID, string(id)), payload: payload}}
	}

	switch member {
	case connectorapi.ToolEnabled:
		// A disabled or removed tool has no other member. Clear each of its
		// channels, so no old value stays on the broker.
		var enabled bool
		if isNull(value) || (json.Unmarshal(value, &enabled) == nil && !enabled) {
			return clearedTool(printerID, id)
		}
	case connectorapi.ToolActive:
		if payload, read := flagOrUnknown(value); read {
			return on(ToolActive, payload)
		}
	case connectorapi.MaterialAbbreviation:
		// connectorapi.NoMaterial is the empty string, so it needs no case of
		// its own.
		if payload, read := textOrUnknown(value); read {
			return on(ToolMaterial, payload)
		}
	case connectorapi.NozzleTemp, connectorapi.NozzleTargetTemp:
		// A heater that is off has a target of 0 in the tree, published as 0.
		// A removed target is not known.
		topic := ToolNozzleTemperature
		if member == connectorapi.NozzleTargetTemp {
			topic = ToolNozzleTarget
		}

		if payload, read := temperatureOrUnknown(value); read && id.IsHead() {
			return on(topic, payload)
		}
	}

	return nil
}

// clearedTool is an empty payload on every channel of one tool. A feeder slot
// has no nozzle channels.
func clearedTool(printerID string, id connectorapi.ToolID) []toolMessage {
	topics := []Topic{ToolActive, ToolMaterial}
	if id.IsHead() {
		topics = append(topics, ToolNozzleTemperature, ToolNozzleTarget)
	}

	out := make([]toolMessage, 0, len(topics))
	for _, topic := range topics {
		out = append(out, toolMessage{topic: topic, resolved: topic.ForTool(printerID, string(id))})
	}

	return out
}

// temperatureOrUnknown is the payload of a temperature node: whole degrees,
// never below 0, or empty for a removed node. read is false for a value that
// is not a number.
func temperatureOrUnknown(value json.RawMessage) (payload string, read bool) {
	if isNull(value) {
		return "", true
	}

	var celsius float64
	if json.Unmarshal(value, &celsius) != nil {
		return "", false
	}

	return temperaturePayload(max(celsius, 0)), true
}

// flagOrUnknown is the payload of a boolean node: "1", "0", or empty for a
// removed node. read is false for a value that is not a boolean.
func flagOrUnknown(value json.RawMessage) (payload string, read bool) {
	if isNull(value) {
		return "", true
	}

	var flag bool
	if json.Unmarshal(value, &flag) != nil {
		return "", false
	}

	return boolPayload(flag), true
}

// textOrUnknown is the payload of a text node, or empty for a removed node.
// read is false for a value that is not text.
func textOrUnknown(value json.RawMessage) (payload string, read bool) {
	if isNull(value) {
		return "", true
	}

	var text string
	if json.Unmarshal(value, &text) != nil {
		return "", false
	}

	return text, true
}

func isNull(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)

	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

// toolTopics is, for each printer, every tool channel this publisher has given
// a value. A whole tree that no longer gives one of them a value clears it, so
// a tool that left the tree while no sample said so is cleared too.
type toolTopics struct {
	mu  sync.Mutex
	set map[string]map[string]Topic
}

// note records what one message leaves on the broker.
func (t *toolTopics) note(printerID string, msg toolMessage) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if msg.payload == "" {
		delete(t.set[printerID], msg.resolved)

		return
	}

	if t.set == nil {
		t.set = map[string]map[string]Topic{}
	}

	if t.set[printerID] == nil {
		t.set[printerID] = map[string]Topic{}
	}

	t.set[printerID][msg.resolved] = msg.topic
}

// outside is every channel of the printer with a value that the given set of
// resolved topics does not name, in topic order.
func (t *toolTopics) outside(printerID string, named map[string]bool) []toolMessage {
	t.mu.Lock()
	defer t.mu.Unlock()

	var out []toolMessage

	for _, resolved := range slices.Sorted(maps.Keys(t.set[printerID])) {
		if !named[resolved] {
			out = append(out, toolMessage{topic: t.set[printerID][resolved], resolved: resolved})
		}
	}

	return out
}

// forget drops what is recorded for one printer.
func (t *toolTopics) forget(printerID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.set, printerID)
}

// PublishToolSample publishes one node of a printer's tool tree on its tool
// channel. It handles enabled, active, the material, and a head's nozzle
// temperature and target. It does nothing for any other path.
func (p *Publisher) PublishToolSample(printerID, path string, value json.RawMessage) {
	for _, msg := range toolMessages(printerID, path, value) {
		p.offerTool(printerID, msg)
	}
}

// PublishToolTree publishes a printer's whole tool tree. It clears every tool
// channel that has a value on the broker and none in the tree.
func (p *Publisher) PublishToolTree(printerID string, nodes []eventsapi.Sample) {
	named := map[string]bool{}

	for _, node := range nodes {
		for _, msg := range toolMessages(printerID, node.Path, node.Value) {
			named[msg.resolved] = true

			p.offerTool(printerID, msg)
		}
	}

	for _, msg := range p.tools.outside(printerID, named) {
		p.offerTool(printerID, msg)
	}
}

func (p *Publisher) offerTool(printerID string, msg toolMessage) {
	p.tools.note(printerID, msg)
	p.offer(msg.topic, msg.resolved, msg.payload)
}
