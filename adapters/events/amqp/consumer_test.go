package amqp

import (
	"bufio"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The class and method identifiers a consumer needs. The adapter publishes and
// never consumes, so they live here rather than in wire.go: a reference
// implementation carries the code it runs, and a copier reading it should not
// have to work out which half is the test's.
const (
	classQueue = 50

	queueDeclare   = 10
	queueDeclareOk = 11
	queueBind      = 20
	queueBindOk    = 21

	basicConsume   = 20
	basicConsumeOk = 21
	basicDeliver   = 60
)

// delivered is one message as the broker handed it back.
type delivered struct {
	exchange string
	key      string
	body     string
}

// consumer is a minimal AMQP consumer, here so the adapter is verified through
// a real broker rather than against the encoder that produced the bytes. It
// declares an exclusive queue, binds it to the exchange under test, and reads
// whatever arrives.
type consumer struct {
	socket net.Conn
	in     *bufio.Reader
	out    *bufio.Writer
}

func consume(t *testing.T, address, exchange, bindingKey string) *consumer {
	t.Helper()

	socket, err := net.DialTimeout("tcp", address, 5*time.Second)
	require.NoError(t, err)

	t.Cleanup(func() { _ = socket.Close() })

	c := &consumer{socket: socket, in: bufio.NewReader(socket), out: bufio.NewWriter(socket)}

	require.NoError(t, socket.SetDeadline(time.Now().Add(10*time.Second)))

	c.open(t)
	queue := c.declare(t)
	c.bind(t, queue, exchange, bindingKey)
	c.start(t, queue)

	return c
}

func (c *consumer) send(t *testing.T, channel uint16, payload []byte) {
	t.Helper()

	require.NoError(t, writeFrame(c.out, frame{kind: frameMethod, channel: channel, payload: payload}))
	require.NoError(t, c.out.Flush())
}

func (c *consumer) open(t *testing.T) {
	t.Helper()

	_, err := c.socket.Write(protocolHeader)
	require.NoError(t, err)

	_, err = expect(c.in, classConnection, connectionStart)
	require.NoError(t, err)

	c.send(t, 0, newMethod(classConnection, connectionStartOk).
		table().
		shortstr("PLAIN").
		longstr([]byte("\x00guest\x00guest")).
		shortstr("en_US").
		bytes())

	_, err = expect(c.in, classConnection, connectionTune)
	require.NoError(t, err)

	c.send(t, 0, newMethod(classConnection, connectionTuneOk).
		short(1).
		long(maxFrameBytes).
		short(0).
		bytes())

	c.send(t, 0, newMethod(classConnection, connectionOpen).
		shortstr("/").shortstr("").bit(false).bytes())

	_, err = expect(c.in, classConnection, connectionOpenOk)
	require.NoError(t, err)

	c.send(t, publishChannel, newMethod(classChannel, channelOpen).shortstr("").bytes())

	_, err = expect(c.in, classChannel, channelOpenOk)
	require.NoError(t, err)
}

// declare makes an exclusive, auto-deleting queue with a name the broker
// chooses, so two runs of the suite never share one.
func (c *consumer) declare(t *testing.T) string {
	t.Helper()

	c.send(t, publishChannel, newMethod(classQueue, queueDeclare).
		short(0).
		shortstr("").
		bit(false). // passive
		bit(false). // durable
		bit(true).  // exclusive
		bit(true).  // auto-delete
		bit(false). // no-wait
		table().
		bytes())

	ok, err := expect(c.in, classQueue, queueDeclareOk)
	require.NoError(t, err)

	name := ok.args.shortstr()
	require.NoError(t, ok.args.err)
	require.NotEmpty(t, name)

	return name
}

func (c *consumer) bind(t *testing.T, queue, exchange, key string) {
	t.Helper()

	c.send(t, publishChannel, newMethod(classQueue, queueBind).
		short(0).
		shortstr(queue).
		shortstr(exchange).
		shortstr(key).
		bit(false). // no-wait
		table().
		bytes())

	_, err := expect(c.in, classQueue, queueBindOk)
	require.NoError(t, err)
}

func (c *consumer) start(t *testing.T, queue string) {
	t.Helper()

	c.send(t, publishChannel, newMethod(classBasic, basicConsume).
		short(0).
		shortstr(queue).
		shortstr(""). // consumer tag, chosen by the broker
		bit(false).   // no-local
		bit(true).    // no-ack, because nothing here redelivers
		bit(false).   // exclusive
		bit(false).   // no-wait
		table().
		bytes())

	_, err := expect(c.in, classBasic, basicConsumeOk)
	require.NoError(t, err)
}

// next reads one delivery, or reports that none arrived before the deadline. A
// delivery is three frames: the method, the content header, and the body.
func (c *consumer) next(t *testing.T, within time.Duration) (delivered, bool) {
	t.Helper()

	require.NoError(t, c.socket.SetReadDeadline(time.Now().Add(within)))

	for {
		f, err := readFrame(c.in)
		if err != nil {
			return delivered{}, false
		}

		if f.kind != frameMethod {
			continue
		}

		m, err := f.asMethod()
		require.NoError(t, err)

		if !m.is(classBasic, basicDeliver) {
			continue
		}

		m.args.shortstr() // consumer tag
		m.args.longlong() // delivery tag
		m.args.octet()    // redelivered
		exchange := m.args.shortstr()
		key := m.args.shortstr()
		require.NoError(t, m.args.err)

		header, err := readFrame(c.in)
		require.NoError(t, err)
		require.Equal(t, byte(frameHeader), header.kind)

		body, err := readFrame(c.in)
		require.NoError(t, err)
		require.Equal(t, byte(frameBody), body.kind)

		return delivered{exchange: exchange, key: key, body: string(body.payload)}, true
	}
}

// drain reads until nothing more arrives, so a case can assert on the whole set
// rather than on the first message.
func (c *consumer) drain(t *testing.T, within time.Duration) []delivered {
	t.Helper()

	var seen []delivered

	for {
		one, ok := c.next(t, within)
		if !ok {
			return seen
		}

		seen = append(seen, one)
	}
}
