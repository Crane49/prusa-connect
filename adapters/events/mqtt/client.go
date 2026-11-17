package mqtt

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// initialBackoff and maxBackoff bound the wait between connection
	// attempts.
	initialBackoff = 500 * time.Millisecond
	maxBackoff     = 30 * time.Second
	backoffFactor  = 2

	// pingsPerKeepalive sends two PINGREQ per keepalive interval, so one lost
	// packet does not make the broker drop the connection.
	pingsPerKeepalive = 2

	// acksPerPublish is the most acknowledgements one publish gets: PUBREC
	// then PUBCOMP, at QoS 2.
	acksPerPublish = 2

	// maxPacketID is the largest packet id. Zero is reserved.
	maxPacketID = 0xFFFF
)

var (
	// ErrNotConnected reports a publish attempted with no connection. A
	// publish never waits for the broker.
	ErrNotConnected = errors.New("mqtt: not connected to a broker")

	errPublishTimeout  = errors.New("mqtt: broker did not acknowledge the publish in time")
	errUnexpectedReply = errors.New("mqtt: broker answered a publish with the wrong packet")
)

// brokerConfig is everything the client needs to reach one broker.
type brokerConfig struct {
	address        string
	tls            *tls.Config
	clientID       string
	creds          credentials
	keepalive      time.Duration
	connectTimeout time.Duration
	publishTimeout time.Duration
	will           will
}

// client is a publish-only MQTT 3.1.1 client on the standard library. An
// adapter compiles against sdk/ and the standard library only.
type client struct {
	cfg          brokerConfig
	onConnect    func()
	onDisconnect func()

	active atomic.Pointer[connection]

	stop      chan struct{}
	stopped   chan struct{}
	closeOnce sync.Once

	nextID atomic.Uint32
}

// connection is one live socket and the acknowledgements outstanding on it. A
// packet id is valid only within its connection.
type connection struct {
	socket  net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex

	acksMu sync.Mutex
	acks   map[uint16]chan byte

	done      chan struct{}
	closeOnce sync.Once
}

// newClient builds the client. It connects to nothing until start is called.
// The caller must store the returned client before calling start, because
// onConnect uses it and can run as soon as the broker answers.
func newClient(cfg brokerConfig, onConnect, onDisconnect func()) *client {
	return &client{
		cfg:          cfg,
		onConnect:    onConnect,
		onDisconnect: onDisconnect,
		stop:         make(chan struct{}),
		stopped:      make(chan struct{}),
	}
}

// start starts the connection supervisor. It returns before the broker
// answers. The supervisor reconnects for as long as the process runs.
func (c *client) start() { go c.supervise() }

// connected reports whether a socket is up. It is false while reconnecting.
func (c *client) connected() bool {
	return c.active.Load() != nil
}

// publish sends one message and waits for whatever its QoS requires.
func (c *client) publish(topic, payload string, qos byte, retain bool) error {
	conn := c.active.Load()
	if conn == nil {
		return ErrNotConnected
	}

	if qos == 0 {
		packet, err := encodePublish(topic, payload, 0, retain, 0)
		if err != nil {
			return err
		}

		return c.write(conn, packet)
	}

	id := c.packetID()
	acks := conn.expect(id)

	defer conn.forget(id)

	packet, err := encodePublish(topic, payload, qos, retain, id)
	if err != nil {
		return err
	}

	if err = c.write(conn, packet); err != nil {
		return err
	}

	return c.awaitAck(conn, id, qos, acks)
}

// close stops reconnecting and waits for the supervisor to disconnect. The
// supervisor does the disconnect, because a connection can come up while
// close runs and only the supervisor sees it.
func (c *client) close() {
	c.closeOnce.Do(func() {
		close(c.stop)
		<-c.stopped
	})
}

// awaitAck completes the exchange the QoS requires: PUBACK at QoS 1, and
// PUBREC, PUBREL, PUBCOMP at QoS 2.
func (c *client) awaitAck(conn *connection, id uint16, qos byte, acks <-chan byte) error {
	first, err := waitFor(acks, c.cfg.publishTimeout, conn.done)
	if err != nil {
		return err
	}

	if qos == 1 {
		if first != pubackPacket {
			return fmt.Errorf("%w: expected PUBACK, got packet type %d", errUnexpectedReply, first)
		}

		return nil
	}

	if first != pubrecPacket {
		return fmt.Errorf("%w: expected PUBREC, got packet type %d", errUnexpectedReply, first)
	}

	if err = c.write(conn, encodePacketID(pubrelPacket, id)); err != nil {
		return err
	}

	second, err := waitFor(acks, c.cfg.publishTimeout, conn.done)
	if err != nil {
		return err
	}

	if second != pubcompPacket {
		return fmt.Errorf("%w: expected PUBCOMP, got packet type %d", errUnexpectedReply, second)
	}

	return nil
}

func (c *client) write(conn *connection, packet []byte) error {
	conn.writeMu.Lock()
	defer conn.writeMu.Unlock()

	if err := conn.socket.SetWriteDeadline(time.Now().Add(c.cfg.publishTimeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}

	if _, err := conn.socket.Write(packet); err != nil {
		return fmt.Errorf("write packet: %w", err)
	}

	return nil
}

// packetID returns the next packet id. It is never zero.
func (c *client) packetID() uint16 {
	return uint16(c.nextID.Add(1)%maxPacketID) + 1
}

// supervise connects, waits until the socket fails, backs off and connects
// again. It is the only goroutine that creates a connection.
func (c *client) supervise() {
	defer close(c.stopped)

	backoff := initialBackoff

	for {
		conn, err := c.dial()
		if err != nil {
			if !c.pause(backoff) {
				return
			}

			backoff = min(backoff*backoffFactor, maxBackoff)

			continue
		}

		backoff = initialBackoff

		c.active.Store(conn)

		// Start the reader before onConnect. onConnect publishes above QoS 0
		// and waits for an acknowledgement the reader delivers.
		go c.serve(conn)

		c.onConnect()

		select {
		case <-c.stop:
			c.farewell(conn)
			c.active.Store(nil)

			return
		case <-conn.done:
		}

		c.active.Store(nil)

		if !c.pause(initialBackoff) {
			return
		}
	}
}

// farewell runs onDisconnect and sends DISCONNECT. Without the DISCONNECT the
// broker would publish the will on a clean shutdown.
func (c *client) farewell(conn *connection) {
	c.onDisconnect()

	_ = c.write(conn, encodeEmpty(disconnectPacket))

	conn.close()
}

// serve reads the connection until a read fails, then closes it. The ping
// goroutine ends with the connection.
func (c *client) serve(conn *connection) {
	go c.ping(conn)

	c.read(conn)
	conn.close()
}

// pause waits, and reports false when the client is closing.
func (c *client) pause(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-c.stop:
		return false
	case <-timer.C:
		return true
	}
}

// dial opens a socket and completes the CONNECT handshake.
func (c *client) dial() (*connection, error) {
	select {
	case <-c.stop:
		return nil, ErrNotConnected
	default:
	}

	socket, err := c.open()
	if err != nil {
		return nil, err
	}

	conn := &connection{
		socket: socket,
		reader: bufio.NewReader(socket),
		acks:   make(map[uint16]chan byte),
		done:   make(chan struct{}),
	}

	if err = c.handshake(conn); err != nil {
		conn.close()

		return nil, err
	}

	return conn, nil
}

// open dials the broker, over TLS when configured, within connect_timeout.
func (c *client) open() (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.connectTimeout)
	defer cancel()

	dialer := &net.Dialer{Timeout: c.cfg.connectTimeout}

	if c.cfg.tls != nil {
		secure := &tls.Dialer{NetDialer: dialer, Config: c.cfg.tls}

		socket, err := secure.DialContext(ctx, "tcp", c.cfg.address)
		if err != nil {
			return nil, fmt.Errorf("dial broker over TLS: %w", err)
		}

		return socket, nil
	}

	socket, err := dialer.DialContext(ctx, "tcp", c.cfg.address)
	if err != nil {
		return nil, fmt.Errorf("dial broker: %w", err)
	}

	return socket, nil
}

func (c *client) handshake(conn *connection) error {
	packet, err := encodeConnect(c.cfg.clientID, keepaliveSeconds(c.cfg.keepalive), c.cfg.creds, c.cfg.will)
	if err != nil {
		return err
	}

	if err = c.write(conn, packet); err != nil {
		return err
	}

	if err = conn.socket.SetReadDeadline(time.Now().Add(c.cfg.connectTimeout)); err != nil {
		return fmt.Errorf("set read deadline: %w", err)
	}

	kind, _, body, err := readPacket(conn.reader)
	if err != nil {
		return fmt.Errorf("read CONNACK: %w", err)
	}

	if kind != connackPacket {
		return fmt.Errorf("%w: expected CONNACK, got packet type %d", errMalformedPacket, kind)
	}

	return connackError(body)
}

// read reads the socket until it fails and gives each acknowledgement to the
// publish waiting for it. Every other packet is ignored: this client never
// subscribes.
func (c *client) read(conn *connection) {
	for {
		if err := conn.socket.SetReadDeadline(time.Now().Add(c.readTimeout())); err != nil {
			return
		}

		kind, _, body, err := readPacket(conn.reader)
		if err != nil {
			return
		}

		switch kind {
		case pubackPacket, pubrecPacket, pubcompPacket:
			id, idErr := packetIDFrom(body)
			if idErr != nil {
				return
			}

			conn.deliver(id, kind)
		default:
		}
	}
}

// readTimeout is how long a silent socket is tolerated: one and a half
// keepalives. The ping runs every half keepalive, so a healthy connection is
// never silent that long.
func (c *client) readTimeout() time.Duration {
	if c.cfg.keepalive <= 0 {
		return maxBackoff
	}

	return c.cfg.keepalive + c.cfg.keepalive/pingsPerKeepalive
}

// keepaliveSeconds clamps the interval to the two-byte field of CONNECT.
func keepaliveSeconds(keepalive time.Duration) uint16 {
	seconds := int64(keepalive / time.Second)
	if seconds < 0 {
		return 0
	}

	if seconds > maxPacketID {
		return maxPacketID
	}

	return uint16(seconds)
}

// ping sends PINGREQ until the connection ends.
func (c *client) ping(conn *connection) {
	if c.cfg.keepalive <= 0 {
		return
	}

	ticker := time.NewTicker(c.cfg.keepalive / pingsPerKeepalive)
	defer ticker.Stop()

	for {
		select {
		case <-conn.done:
			return
		case <-c.stop:
			return
		case <-ticker.C:
			if err := c.write(conn, encodeEmpty(pingreqPacket)); err != nil {
				return
			}
		}
	}
}

// expect registers a waiter for an id. Call it before the packet is written,
// or a fast acknowledgement is lost.
func (conn *connection) expect(id uint16) <-chan byte {
	// Buffered for the two acknowledgements of QoS 2, so the reader never
	// blocks on a waiter that gave up.
	acks := make(chan byte, acksPerPublish)

	conn.acksMu.Lock()
	defer conn.acksMu.Unlock()

	conn.acks[id] = acks

	return acks
}

func (conn *connection) forget(id uint16) {
	conn.acksMu.Lock()
	defer conn.acksMu.Unlock()

	delete(conn.acks, id)
}

func (conn *connection) deliver(id uint16, kind byte) {
	conn.acksMu.Lock()
	acks, waiting := conn.acks[id]
	conn.acksMu.Unlock()

	if !waiting {
		return
	}

	select {
	case acks <- kind:
	default:
	}
}

func (conn *connection) close() {
	conn.closeOnce.Do(func() {
		close(conn.done)
		_ = conn.socket.Close()
	})
}

// waitFor waits for one acknowledgement, until the timeout or until the
// connection closes.
func waitFor(acks <-chan byte, timeout time.Duration, done <-chan struct{}) (byte, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case kind := <-acks:
		return kind, nil
	case <-done:
		return 0, ErrNotConnected
	case <-timer.C:
		return 0, errPublishTimeout
	}
}
