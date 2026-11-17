package amqp

import (
	"encoding/json"
	"maps"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi/eventsapitest"
)

// These run against a real broker. A publisher verified against a fake proves
// the fake: the handshake, the tuning, the exchange declaration and the
// publisher confirms are all things a broker does, and a hand-written encoder
// that a hand-written decoder reads back is not evidence that RabbitMQ accepts
// any of it.
//
//	docker compose -f docker-compose.local.yml up -d rabbitmq-fixture
//
// The fixture is docker/rabbitmq-fixture, part of the local stack, so a
// developer who has brought the dev environment up already has it. Point
// PCL_AMQP_TEST_BROKER at another broker to override, or leave it unset and the
// fixture's own port is used.
const brokerEnv = "PCL_AMQP_TEST_BROKER"

const defaultTestBroker = "127.0.0.1:55672"

// settle is how long a case waits for a message that should already be there.
// It is generous because a broker under a cold start is slower than a broker
// that has been running.
const settle = 3 * time.Second

// fixtureAddress is where the broker is, overridable for a run that published a
// different port.
func fixtureAddress() string {
	if address := os.Getenv(brokerEnv); address != "" {
		return address
	}

	return defaultTestBroker
}

// testBroker finds the fixture to test against. It skips when nothing answers,
// so a developer without Docker still gets the rest of the suite, and it fails
// when something answers that is not the fixture: a broker that is not this one
// accepts every publish the suite makes and the run goes green against
// somebody else's data. See fixture_test.go.
func testBroker(t *testing.T) string {
	t.Helper()

	address := fixtureAddress()

	socket, err := net.DialTimeout("tcp", address, 500*time.Millisecond)
	if err != nil {
		t.Skipf("no AMQP broker at %s, set %s to point at one: %v", address, brokerEnv, err)
	}

	require.NoError(t, socket.Close())

	declared, err := declaredVersion()
	require.NoError(t, err)

	properties, err := serverProperties()
	if err != nil {
		t.Fatal(foreignBroker(address, "answers nothing an AMQP client can read: "+err.Error(), declared))
	}

	require.NoError(t, checkBrokerIsOurs(address, properties, declared))

	return address
}

func connected(t *testing.T, address string, extra map[string]any) *Processor {
	t.Helper()

	config := map[string]any{"url": "amqp://guest:guest@" + address + "/"}
	maps.Copy(config, extra)

	built, err := New(eventsapi.ProviderSpec{Type: Type, Config: config})
	require.NoError(t, err)

	p, ok := built.(*Processor)
	require.True(t, ok)

	t.Cleanup(func() { _ = p.Close() })

	deadline := time.Now().Add(10 * time.Second)
	for !p.client.connected() {
		if time.Now().After(deadline) {
			t.Fatal("the adapter never connected to the broker")
		}

		time.Sleep(20 * time.Millisecond)
	}

	return p
}

// The published suite every event sink is held to, ours included. The broker is
// a black hole rather than RabbitMQ: every clause of the Processor contract is
// a property of what happens when the destination stops answering.
func TestSinkContract(t *testing.T) {
	address := eventsapitest.BlackHole(t)

	eventsapitest.RunProcessorContract(t, func(t *testing.T) eventsapi.Processor {
		built, err := New(eventsapi.ProviderSpec{
			Type: Type,
			Config: map[string]any{
				"url":             "amqp://" + address,
				"connect_timeout": "1s",
				"heartbeat":       "1s",
			},
		})
		require.NoError(t, err)

		return built
	})
}

// Every domain fact reaches the broker, on the routing key the reference
// documents and with the body a consumer parses. This is the case that would
// catch a rewrite changing what a bound queue receives.
//
// It is also the broker-side half of the reference gate: the keys a bound queue
// received are compared against the published reference as well as against the
// written-out list, so the encoder between the queue and the socket is in the
// chain. TestTheReferenceListsTheKeysTheWireCarries makes the same comparison
// without a broker.
func TestEveryEventReachesTheBroker(t *testing.T) {
	address := testBroker(t)

	p := connected(t, address, nil)
	watcher := consume(t, address, defaultExchange, "#")

	offerOneOfEach(p)

	seen := watcher.drain(t, settle)
	require.Len(t, seen, len(publishedKeys))

	keys := make([]string, 0, len(seen))
	for _, one := range seen {
		keys = append(keys, one.key)
		assert.Equal(t, defaultExchange, one.exchange)
	}

	assert.Equal(t, publishedKeys, keys, "the broker must see the events in the order they were offered")

	assert.Equal(t, publishedKeys, referenceAddresses(t), "the addresses the published reference lists")

	var first struct {
		Kind string `json:"kind"`
		Data struct {
			ID    string `json:"ID"`
			State string `json:"State"`
		} `json:"data"`
	}

	require.NoError(t, json.Unmarshal([]byte(seen[0].body), &first))
	assert.Equal(t, "printer.state_changed", first.Kind)
	assert.Equal(t, "printer-1", first.Data.ID)
	assert.Equal(t, "PRINTING", first.Data.State)

	t.Logf("routing keys through the broker: %v", keys)
	t.Logf("first body: %s", seen[0].body)
}

// The broker confirms what it took. Without publisher confirms a publish is a
// write to a socket, and an operator whose exchange is misconfigured sees a
// clean shutdown and no events.
func TestTheBrokerConfirmsWhatItTook(t *testing.T) {
	address := testBroker(t)

	p := connected(t, address, nil)

	for range 10 {
		p.PrinterStateChanged(eventsapi.PrinterState{ID: "printer-1", State: "IDLE"})
	}

	deadline := time.Now().Add(settle)

	var confirmed, refused uint64

	for time.Now().Before(deadline) {
		confirmed, refused = p.client.counts()
		if confirmed >= 10 {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	assert.GreaterOrEqual(t, confirmed, uint64(10), "the broker confirmed fewer messages than were published")
	assert.Zero(t, refused)

	t.Logf("broker confirmed %d, refused %d", confirmed, refused)
}

// A topic exchange is what makes a routing key worth having: a consumer bound
// to printer.# gets printer events and nothing else.
func TestABindingSelectsByRoutingKey(t *testing.T) {
	address := testBroker(t)

	p := connected(t, address, nil)
	printers := consume(t, address, defaultExchange, "printer.#")

	p.PrinterStateChanged(eventsapi.PrinterState{ID: "printer-1", State: "IDLE"})
	p.JobUpdated(eventsapi.JobInfo{ID: "job-1", PrinterID: "printer-1", State: "PRINTING"})
	p.LabelDeleted("label-1")

	seen := printers.drain(t, settle)
	require.Len(t, seen, 1)
	assert.Equal(t, "printer.state_changed", seen[0].key)
}

// The prefix is for a broker shared with other publishers, so it has to reach
// the routing key rather than only the documentation.
func TestTheRoutingKeyPrefixReachesTheBroker(t *testing.T) {
	address := testBroker(t)

	p := connected(t, address, map[string]any{"routing_key_prefix": "shop-a."})
	watcher := consume(t, address, defaultExchange, "shop-a.#")

	p.PrinterDeleted("printer-9")

	seen := watcher.drain(t, settle)
	require.Len(t, seen, 1)
	assert.Equal(t, "shop-a.printer.deleted", seen[0].key)
}

// An exchange that is not there is the operator's mistake, and the broker's own
// reason for refusing has to reach them rather than a closed socket.
func TestAMissingExchangeIsReportedWithTheBrokerReason(t *testing.T) {
	address := testBroker(t)

	built, err := New(eventsapi.ProviderSpec{
		Type: Type,
		Config: map[string]any{
			"url":              "amqp://guest:guest@" + address + "/",
			"exchange":         "no-such-exchange-" + time.Now().Format("150405.000000"),
			"declare_exchange": false,
			"connect_timeout":  "3s",
		},
	})
	require.NoError(t, err)

	p, ok := built.(*Processor)
	require.True(t, ok)

	defer func() { _ = p.Close() }()

	// The connection comes up, and the refusal arrives when the first publish
	// names an exchange the broker does not hold.
	deadline := time.Now().Add(10 * time.Second)
	for !p.client.connected() {
		require.False(t, time.Now().After(deadline), "the adapter never connected")
		time.Sleep(20 * time.Millisecond)
	}

	p.PrinterDeleted("printer-1")

	refused := uint64(0)

	for deadline = time.Now().Add(settle); time.Now().Before(deadline); {
		if _, refused = p.client.counts(); refused > 0 {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	assert.Positive(t, refused, "publishing to an exchange the broker does not hold was not counted")
}

func TestASpecWithNoBrokerIsRejected(t *testing.T) {
	t.Parallel()

	_, err := New(eventsapi.ProviderSpec{Type: Type, Config: map[string]any{}})
	require.ErrorIs(t, err, ErrMissingURL)
}

func TestConfigurationIsRejectedBeforeAnythingStarts(t *testing.T) {
	t.Parallel()

	for name, config := range map[string]map[string]any{
		"unknown scheme":    {"url": "kafka://broker:9092"},
		"no host":           {"url": "amqp://"},
		"unusable depth":    {"url": "amqp://broker", "queue_depth": 0},
		"unreadable beat":   {"url": "amqp://broker", "heartbeat": "half a minute"},
		"beat below a tick": {"url": "amqp://broker", "heartbeat": "100ms"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := New(eventsapi.ProviderSpec{Type: Type, Config: config})
			require.Error(t, err)
		})
	}
}

// The URI form RabbitMQ's own documentation uses has to parse: credentials with
// characters the grammar escapes, a port, and a vhost.
func TestTheBrokerURIIsReadTheWayRabbitMQWritesIt(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		url      string
		address  string
		vhost    string
		user     string
		password string
		secure   bool
	}{
		"bare host": {
			url: "broker", address: "broker:5672", vhost: "/", user: "guest", password: "guest",
		},
		"credentials and vhost": {
			url:     "amqp://shop:s3cret@broker:5673/telemetry",
			address: "broker:5673", vhost: "telemetry", user: "shop", password: "s3cret",
		},
		"escaped default vhost": {
			url:     "amqp://guest:guest@broker/%2F",
			address: "broker:5672", vhost: "/", user: "guest", password: "guest",
		},
		"escaped password": {
			url:     "amqp://shop:p%40ss@broker/",
			address: "broker:5672", vhost: "/", user: "shop", password: "p@ss",
		},
		"tls": {
			url: "amqps://broker", address: "broker:5671", vhost: "/",
			user: "guest", password: "guest", secure: true,
		},
	}

	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			address, vhost, creds, secure, err := brokerAddress(want.url)
			require.NoError(t, err)
			assert.Equal(t, want.address, address)
			assert.Equal(t, want.vhost, vhost)
			assert.Equal(t, want.user, creds.username)
			assert.Equal(t, want.password, creds.password)
			assert.Equal(t, want.secure, secure)
		})
	}
}
