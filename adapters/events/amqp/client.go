package amqp

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"net"
	"slices"
	"strings"
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

	// heartbeatsPerInterval sends two heartbeats per interval, so one lost
	// frame does not make the broker drop the connection.
	heartbeatsPerInterval = 2

	// publishChannel is the one channel this adapter opens.
	publishChannel = 1
)

// ErrNotConnected reports a publish attempted with no connection. A publish
// never waits for the broker.
var ErrNotConnected = errors.New("amqp: not connected to a broker")

// ErrRefused reports a broker that closed the connection or the channel with a
// reason, such as a missing exchange, permission or vhost.
var ErrRefused = errors.New("amqp: the broker refused")

// credentials are the PLAIN mechanism's user and password.
type credentials struct {
	username string
	password string
}

// brokerConfig is everything one connection needs.
type brokerConfig struct {
	address        string
	vhost          string
	creds          credentials
	tls            *tls.Config
	heartbeat      time.Duration
	connectTimeout time.Duration
	exchange       string
	exchangeKind   string
	declare        bool
	persistent     bool
}

// client owns one connection to a broker and the goroutine that keeps it. It
// reconnects for as long as the process runs.
type client struct {
	config brokerConfig

	// live is the connection to publish on. The supervisor swaps it. A
	// publish reads it once, so one publish never uses two connections.
	mu   sync.Mutex
	live *connection

	up atomic.Bool

	confirmed atomic.Uint64
	refused   atomic.Uint64

	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

// connection is one live session. mu keeps the three frames of a publish
// together.
type connection struct {
	socket net.Conn

	mu     sync.Mutex
	out    *bufio.Writer
	closed bool
}

func newClient(config brokerConfig) *client {
	c := &client{
		config:  config,
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}

	go c.supervise()

	return c
}

func (c *client) connected() bool { return c.up.Load() }

// counts reports what the broker confirmed and what it refused.
func (c *client) counts() (confirmed, refused uint64) {
	return c.confirmed.Load(), c.refused.Load()
}

// supervise keeps one connection up. It backs off after every failed attempt.
func (c *client) supervise() {
	defer close(c.stopped)

	backoff := initialBackoff

	for {
		select {
		case <-c.done:
			return
		default:
		}

		conn, err := c.dial()
		if err == nil {
			backoff = initialBackoff

			c.serve(conn)

			continue
		}

		select {
		case <-c.done:
			return
		case <-time.After(backoff):
		}

		if backoff < maxBackoff {
			backoff *= backoffFactor
		}
	}
}

// serve keeps a connection live and sends heartbeats until it fails, then
// tears it down.
func (c *client) serve(conn *connection) {
	c.mu.Lock()
	c.live = conn
	c.mu.Unlock()
	c.up.Store(true)

	beats := time.NewTicker(c.config.heartbeat / heartbeatsPerInterval)
	defer beats.Stop()

	failed := make(chan struct{})

	go func() {
		defer close(failed)

		c.read(conn)
	}()

	for {
		select {
		case <-c.done:
			c.tearDown(conn)
			<-failed

			return
		case <-failed:
			c.tearDown(conn)

			return
		case <-beats.C:
			if err := conn.write(frame{kind: frameHeartbeat, channel: 0}); err != nil {
				c.tearDown(conn)
				<-failed

				return
			}
		}
	}
}

func (c *client) tearDown(conn *connection) {
	c.up.Store(false)

	c.mu.Lock()
	if c.live == conn {
		c.live = nil
	}
	c.mu.Unlock()

	conn.shut()
}

// read reads the broker's frames until one of them, or the socket, ends the
// session. It counts the publisher confirms.
func (c *client) read(conn *connection) {
	reader := bufio.NewReader(conn.socket)

	for {
		// The read deadline is two heartbeat intervals. A broker that sends
		// nothing for that long is reconnected to.
		_ = conn.socket.SetReadDeadline(time.Now().Add(c.config.heartbeat * heartbeatsPerInterval))

		f, err := readFrame(reader)
		if err != nil {
			return
		}

		if f.kind != frameMethod {
			continue
		}

		m, err := f.asMethod()
		if err != nil {
			return
		}

		switch {
		case m.is(classBasic, basicAck):
			c.confirmed.Add(1)
		case m.is(classBasic, basicNack), m.is(classBasic, basicReturn):
			c.refused.Add(1)
		case m.is(classChannel, channelClose), m.is(classConnection, connectionClose):
			c.refused.Add(1)

			return
		}
	}
}

// publish sends one message as three frames under the connection's lock. A
// heartbeat frame between them would be a protocol error.
func (c *client) publish(exchange, key string, body []byte) error {
	c.mu.Lock()
	conn := c.live
	c.mu.Unlock()

	if conn == nil || !c.up.Load() {
		return ErrNotConnected
	}

	publishFrame := newMethod(classBasic, basicPublish).
		short(0). // reserved
		shortstr(exchange).
		shortstr(key).
		bit(false). // mandatory: an unroutable message is dropped, not returned
		bit(false). // immediate: removed from the protocol in 0-9-1 brokers
		bytes()

	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.closed {
		return ErrNotConnected
	}

	frames := []frame{
		{kind: frameMethod, channel: publishChannel, payload: publishFrame},
		{
			kind:    frameHeader,
			channel: publishChannel,
			payload: contentHeader(len(body), "application/json", c.config.persistent, time.Now()),
		},
		{kind: frameBody, channel: publishChannel, payload: body},
	}

	for _, f := range frames {
		if err := writeFrame(conn.out, f); err != nil {
			conn.closed = true

			return err
		}
	}

	if err := conn.out.Flush(); err != nil {
		conn.closed = true

		return err
	}

	return nil
}

func (conn *connection) write(f frame) error {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.closed {
		return ErrNotConnected
	}

	if err := writeFrame(conn.out, f); err != nil {
		conn.closed = true

		return err
	}

	if err := conn.out.Flush(); err != nil {
		conn.closed = true

		return err
	}

	return nil
}

func (conn *connection) shut() {
	conn.mu.Lock()
	conn.closed = true
	conn.mu.Unlock()

	_ = conn.socket.Close()
}

// stop ends the supervisor and its connection.
func (c *client) stop() {
	c.once.Do(func() { close(c.done) })

	<-c.stopped
}

// dial opens a connection and completes the handshake, so the connection it
// returns is ready to publish on.
func (c *client) dial() (*connection, error) {
	socket, err := c.open()
	if err != nil {
		return nil, err
	}

	// The deadline covers the whole handshake, so a broker that stops
	// answering does not block the supervisor.
	if err = socket.SetDeadline(time.Now().Add(c.config.connectTimeout)); err != nil {
		_ = socket.Close()

		return nil, err
	}

	conn := &connection{socket: socket, out: bufio.NewWriter(socket)}

	if err = c.handshake(conn, bufio.NewReader(socket)); err != nil {
		_ = socket.Close()

		return nil, err
	}

	if err = socket.SetDeadline(time.Time{}); err != nil {
		_ = socket.Close()

		return nil, err
	}

	return conn, nil
}

// open dials the broker, over TLS when configured, within connect_timeout.
func (c *client) open() (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.connectTimeout)
	defer cancel()

	dialer := &net.Dialer{Timeout: c.config.connectTimeout}

	if c.config.tls != nil {
		secure := &tls.Dialer{NetDialer: dialer, Config: c.config.tls}

		socket, err := secure.DialContext(ctx, "tcp", c.config.address)
		if err != nil {
			return nil, fmt.Errorf("amqp: dial broker over TLS: %w", err)
		}

		return socket, nil
	}

	socket, err := dialer.DialContext(ctx, "tcp", c.config.address)
	if err != nil {
		return nil, fmt.Errorf("amqp: dial broker: %w", err)
	}

	return socket, nil
}

// handshake runs the opening sequence: the protocol header, PLAIN
// authentication, tuning, the vhost, one channel, the exchange, and publisher
// confirms.
func (c *client) handshake(conn *connection, in *bufio.Reader) error {
	if _, err := conn.socket.Write(protocolHeader); err != nil {
		return err
	}

	start, err := expect(in, classConnection, connectionStart)
	if err != nil {
		return err
	}

	if err = requirePlain(start); err != nil {
		return err
	}

	// The PLAIN response (RFC 4616) is a null-separated triple with an empty
	// authorization identity.
	response := "\x00" + c.config.creds.username + "\x00" + c.config.creds.password

	startOk := newMethod(classConnection, connectionStartOk).
		table().           // peer properties, deliberately empty
		shortstr("PLAIN"). //
		longstr([]byte(response)).
		shortstr("en_US").
		bytes()

	if err = conn.write(frame{kind: frameMethod, payload: startOk}); err != nil {
		return err
	}

	tune, err := expect(in, classConnection, connectionTune)
	if err != nil {
		return err
	}

	channelMax, frameMax, heartbeat := c.tuning(tune)

	tuneOk := newMethod(classConnection, connectionTuneOk).
		short(channelMax).
		long(frameMax).
		short(heartbeat).
		bytes()

	if err = conn.write(frame{kind: frameMethod, payload: tuneOk}); err != nil {
		return err
	}

	open := newMethod(classConnection, connectionOpen).
		shortstr(c.config.vhost).
		shortstr(""). // reserved
		bit(false).   // reserved
		bytes()

	if err = conn.write(frame{kind: frameMethod, payload: open}); err != nil {
		return err
	}

	if _, err = expect(in, classConnection, connectionOpenOk); err != nil {
		return err
	}

	return c.openChannel(conn, in)
}

// openChannel opens the publishing channel, declares the exchange when
// configured, and turns on publisher confirms.
func (c *client) openChannel(conn *connection, in *bufio.Reader) error {
	open := newMethod(classChannel, channelOpen).shortstr("").bytes()
	if err := conn.write(frame{kind: frameMethod, channel: publishChannel, payload: open}); err != nil {
		return err
	}

	if _, err := expect(in, classChannel, channelOpenOk); err != nil {
		return err
	}

	if c.config.declare && c.config.exchange != "" {
		declare := newMethod(classExchange, exchangeDeclare).
			short(0). // reserved
			shortstr(c.config.exchange).
			shortstr(c.config.exchangeKind).
			bit(false). // passive
			bit(true).  // durable, so a broker restart does not lose the topology
			bit(false). // auto-delete
			bit(false). // internal
			bit(false). // no-wait, because the declare-ok is what reports a mismatch
			table().
			bytes()

		if err := conn.write(frame{kind: frameMethod, channel: publishChannel, payload: declare}); err != nil {
			return err
		}

		if _, err := expect(in, classExchange, exchangeDeclareOk); err != nil {
			return err
		}
	}

	// Without publisher confirms the sink cannot tell that the broker
	// refused a message.
	selectConfirm := newMethod(classConfirm, confirmSelect).bit(false).bytes()
	if err := conn.write(frame{kind: frameMethod, channel: publishChannel, payload: selectConfirm}); err != nil {
		return err
	}

	_, err := expect(in, classConfirm, confirmSelectOk)

	return err
}

// requirePlain fails when the broker does not offer PLAIN, the one mechanism
// this adapter implements.
func requirePlain(start method) error {
	start.args.octet() // version-major
	start.args.octet() // version-minor
	skipTable(start.args)

	mechanisms := start.args.longstr()
	if start.args.err != nil {
		return start.args.err
	}

	if slices.Contains(strings.Fields(mechanisms), "PLAIN") {
		return nil
	}

	return fmt.Errorf("%w: the broker offers %q and this adapter speaks PLAIN", ErrProtocol, mechanisms)
}

// tuning answers the broker's proposal. Each value is the smaller of the
// broker's offer and this adapter's own limit, as AMQP 0-9-1 requires.
func (c *client) tuning(tune method) (channelMax uint16, frameMax uint32, heartbeat uint16) {
	offeredChannels := tune.args.short()
	offeredFrame := tune.args.long()
	offeredHeartbeat := tune.args.short()

	channelMax = publishChannel
	if offeredChannels != 0 && offeredChannels < channelMax {
		channelMax = offeredChannels
	}

	frameMax = maxFrameBytes
	if offeredFrame != 0 && offeredFrame < frameMax {
		frameMax = offeredFrame
	}

	// The heartbeat field is seconds in two bytes, so a longer heartbeat is
	// capped.
	heartbeat = math.MaxUint16
	if seconds := c.config.heartbeat / time.Second; seconds > 0 && seconds < math.MaxUint16 {
		heartbeat = uint16(seconds)
	}

	if offeredHeartbeat != 0 && offeredHeartbeat < heartbeat {
		heartbeat = offeredHeartbeat
	}

	return channelMax, frameMax, heartbeat
}

// expect reads frames until the awaited method arrives. A close from the
// broker becomes an error with its reason. Heartbeats are skipped.
func expect(in *bufio.Reader, class, id uint16) (method, error) {
	for {
		f, err := readFrame(in)
		if err != nil {
			return method{}, err
		}

		if f.kind != frameMethod {
			continue
		}

		m, err := f.asMethod()
		if err != nil {
			return method{}, err
		}

		switch {
		case m.is(class, id):
			return m, nil
		case m.is(classConnection, connectionClose), m.is(classChannel, channelClose):
			return method{}, refusal(m)
		case m.is(classConnection, connectionSecure):
			return method{}, fmt.Errorf(
				"%w: the broker asked to continue the security handshake, which needs a mechanism "+
					"beyond PLAIN", ErrProtocol)
		default:
			return method{}, fmt.Errorf("%w: expected class %d method %d, got %s", ErrProtocol, class, id, m)
		}
	}
}

// refusal reads the reply code and text a broker closes with, such as
// "NOT_FOUND - no exchange".
func refusal(m method) error {
	code := m.args.short()
	text := m.args.shortstr()

	return fmt.Errorf("%w: %d %s", ErrRefused, code, text)
}

// skipTable steps over a field table without decoding it.
func skipTable(r *reader) {
	size := int(r.long())
	if r.err != nil || len(r.buf) < size {
		r.fail(size)

		return
	}

	r.buf = r.buf[size:]
}
