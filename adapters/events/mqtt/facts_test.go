package mqtt

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// The fact channel, observed through the fixture broker rather than through the
// adapter's own counters. A publish that the adapter believes it made and the
// broker never saw is the failure these cases exist for, and only a subscriber
// on the far side can tell the two apart.

// commandFact builds one of the three transitions a command contributes to its
// printer's origin, as the sink feed hands it over.
func commandFact(origin event.Origin, eventType string, class event.Class, sequence uint64, commandID string,
	payload string,
) eventsapi.Fact {
	return eventsapi.Fact{
		Origin:      origin,
		Type:        eventType,
		Class:       class,
		At:          time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC),
		Sequence:    sequence,
		Epoch:       "01JC0000000000000000000EPO",
		CausationID: commandID,
		Payload:     json.RawMessage(payload),
	}
}

// uniqueOrigin keeps one case's facts out of another's, the way uniquePrinter
// does for the frozen channels.
func uniqueOrigin(t *testing.T) event.Origin {
	t.Helper()

	origin, err := event.NewOrigin(event.KindDevice, uniqueName("printer-"))
	require.NoError(t, err)

	return origin
}

// TestACommandLifecycleFactReachesTheBroker is the proof the whole change was
// made for: the three frames a command contributes to its printer's origin
// reach a real broker, each on its own topic, with the envelope intact.
//
// It runs against the mosquitto fixture and skips when nothing answers, so a
// developer without Docker still gets the rest of the suite. See the Makefile's
// test-adapters target, which starts the fixture and removes it afterwards.
func TestACommandLifecycleFactReachesTheBroker(t *testing.T) {
	address := testBroker(t)
	origin := uniqueOrigin(t)

	const commandID = "01JC000000000000000000000C"

	p := connectedProcessor(t, address)

	// Subscribed before the publish, because the fact channel is not retained:
	// a subscriber that arrives later is meant to see nothing.
	live := subscribeAt(t, address, "v1/events/"+origin.Path()+"/#")

	facts := []eventsapi.Fact{
		commandFact(origin, "com.prusa.connect.device.command.requested", event.ClassCommand, 7, commandID,
			`{"command_id":"`+commandID+`","verb":"printer.pause"}`),
		commandFact(origin, "com.prusa.connect.device.command.authorized", event.ClassAudit, 8, commandID,
			`{"command_id":"`+commandID+`","verb":"printer.pause","auth_type":"user","auth_id":"ada"}`),
		commandFact(origin, "com.prusa.connect.device.command.settled", event.ClassState, 9, commandID,
			`{"command_id":"`+commandID+`","verb":"printer.pause","status":"succeeded","ok":true}`),
	}

	for _, fact := range facts {
		p.FactObserved(fact)
	}

	messages := live.await(t, len(facts))
	require.Len(t, messages, len(facts))

	for i, message := range messages {
		want := facts[i]

		assert.Equal(t, "v1/events/"+origin.Path()+"/"+want.Type, message.topic,
			"one topic per origin and event type")
		assert.Equal(t, byte(1), message.qos, "a fact is published at QoS 1")

		var got eventsapi.Fact
		require.NoError(t, json.Unmarshal([]byte(message.payload), &got),
			"the body a subscriber reads has to be the envelope")

		assert.Equal(t, want.Origin, got.Origin)
		assert.Equal(t, want.Type, got.Type)
		assert.Equal(t, want.Class, got.Class)
		assert.True(t, want.At.Equal(got.At), "time on %s", message.topic)
		assert.Equal(t, want.Sequence, got.Sequence)
		assert.Equal(t, want.Epoch, got.Epoch)
		assert.Equal(t, commandID, got.CausationID,
			"the causation id is what joins the three transitions into one invocation")
		assert.JSONEq(t, string(want.Payload), string(got.Payload))
	}

	// A subscriber that arrives after the fact sees nothing, which is the other
	// half of the retain decision: a command that settled an hour ago must not
	// be delivered to every dashboard that opens as though it had just
	// happened.
	late := subscribeAt(t, address, "v1/events/"+origin.Path()+"/#")
	late.settle()
	assert.Empty(t, late.seen(), "the fact channel is not retained")

	t.Logf("three command transitions observed on %s", "v1/events/"+origin.Path()+"/#")
}

// TestTwoIdenticalFactsAreBothPublished holds the one place the fact channel
// departs from the frozen channels. Publish-only-on-change is right for a state
// value and wrong for an occurrence: two identical commands are two commands,
// and a filter that swallowed the second would lose one of them.
func TestTwoIdenticalFactsAreBothPublished(t *testing.T) {
	address := testBroker(t)
	origin := uniqueOrigin(t)

	p := connectedProcessor(t, address)
	live := subscribeAt(t, address, "v1/events/"+origin.Path()+"/#")

	fact := commandFact(origin, "com.prusa.connect.device.command.requested", event.ClassCommand, 1,
		"01JC000000000000000000000C", `{"verb":"printer.pause"}`)

	p.FactObserved(fact)
	p.FactObserved(fact)

	messages := live.await(t, 2)
	assert.Len(t, messages, 2, "two identical facts are two things that happened")
}

// TestAFactWithNoOriginIsNotPublished keeps a frame that names nothing off the
// broker. A topic with an empty segment in it is a topic no filter was written
// for, and publishing one is worse than dropping the fact.
func TestAFactWithNoOriginIsNotPublished(t *testing.T) {
	address := testBroker(t)
	origin := uniqueOrigin(t)

	p := connectedProcessor(t, address)
	live := subscribeAt(t, address, "v1/events/#")

	p.FactObserved(eventsapi.Fact{Type: "com.prusa.connect.device.command.settled"})
	p.FactObserved(eventsapi.Fact{Origin: origin})

	// One good fact behind the two bad ones, so the case proves the publisher
	// was working rather than that nothing was published at all.
	p.FactObserved(commandFact(origin, "com.prusa.connect.device.command.settled", event.ClassState, 1, "",
		`{"status":"succeeded"}`))

	messages := live.await(t, 1)
	require.Len(t, messages, 1)
	assert.Equal(t, "v1/events/"+origin.Path()+"/com.prusa.connect.device.command.settled", messages[0].topic)
}
