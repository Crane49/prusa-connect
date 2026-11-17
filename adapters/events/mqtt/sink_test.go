package mqtt

import (
	"bufio"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// The sink properties, which hold whether or not a broker exists. E13 and O7:
// a dead broker must cost nanoseconds, not a timeout per event, and dropping is
// the correct degradation for a sink.

var _ eventsapi.Processor = (*Processor)(nil)

func TestTheFactoryIsRegisteredUnderItsType(t *testing.T) {
	t.Parallel()

	// The whole extension mechanism is an init() calling Register. If this
	// fails, the adapter is invisible to configuration no matter how well the
	// rest of it works.
	for _, descriptor := range eventsapi.Descriptors() {
		if descriptor.Name() == Type {
			return
		}
	}

	t.Fatalf("%q is not in the gathering list this build drains", Type)
}

func TestASpecWithNoBrokerIsRejected(t *testing.T) {
	t.Parallel()

	_, err := New(eventsapi.ProviderSpec{Type: Type, Config: map[string]any{}})
	require.ErrorIs(t, err, ErrMissingURL)
}

func TestConfigurationIsRejectedBeforeAnythingStarts(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		config  map[string]any
		wantMsg string
	}{
		"unusable queue depth": {
			config:  map[string]any{"url": "tcp://127.0.0.1:1883", "queue_depth": 0},
			wantMsg: "queue_depth must be at least 1",
		},
		"unparseable duration": {
			config:  map[string]any{"url": "tcp://127.0.0.1:1883", "keepalive": "half an hour"},
			wantMsg: "config.keepalive",
		},
		"unknown default action": {
			config:  map[string]any{"url": "tcp://127.0.0.1:1883", "default_action": "sideways"},
			wantMsg: "default_action",
		},
		"routes that are not a mapping": {
			config:  map[string]any{"url": "tcp://127.0.0.1:1883", "routes": []any{"v1/#"}},
			wantMsg: "routes[0]",
		},
		"unsupported scheme": {
			config:  map[string]any{"url": "ws://127.0.0.1:9001/mqtt"},
			wantMsg: "scheme",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := New(eventsapi.ProviderSpec{Type: Type, Config: tc.config})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

func TestQueueDepthIsReadFromEitherNumberKind(t *testing.T) {
	t.Parallel()

	// YAML decodes a number as int or float64 depending on the decoder, and an
	// operator should not have to know which.
	for name, depth := range map[string]any{"int": 4, "float64": float64(4)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p, err := New(eventsapi.ProviderSpec{
				Type:   Type,
				Config: map[string]any{"url": "tcp://" + closedAddress(t), "queue_depth": depth},
			})
			require.NoError(t, err)
			_ = p.Close()
		})
	}
}

func TestKeepaliveAcceptsSecondsOrADuration(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]any{"seconds": 45, "duration": "45s"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg, err := configFrom(map[string]any{"url": "tcp://host", "keepalive": value})
			require.NoError(t, err)
			assert.Equal(t, 45*time.Second, cfg.Keepalive)
		})
	}
}

// A broker drops the older connection when a second client connects with the
// same client id. So every sink the server builds without a configured id must
// get its own, even when several are built within one tick of the clock.
func TestEveryGeneratedClientIDIsUnique(t *testing.T) {
	t.Parallel()

	const sinks = 1000

	seen := make(map[string]bool, sinks)

	for range sinks {
		cfg, err := configFrom(map[string]any{"url": "tcp://host"})
		require.NoError(t, err)
		require.Regexp(t, `^prusa-server-[0-9]+$`, cfg.ClientID)
		require.False(t, seen[cfg.ClientID], "two sinks got the client id %s", cfg.ClientID)

		seen[cfg.ClientID] = true
	}
}

// TestADeadBrokerNeverBlocksTheCaller is the property the whole queue exists
// for. The caller here is the event bus, and an event bus that waits on a TCP
// timeout has stopped being one.
func TestADeadBrokerNeverBlocksTheCaller(t *testing.T) {
	t.Parallel()

	p, err := New(eventsapi.ProviderSpec{
		Type:   Type,
		Config: map[string]any{"url": "tcp://" + closedAddress(t), "queue_depth": 1},
	})
	require.NoError(t, err)

	const events = 10000

	start := time.Now()

	for i := range events {
		p.PrinterStateChanged(eventsapi.PrinterState{ID: strconv.Itoa(i), State: "PRINTING"})
	}

	elapsed := time.Since(start)

	t.Logf("%d events offered against a dead broker in %v, %v each", events, elapsed, elapsed/events)
	assert.Less(t, elapsed, 2*time.Second, "offering events waited on the network")

	_ = p.Close()
}

// TestAStalledBrokerNeverBlocksTheCaller is the harder half: a broker that
// accepts the connection and then never acknowledges anything, so the sender
// is genuinely stuck rather than failing fast.
func TestAStalledBrokerNeverBlocksTheCaller(t *testing.T) {
	t.Parallel()

	address := stalledBroker(t)

	p, err := New(eventsapi.ProviderSpec{
		Type: Type,
		Config: map[string]any{
			"url": "tcp://" + address, "queue_depth": 1, "publish_timeout": "200ms",
		},
	})
	require.NoError(t, err)

	done := make(chan time.Duration)

	go func() {
		start := time.Now()

		for i := range 500 {
			p.PrinterStateChanged(eventsapi.PrinterState{ID: strconv.Itoa(i), State: "PRINTING"})
		}

		done <- time.Since(start)
	}()

	select {
	case elapsed := <-done:
		t.Logf("500 events offered against a stalled broker in %v", elapsed)
	case <-time.After(5 * time.Second):
		t.Fatal("offering events blocked on a stalled broker")
	}

	_ = p.Close()
}

// TestCloseReportsWhatNeverArrived is the other half of dropping: an operator
// has to be able to find out that it happened.
func TestCloseReportsWhatNeverArrived(t *testing.T) {
	t.Parallel()

	address := stalledBroker(t)

	p, err := New(eventsapi.ProviderSpec{
		Type: Type,
		Config: map[string]any{
			"url": "tcp://" + address, "queue_depth": 1, "publish_timeout": "200ms",
		},
	})
	require.NoError(t, err)

	for i := range 200 {
		p.PrinterStateChanged(eventsapi.PrinterState{ID: strconv.Itoa(i), State: "PRINTING"})
	}

	err = p.Close()
	require.Error(t, err, "dropping events silently is the failure this reports")
	assert.Contains(t, err.Error(), "dropped")

	t.Logf("Close reported: %v", err)
}

func TestCloseIsSafeToCallTwice(t *testing.T) {
	t.Parallel()

	p, err := New(eventsapi.ProviderSpec{Type: Type, Config: map[string]any{"url": "tcp://" + closedAddress(t)}})
	require.NoError(t, err)

	_ = p.Close()

	// A host shutting down after an error can reach Close on a path that
	// already ran. Panicking there turns one failure into two.
	assert.NotPanics(t, func() { _ = p.Close() })
}

func TestOfferingAfterCloseDoesNotPanic(t *testing.T) {
	t.Parallel()

	p, err := New(eventsapi.ProviderSpec{Type: Type, Config: map[string]any{"url": "tcp://" + closedAddress(t)}})
	require.NoError(t, err)
	_ = p.Close()

	assert.NotPanics(t, func() {
		p.PrinterStateChanged(eventsapi.PrinterState{ID: "one", State: "IDLE"})
		p.JobUpdated(eventsapi.JobInfo{PrinterID: "one", ExternalID: 1, State: "PRINTING"})
		p.PrinterDeleted("one")
		p.PrinterLabelsChanged(eventsapi.PrinterLabels{ID: "one"})
		p.LabelDeleted("label")
	})
}

// closedAddress returns an address nothing is listening on, which is what a
// broker that is down looks like.
func closedAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	address := listener.Addr().String()
	require.NoError(t, listener.Close())

	return address
}

// stalledBroker accepts a connection, answers CONNECT, and then says nothing
// ever again. It is not a broker under test: it exists to hold the sender still
// so the queue fills, which is the only way to observe a drop.
func stalledBroker(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			socket, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			go func() {
				defer func() { _ = socket.Close() }()

				reader := bufio.NewReader(socket)

				kind, _, _, readErr := readPacket(reader)
				if readErr != nil || kind != connectPacket {
					return
				}

				if _, writeErr := socket.Write(encodePacket(connackPacket, 0, []byte{0, 0})); writeErr != nil {
					return
				}

				// Read and discard for as long as the client keeps writing,
				// answering nothing.
				_, _ = reader.WriteTo(discard{})
			}()
		}
	}()

	return listener.Addr().String()
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// TestRoutingWarningsReachTheHost is the only way the startup warning half of
// the routing contract can be surfaced: sdk/eventsapi hands a processor no
// logger, so a host reads them off the built processor instead.
func TestRoutingWarningsReachTheHost(t *testing.T) {
	t.Parallel()

	built, err := New(eventsapi.ProviderSpec{Type: Type, Config: map[string]any{
		"url": "tcp://" + closedAddress(t),
		"routes": []any{
			map[string]any{"from": "v1/devices/printers/+/data/#", "action": "drop"},
			map[string]any{"from": "v1/devices/printers/{id}/data/state", "to": "printers/{id}/state"},
			map[string]any{"from": "v1/#", "to": "flattened"},
		},
	}})
	require.NoError(t, err)

	p, ok := built.(*Processor)
	require.True(t, ok)

	t.Cleanup(func() { _ = p.Close() })

	warnings := p.Warnings()
	require.Len(t, warnings, 2)
	assert.Contains(t, warnings[0], "cannot be combined with #")
	assert.Contains(t, warnings[1], "may be unreachable")

	t.Logf("warnings: %v", warnings)
}
