package e2e

import (
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	mqttclient "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/require"
)

// The MQTT half of the durable command record, driven against a real broker.
//
// ADR-20260901-atx rules 12 and 13 name two transports for the audit frame, the live
// stream and the history read, and both are covered elsewhere in this package
// and in internal/. An MQTT sink is a third path out of this server, and the
// question a live test has to answer is not whether the gate is written there
// but whether anything reaches it at all.
//
// It did not, and now it does. A sink was fed through sdk/eventsapi, which
// carried five domain facts and no envelope, and internal/app's sinkFeed
// forwarded one event type, printer.telemetry.sampled at /state: no command
// frame had a fact that meant it, so the topic namespace carried none. The
// contract now carries a generic fact, and the command lifecycle reaches a sink
// like every other durable frame.
//
// What this case holds is where it arrives. The v1/devices/printers subtree is
// frozen contract that existing subscribers read today, so the facts go under a root of their own and that subtree stays exactly as wide as
// it shipped.
//
// The case is written the way adapters/events/mqtt's own broker cases are: it
// skips when nothing answers on the fixture address, because a developer with
// no Docker still gets the rest of the suite.
//
//	docker run -d -p 51883:1883 -v "$PWD/mosquitto.conf:/mosquitto/config/mosquitto.conf:ro" eclipse-mosquitto:2
//
// with `listener 1883`, `allow_anonymous true` and `persistence false`.
const (
	brokerEnv         = "PCL_MQTT_TEST_BROKER"
	defaultTestBroker = "127.0.0.1:51883"
)

// brokerAddress finds the fixture, or skips.
func brokerAddress(t *testing.T) string {
	t.Helper()

	address := os.Getenv(brokerEnv)
	if address == "" {
		address = defaultTestBroker
	}

	socket, err := net.DialTimeout("tcp", address, 500*time.Millisecond)
	if err != nil {
		t.Skipf("no MQTT broker at %s, set %s to point at one: %v", address, brokerEnv, err)
	}

	_ = socket.Close()

	return address
}

// mqttResources declares one sink pointed at the fixture broker.
func mqttResources(address string) string {
	return strings.Join([]string{
		"  sink:",
		"    bus:",
		"      type: mqtt",
		"      enabled: true",
		"      config:",
		"        url: tcp://" + address,
	}, "\n")
}

// TestACommandLifecycleReachesTheBrokerOutsideTheFrozenNamespace is the third
// path, measured rather than assumed.
func TestACommandLifecycleReachesTheBrokerOutsideTheFrozenNamespace(t *testing.T) {
	t.Parallel()

	address := brokerAddress(t)

	server := startServerWith(t, mqttResources(address))
	server.awaitReady(t)

	_, listed := server.attachPrinter(t, "MK4")

	// A sink is a principal of its own type and holds exactly what it was
	// granted. Without this it subscribes to nothing and the case would pass
	// for the wrong reason.
	grant := server.do(t, http.MethodPost, "/api/v1/grants/device/"+listed.ID,
		`{"relation":"manager","subject":"sink:bus"}`, nil)
	if grant.status != http.StatusOK && grant.status != http.StatusCreated {
		t.Fatalf("granting the sink answered %d: %s", grant.status, grant.body)
	}

	seen := make(chan string, 256)

	options := mqttclient.NewClientOptions().
		AddBroker("tcp://" + address).
		SetClientID("e2e-command-watcher").
		SetConnectTimeout(5 * time.Second)

	client := mqttclient.NewClient(options)

	if token := client.Connect(); !token.WaitTimeout(5*time.Second) || token.Error() != nil {
		t.Fatalf("connect to the broker: %v", token.Error())
	}

	defer client.Disconnect(250)

	// Retained messages are skipped. The state topics are retained and the
	// printer ids are derived from fixed serials, so a broker that has served
	// this case before hands back this printer's last state the moment the
	// subscription opens: an aliveness gate that counted it would pass before
	// this run's sink had attached, and the case would then measure the
	// start-up order rather than the forwarding.
	token := client.Subscribe("#", 1, func(_ mqttclient.Client, message mqttclient.Message) {
		if message.Retained() {
			return
		}

		select {
		case seen <- message.Topic():
		default:
		}
	})
	if !token.WaitTimeout(5*time.Second) || token.Error() != nil {
		t.Fatalf("subscribe to the broker: %v", token.Error())
	}

	// The sink is a supervised instance and attaches a moment after the
	// listener binds, so the commands wait for a topic naming this printer.
	// Without the wait they are published before anything is subscribed and the
	// case measures the start-up order rather than the forwarding.
	// Three times the package's own budget, because this wait spans a
	// supervised sink attaching, a broker round trip and a printer's telemetry
	// tick, on a machine that is running the rest of the suite beside it. At
	// settleBudget it timed out under load while the same case passed alone.
	deadline := time.After(3 * settleBudget)
	topics := map[string]int{}

	for alive := false; !alive; {
		select {
		case topic := <-seen:
			topics[topic]++

			if strings.Contains(topic, listed.ID) && strings.HasSuffix(topic, "/data/state") {
				alive = true
			}
		case <-deadline:
			t.Fatalf("no state topic named %s, so nothing proves the sink was connected: %v",
				listed.ID, topics)
		}
	}

	// Three commands, so the run publishes requested, authorized and settled
	// frames more than once.
	for range 3 {
		answer := server.do(t, http.MethodPost, "/api/v1/devices/"+listed.ID+"/commands",
			`{"verb":"`+sendInfo+`"}`, nil)
		if answer.status != http.StatusOK {
			t.Fatalf("POST a command answered %d: %s", answer.status, answer.body)
		}
	}

	// Drain whatever else the broker has for a moment, then read the whole set.
	drain := time.After(2 * time.Second)

	for draining := true; draining; {
		select {
		case topic := <-seen:
			topics[topic]++
		case <-drain:
			draining = false
		}
	}

	t.Logf("observed topics: %v", topics)

	lifecycle := 0

	for topic := range topics {
		// The frozen subtree is what an existing subscriber reads, and a command arriving inside it would be traffic
		// nobody asked for on a namespace whose whole value is that it does not
		// change.
		if strings.HasPrefix(topic, "v1/devices/printers/") &&
			strings.Contains(topic, "command") {
			t.Errorf("a command topic reached the frozen namespace: %s", topic)
		}

		if strings.HasPrefix(topic, "v1/events/") &&
			strings.Contains(topic, "device.command.") {
			lifecycle++
		}
	}

	require.Positive(t, lifecycle,
		"no command lifecycle fact reached the broker, so the sink is still fed one event type")

	t.Logf("observed: %d command lifecycle topics arrived under v1/events/", lifecycle)
}
