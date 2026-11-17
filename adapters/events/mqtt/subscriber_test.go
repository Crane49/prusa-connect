package mqtt

import (
	"bufio"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The adapter publishes and never subscribes, so the subscriber that observes
// it lives here. It speaks the same wire format from the other side, which is
// what makes a broker in the middle a real test: the broker parses what the
// adapter wrote and re-encodes it, and a mistake in either direction shows up
// as a message that does not arrive or does not match.

const (
	subscribePacket byte = 8
	subackPacket    byte = 9
)

// observed is one message as a subscriber saw it, including the two flags the
// frozen contract is mostly about.
type observed struct {
	topic   string
	payload string
	qos     byte
	retain  bool
}

type subscriber struct {
	socket net.Conn
	reader *bufio.Reader

	mu       sync.Mutex
	messages []observed

	closeOnce sync.Once
}

// subscribeAt connects a subscriber and subscribes at QoS 2, so a delivered
// message carries the QoS it was published at rather than one capped by the
// subscription.
func subscribeAt(t *testing.T, address, filter string) *subscriber {
	t.Helper()

	return subscribeWithCredentials(t, address, filter, "", "")
}

// subscribeWithCredentials is subscribeAt against a broker that refuses
// anonymous connections.
func subscribeWithCredentials(t *testing.T, address, filter, username, password string) *subscriber {
	t.Helper()

	socket, err := net.DialTimeout("tcp", address, 5*time.Second)
	require.NoError(t, err)

	s := &subscriber{socket: socket, reader: bufio.NewReader(socket)}
	t.Cleanup(s.close)

	clientID := uniqueName("observer-")

	connect, err := encodeConnect(clientID, 0, credentials{username: username, password: password}, will{})
	require.NoError(t, err)
	s.write(t, connect)

	kind, _, body, err := readPacket(s.reader)
	require.NoError(t, err)
	require.Equal(t, connackPacket, kind)
	require.NoError(t, connackError(body))

	s.write(t, encodeSubscribe(1, filter))

	kind, _, _, err = readPacket(s.reader)
	require.NoError(t, err)
	require.Equal(t, subackPacket, kind)

	go s.receive()

	return s
}

func encodeSubscribe(packetID uint16, filter string) []byte {
	body := binary.BigEndian.AppendUint16(nil, packetID)
	body, _ = appendString(body, filter)
	body = append(body, 2)

	return encodePacket(subscribePacket, 0x02, body)
}

func (s *subscriber) write(t *testing.T, packet []byte) {
	t.Helper()

	_, err := s.socket.Write(packet)
	require.NoError(t, err)
}

// receive reads until the socket closes, acknowledging whatever the QoS
// requires so the broker does not redeliver.
func (s *subscriber) receive() {
	for {
		kind, flags, body, err := readPacket(s.reader)
		if err != nil {
			return
		}

		switch kind {
		case publishPacket:
			s.record(flags, body)
		case pubrelPacket:
			id, idErr := packetIDFrom(body)
			if idErr != nil {
				return
			}

			if _, err = s.socket.Write(encodePacketID(pubcompPacket, id)); err != nil {
				return
			}
		default:
		}
	}
}

func (s *subscriber) record(flags byte, body []byte) {
	qos := (flags >> 1) & 0x03

	if len(body) < 2 {
		return
	}

	topicLength := int(binary.BigEndian.Uint16(body))
	if len(body) < 2+topicLength {
		return
	}

	message := observed{
		topic:  string(body[2 : 2+topicLength]),
		qos:    qos,
		retain: flags&0x01 == 1,
	}

	rest := body[2+topicLength:]

	if qos > 0 {
		if len(rest) < 2 {
			return
		}

		id := binary.BigEndian.Uint16(rest)
		rest = rest[2:]

		ack := pubackPacket
		if qos == 2 {
			ack = pubrecPacket
		}

		if _, err := s.socket.Write(encodePacketID(ack, id)); err != nil {
			return
		}
	}

	message.payload = string(rest)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, message)
}

// await blocks until at least count messages have arrived, and fails the test
// rather than returning a short slice.
func (s *subscriber) await(t *testing.T, count int) []observed {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		if got := s.seen(); len(got) >= count {
			return got
		}

		time.Sleep(20 * time.Millisecond)
	}

	got := s.seen()
	t.Fatalf("waited for %d message(s), saw %d: %v", count, len(got), got)

	return nil
}

// awaitPayloadAfter blocks until a message with this payload arrives on this
// topic at a position later than after, and returns where it landed. It exists
// because a retained value is delivered the moment a subscriber connects, so
// counting messages is not enough to say which publish caused which arrival.
func (s *subscriber) awaitPayloadAfter(t *testing.T, topic, payload string, after int) int {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)

	for time.Now().Before(deadline) {
		for i, message := range s.seen() {
			if i > after && message.topic == topic && message.payload == payload {
				return i
			}
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("waited for %q on %s after position %d, saw %v", payload, topic, after, s.seen())

	return 0
}

// settle waits for the broker to have had time to deliver anything it was
// going to, which is how an absence is asserted rather than assumed.
func (s *subscriber) settle() {
	time.Sleep(750 * time.Millisecond)
}

func (s *subscriber) seen() []observed {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]observed(nil), s.messages...)
}

func (s *subscriber) close() {
	s.closeOnce.Do(func() { _ = s.socket.Close() })
}

// on returns the messages that arrived on one topic, in order.
func (s *subscriber) on(topic string) []observed {
	var out []observed

	for _, message := range s.seen() {
		if message.topic == topic {
			out = append(out, message)
		}
	}

	return out
}
