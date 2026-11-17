package kafka

import (
	"encoding/json"
	"maps"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi/eventsapitest"
)

// These run against a real cluster. A producer verified against a fake proves
// the fake: the record batch's CRC-32C, the varint record encoding and the
// produce version are all things a broker validates and rejects, and an encoder
// that this repository's own decoder reads back is not evidence that Kafka
// accepts any of it.
//
//	docker compose -f docker-compose.local.yml up -d redpanda-fixture
//
// The fixture is docker/redpanda-fixture, part of the local stack, so a
// developer who has brought the dev environment up already has it. Redpanda
// speaks the Kafka wire, which is the only thing under test. Point
// PCL_KAFKA_TEST_BROKER at another broker to override, or leave it unset and
// the fixture's own port is used.
const brokerEnv = "PCL_KAFKA_TEST_BROKER"

const defaultTestBroker = "127.0.0.1:59092"

// settle is how long a case waits for records the cluster has already
// acknowledged. Acknowledged and readable are two moments, and the second is
// the one a consumer sees.
const settle = 10 * time.Second

// fixtureAddress is where the cluster is, overridable for a run that published
// a different port.
func fixtureAddress() string {
	if address := os.Getenv(brokerEnv); address != "" {
		return address
	}

	return defaultTestBroker
}

// testBroker finds the fixture to test against. It skips when nothing answers,
// so a developer without Docker still gets the rest of the suite, and it fails
// when something answers that is not the fixture: a cluster that is not this
// one accepts every record the suite produces and the run goes green against
// somebody else's data. See fixture_test.go.
func testBroker(t *testing.T) string {
	t.Helper()

	address := fixtureAddress()

	socket, err := net.DialTimeout("tcp", address, 500*time.Millisecond)
	if err != nil {
		t.Skipf("no Kafka cluster at %s, set %s to point at one: %v", address, brokerEnv, err)
	}

	require.NoError(t, socket.Close())

	declared, err := declaredCluster()
	require.NoError(t, err)

	described, err := describedCluster()
	if err != nil {
		t.Fatal(foreignCluster(address, "answers nothing a Kafka client can read: "+err.Error(), declared))
	}

	require.NoError(t, checkClusterIsOurs(address, described, declared))

	return address
}

// uniqueTopic keeps one case's records out of another's, because a cluster
// remembers them and the suite runs in a random order.
func uniqueTopic(t *testing.T) string {
	t.Helper()

	return "pcl-test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

func producing(t *testing.T, address, topic string, extra map[string]any) *Processor {
	t.Helper()

	config := map[string]any{
		"brokers":      []any{address},
		"topic":        topic,
		"create_topic": true,
	}
	maps.Copy(config, extra)

	built, err := New(eventsapi.ProviderSpec{Type: Type, Config: config})
	require.NoError(t, err)

	p, ok := built.(*Processor)
	require.True(t, ok)

	t.Cleanup(func() { _ = p.Close() })

	return p
}

// partitionsOf waits for the cluster to know about the topic and reports what
// it holds, which is also what proves the adapter's own metadata request works.
func partitionsOf(t *testing.T, p *Processor) []int32 {
	t.Helper()

	deadline := time.Now().Add(settle)

	for {
		if err := p.client.refresh(); err == nil {
			p.client.mu.Lock()
			defer p.client.mu.Unlock()

			return p.client.order
		}

		if time.Now().After(deadline) {
			t.Fatal("the cluster never reported a partition for the topic")
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// The published suite every event sink is held to, ours included. The cluster is
// a black hole rather than Redpanda: every clause of the Processor contract is
// a property of what happens when the destination stops answering.
func TestSinkContract(t *testing.T) {
	address := eventsapitest.BlackHole(t)

	eventsapitest.RunProcessorContract(t, func(t *testing.T) eventsapi.Processor {
		built, err := New(eventsapi.ProviderSpec{
			Type: Type,
			Config: map[string]any{
				"brokers":         []any{address},
				"connect_timeout": "1s",
			},
		})
		require.NoError(t, err)

		return built
	})
}

// Every domain fact reaches the cluster, keyed by the printer it is about and
// carrying the body a consumer parses. This is the case that would catch a
// rewrite changing what a topic holds.
//
// It is also the cluster-side half of the reference gate: the kinds the cluster
// stored are compared against the published reference as well as against the
// written-out list, so the record batch encoder is in the chain.
// TestTheReferenceListsTheKindsTheWireCarries makes the same comparison without
// a cluster. The comparison is order-insensitive because the cluster spreads
// records over partitions by key; ordering within one printer is
// TestOnePrintersEventsShareOnePartition's property.
func TestEveryEventReachesTheCluster(t *testing.T) {
	address := testBroker(t)
	topic := uniqueTopic(t)

	p := producing(t, address, topic, nil)
	partitions := partitionsOf(t, p)

	offerOneOfEach(p)

	seen := fetchRecords(t, address, topic, partitions, len(publishedKinds))

	kinds := make([]string, 0, len(seen))
	for _, one := range seen {
		kinds = append(kinds, kindOf(t, one.value))
	}

	assert.ElementsMatch(t, publishedKinds, kinds)

	assert.ElementsMatch(t, publishedKinds, referenceAddresses(t),
		"the addresses the published reference lists")

	t.Logf("records the cluster stored: %d", len(seen))

	for _, one := range seen {
		t.Logf("  partition %d key %q value %s", one.partition, one.key, one.value)
	}
}

// A printer's events are keyed by its id, so they land on one partition and a
// consumer reads that printer's history in order. A producer that keyed on
// nothing spreads one printer over the topic and loses the ordering.
func TestOnePrintersEventsShareOnePartition(t *testing.T) {
	address := testBroker(t)
	topic := uniqueTopic(t)

	p := producing(t, address, topic, nil)
	partitions := partitionsOf(t, p)

	for _, state := range []string{"IDLE", "PRINTING", "PAUSED", "FINISHED", "IDLE"} {
		p.PrinterStateChanged(eventsapi.PrinterState{ID: "printer-7", State: state})
	}

	seen := fetchRecords(t, address, topic, partitions, 5)

	states := make([]string, 0, len(seen))

	for _, one := range seen {
		assert.Equal(t, "printer-7", one.key)
		assert.Equal(t, seen[0].partition, one.partition, "one printer's events must share one partition")

		var body struct {
			Data struct {
				State string `json:"State"`
			} `json:"data"`
		}

		require.NoError(t, json.Unmarshal([]byte(one.value), &body))
		states = append(states, body.Data.State)
	}

	assert.Equal(t, []string{"IDLE", "PRINTING", "PAUSED", "FINISHED", "IDLE"}, states,
		"the partition must hold the states in the order they were offered")
}

// A topic the cluster does not hold and will not create is the operator's
// mistake, and it has to be counted rather than reported as a clean shutdown.
func TestAMissingTopicIsCounted(t *testing.T) {
	address := testBroker(t)

	built, err := New(eventsapi.ProviderSpec{
		Type: Type,
		Config: map[string]any{
			"brokers":         []any{address},
			"topic":           uniqueTopic(t),
			"create_topic":    false,
			"connect_timeout": "3s",
		},
	})
	require.NoError(t, err)

	p, ok := built.(*Processor)
	require.True(t, ok)

	p.PrinterDeleted("printer-1")

	err = p.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "produce attempt(s) failed")
	assert.False(t, p.client.connected())
}

func TestASpecWithNoBrokersIsRejected(t *testing.T) {
	t.Parallel()

	_, err := New(eventsapi.ProviderSpec{Type: Type, Config: map[string]any{}})
	require.ErrorIs(t, err, ErrMissingBrokers)
}

func TestConfigurationIsRejectedBeforeAnythingStarts(t *testing.T) {
	t.Parallel()

	for name, config := range map[string]map[string]any{
		"blank broker list": {"brokers": "  , "},
		"unusable acks":     {"brokers": "broker:9092", "acks": 3},
		"unusable depth":    {"brokers": "broker:9092", "queue_depth": 0},
		"unreadable wait":   {"brokers": "broker:9092", "connect_timeout": "ten seconds"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := New(eventsapi.ProviderSpec{Type: Type, Config: config})
			require.Error(t, err)
		})
	}
}

// The bootstrap list is written as a sequence or as one comma-separated string,
// and a broker with no port gets the one Kafka listens on.
func TestTheBrokerListIsReadEitherWayItIsWritten(t *testing.T) {
	t.Parallel()

	for name, written := range map[string]any{
		"sequence": []any{"a:9092", "b"},
		"string":   "a:9092, b",
		"strings":  []string{"a:9092", "b"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, []string{"a:9092", "b:9092"}, brokersFrom(written))
		})
	}
}

// The partition a key lands on is stable, because a key that moved between
// calls would spread one printer over the topic.
func TestAKeyAlwaysPicksTheSamePartition(t *testing.T) {
	t.Parallel()

	partitions := []int32{0, 1, 2, 3, 4, 5}

	first := partitionFor([]byte("printer-7"), partitions)
	for range 100 {
		assert.Equal(t, first, partitionFor([]byte("printer-7"), partitions))
	}

	assert.NotEqual(t, first, partitionFor([]byte("printer-7"), []int32{first + 1}),
		"the partition has to come from the set it was given")
	assert.Equal(t, int32(0), partitionFor([]byte("printer-7"), nil))
}

func fetchRecords(t *testing.T, address, topic string, partitions []int32, want int) []consumed {
	t.Helper()

	deadline := time.Now().Add(settle)

	for {
		seen := fetchAll(t, address, topic, partitions, time.Second)
		if len(seen) >= want {
			return seen
		}

		if time.Now().After(deadline) {
			require.Len(t, seen, want, "the cluster stored fewer records than were produced")

			return seen
		}

		time.Sleep(200 * time.Millisecond)
	}
}

func kindOf(t *testing.T, value string) string {
	t.Helper()

	var body struct {
		Kind string `json:"kind"`
	}

	require.NoError(t, json.Unmarshal([]byte(value), &body))

	return body.Kind
}
