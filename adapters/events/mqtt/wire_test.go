package mqtt

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The expected bytes are read off the MQTT 3.1.1 specification rather than
// produced by this package, so a change to the encoder that keeps the round
// trip working still fails here. A broker is the other half of the proof and
// lives in broker_test.go.

func TestConnectEncodesTheSpecifiedBytes(t *testing.T) {
	t.Parallel()

	packet, err := encodeConnect("c", 30, credentials{}, will{})
	require.NoError(t, err)

	assert.Equal(t, []byte{
		0x10, 0x0D, // CONNECT, remaining length 13
		0x00, 0x04, 'M', 'Q', 'T', 'T', // protocol name
		0x04,       // protocol level 4, which is 3.1.1
		0x02,       // clean session
		0x00, 0x1E, // keepalive 30
		0x00, 0x01, 'c', // client id
	}, packet)
}

func TestConnectCarriesTheWillAndCredentials(t *testing.T) {
	t.Parallel()

	packet, err := encodeConnect("c", 0, credentials{username: "u", password: "p"},
		will{topic: "v1/server/online", payload: "0", qos: 1, retain: true})
	require.NoError(t, err)

	// username | password | will retain | will QoS 1 | will | clean session.
	assert.Equal(t, byte(0x80|0x40|0x20|0x08|0x04|0x02), packet[9], "connect flags")
	assert.Contains(t, string(packet), "v1/server/online")
	assert.Contains(t, string(packet), "u")
}

func TestPublishEncodesQoSAndRetainIntoTheHeader(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		qos      byte
		retain   bool
		packetID uint16
		want     []byte
	}{
		"qos 0 retained": {
			qos: 0, retain: true,
			want: []byte{0x31, 0x04, 0x00, 0x01, 'a', '1'},
		},
		"qos 1 not retained": {
			qos: 1, packetID: 7,
			want: []byte{0x32, 0x06, 0x00, 0x01, 'a', 0x00, 0x07, '1'},
		},
		"qos 2 retained": {
			qos: 2, retain: true, packetID: 7,
			want: []byte{0x35, 0x06, 0x00, 0x01, 'a', 0x00, 0x07, '1'},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			packet, err := encodePublish("a", "1", tc.qos, tc.retain, tc.packetID)
			require.NoError(t, err)
			assert.Equal(t, tc.want, packet)
		})
	}
}

func TestPubrelCarriesTheReservedFlags(t *testing.T) {
	t.Parallel()

	// A broker closes the connection on a PUBREL whose reserved bits are not
	// 0x02, so this is the difference between QoS 2 working and the session
	// dying halfway through it.
	assert.Equal(t, []byte{0x62, 0x02, 0x00, 0x07}, encodePacketID(pubrelPacket, 7))
	assert.Equal(t, []byte{0x40, 0x02, 0x00, 0x07}, encodePacketID(pubackPacket, 7))
}

func TestEmptyPacketsAreTwoBytes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []byte{0xC0, 0x00}, encodeEmpty(pingreqPacket))
	assert.Equal(t, []byte{0xE0, 0x00}, encodeEmpty(disconnectPacket))
}

func TestRemainingLengthUsesTheContinuationEncoding(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		length int
		want   []byte
	}{
		"one byte":    {length: 127, want: []byte{0x7F}},
		"two bytes":   {length: 128, want: []byte{0x80, 0x01}},
		"boundary":    {length: 16383, want: []byte{0xFF, 0x7F}},
		"three bytes": {length: 16384, want: []byte{0x80, 0x80, 0x01}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			encoded := appendVarint(nil, tc.length)
			assert.Equal(t, tc.want, encoded)

			decoded, err := readVarint(bufio.NewReader(bytes.NewReader(encoded)))
			require.NoError(t, err)
			assert.Equal(t, tc.length, decoded)
		})
	}
}

func TestReadPacketRefusesAnOversizedBody(t *testing.T) {
	t.Parallel()

	// A broker is not trusted to bound what this process allocates.
	oversized := append([]byte{0x30}, appendVarint(nil, maxPacketBody+1)...)

	_, _, _, err := readPacket(bufio.NewReader(bytes.NewReader(oversized)))
	require.ErrorIs(t, err, errPacketTooLarge)
}

func TestConnackNamesWhyTheBrokerRefused(t *testing.T) {
	t.Parallel()

	require.NoError(t, connackError([]byte{0x00, 0x00}))

	err := connackError([]byte{0x00, 0x04})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad username or password")

	require.ErrorIs(t, connackError([]byte{0x00}), errMalformedPacket)
}
