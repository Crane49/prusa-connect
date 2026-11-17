package kafka

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// The Kafka error codes this adapter names in an error message.
const (
	errNone                  = 0
	errUnknownTopic          = 3
	errLeaderNotAvailable    = 5
	errNotLeaderForPartition = 6
	errNotEnoughReplicas     = 19
	errNotEnoughAfterAppend  = 20
)

// metadataTTL is how long the partition map is trusted. A failed produce
// refreshes it sooner, so this bounds how long new partitions go unnoticed.
const metadataTTL = 5 * time.Minute

// ErrNotConnected reports a publish with no usable broker. A publish never
// waits for the cluster.
var ErrNotConnected = errors.New("kafka: no broker is reachable")

// ErrRefused reports a broker that answered with an error code, such as an
// unknown topic or too few replicas.
var ErrRefused = errors.New("kafka: the broker refused the produce")

// brokerConfig is everything one producer needs.
type brokerConfig struct {
	seeds          []string
	topic          string
	clientID       string
	acks           int16
	autoCreate     bool
	produceTimeout time.Duration
	connectTimeout time.Duration
	tls            *tls.Config
}

// partition is one partition and the broker currently leading it.
type partition struct {
	id     int32
	leader string // host:port
}

// client owns the connections to a cluster and the partition map. Only the
// sink's single sender goroutine calls publish, so one request is in flight at
// a time.
type client struct {
	config brokerConfig

	mu     sync.Mutex
	conns  map[string]*conn
	leader map[int32]string
	order  []int32
	seenAt time.Time

	correlation atomic.Int32
	up          atomic.Bool

	closed atomic.Bool
}

// conn is one socket to one broker.
type conn struct {
	socket net.Conn
	in     *bufio.Reader
}

func newClient(config brokerConfig) *client {
	return &client{config: config, conns: map[string]*conn{}, leader: map[int32]string{}}
}

func (c *client) connected() bool { return c.up.Load() }

// publish writes one record to the partition the key selects. A failed
// publish drops the partition map, so the next one looks up the leader again.
func (c *client) publish(key, value []byte) error {
	if c.closed.Load() {
		return ErrNotConnected
	}

	target, err := c.route(key)
	if err != nil {
		c.up.Store(false)

		return err
	}

	if err = c.produce(target, key, value); err != nil {
		c.forget()

		return err
	}

	c.up.Store(true)

	return nil
}

// route returns the partition for key and its leader. It refreshes the map
// when it is empty or stale.
func (c *client) route(key []byte) (partition, error) {
	c.mu.Lock()
	fresh := len(c.order) > 0 && time.Since(c.seenAt) < metadataTTL
	c.mu.Unlock()

	if !fresh {
		if err := c.refresh(); err != nil {
			return partition{}, err
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	id := partitionFor(key, c.order)

	leader, ok := c.leader[id]
	if !ok {
		return partition{}, fmt.Errorf("%w: partition %d has no leader", ErrNotConnected, id)
	}

	return partition{id: id, leader: leader}, nil
}

// refresh asks each seed in turn for the topic's partitions and their leaders,
// stopping at the first that answers.
func (c *client) refresh() error {
	body := &encoder{}
	body.int32(1) // one topic
	body.text(c.config.topic)
	body.boolean(c.config.autoCreate)

	var last error

	for _, seed := range c.config.seeds {
		answer, err := c.roundTrip(seed, apiMetadata, metadataVersion, body.buf)
		if err != nil {
			last = err

			continue
		}

		leader, order, err := readMetadata(answer, c.config.topic)
		if err != nil {
			last = err

			continue
		}

		c.mu.Lock()
		c.leader, c.order, c.seenAt = leader, order, time.Now()
		c.mu.Unlock()

		return nil
	}

	if last == nil {
		last = ErrNotConnected
	}

	return fmt.Errorf("%w: %w", ErrNotConnected, last)
}

// forget drops the partition map so the next publish looks the topology up
// again.
func (c *client) forget() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.order, c.seenAt = nil, time.Time{}
}

// produce sends one record batch and reads the broker's answer.
func (c *client) produce(target partition, key, value []byte) error {
	batch := recordBatch(key, value, time.Now())

	body := &encoder{}
	body.nullableText("", false) // transactional id: this producer is not transactional
	body.int16(c.config.acks)
	body.int32(produceTimeoutMillis(c.config.produceTimeout))
	body.int32(1) // one topic
	body.text(c.config.topic)
	body.int32(1) // one partition
	body.int32(target.id)
	body.blob(batch)

	answer, err := c.roundTrip(target.leader, apiProduce, produceVersion, body.buf)
	if err != nil {
		return err
	}

	// With acks=0 the broker sends no response.
	if c.config.acks == 0 {
		return nil
	}

	return readProduce(answer)
}

// produceTimeoutMillis clamps the timeout to the four-byte field, so a very
// long timeout does not overflow to a negative number.
func produceTimeoutMillis(timeout time.Duration) int32 {
	millis := timeout.Milliseconds()

	switch {
	case millis > math.MaxInt32:
		return math.MaxInt32
	case millis < 0:
		return 0
	default:
		return int32(millis)
	}
}

// roundTrip sends one request to one broker and reads its answer. It dials
// the broker on the first request to it.
func (c *client) roundTrip(address string, apiKey, version int16, body []byte) (*decoder, error) {
	broker, err := c.connection(address)
	if err != nil {
		return nil, err
	}

	correlation := c.correlation.Add(1)

	// One deadline covers the write and the read, so a broker that stops
	// answering does not block the sender goroutine.
	if err = broker.socket.SetDeadline(time.Now().Add(c.config.connectTimeout)); err != nil {
		c.drop(address)

		return nil, err
	}

	if _, err = broker.socket.Write(request(apiKey, version, correlation, c.config.clientID, body)); err != nil {
		c.drop(address)

		return nil, err
	}

	answer, err := readResponse(broker.in, correlation)
	if err != nil {
		c.drop(address)

		return nil, err
	}

	return answer, nil
}

func (c *client) connection(address string) (*conn, error) {
	c.mu.Lock()
	held, ok := c.conns[address]
	c.mu.Unlock()

	if ok {
		return held, nil
	}

	socket, err := c.open(address)
	if err != nil {
		return nil, err
	}

	opened := &conn{socket: socket, in: bufio.NewReader(socket)}

	c.mu.Lock()
	defer c.mu.Unlock()

	// stop may have run during the dial. Close the socket, or it leaks.
	if c.closed.Load() {
		_ = socket.Close()

		return nil, ErrNotConnected
	}

	c.conns[address] = opened

	return opened, nil
}

// open dials one broker, over TLS when configured, within connect_timeout.
func (c *client) open(address string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.connectTimeout)
	defer cancel()

	dialer := &net.Dialer{Timeout: c.config.connectTimeout}

	if c.config.tls != nil {
		secure := &tls.Dialer{NetDialer: dialer, Config: c.config.tls}

		socket, err := secure.DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, fmt.Errorf("kafka: dial %s over TLS: %w", address, err)
		}

		return socket, nil
	}

	socket, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("kafka: dial %s: %w", address, err)
	}

	return socket, nil
}

// drop closes a connection that failed. The next request to that broker dials
// again.
func (c *client) drop(address string) {
	c.mu.Lock()
	held, ok := c.conns[address]
	delete(c.conns, address)
	c.mu.Unlock()

	if ok {
		_ = held.socket.Close()
	}
}

// stop closes every connection. It is safe to call more than once.
func (c *client) stop() {
	c.closed.Store(true)
	c.up.Store(false)

	c.mu.Lock()
	held := c.conns
	c.conns = map[string]*conn{}
	c.mu.Unlock()

	for _, one := range held {
		_ = one.socket.Close()
	}
}

// readMetadata reads the topic's partitions and their leaders from a Metadata
// v4 response.
func readMetadata(d *decoder, topic string) (map[int32]string, []int32, error) {
	addresses := map[int32]string{}

	d.int32() // throttle time

	for range d.arrayLen() {
		node := d.int32()
		host := d.text()
		port := d.int32()
		d.text() // rack

		addresses[node] = net.JoinHostPort(host, strconv.Itoa(int(port)))
	}

	d.text()  // cluster id
	d.int32() // controller id

	leader := map[int32]string{}

	var order []int32

	for range d.arrayLen() {
		code := d.int16()
		name := d.text()
		d.int8() // internal

		for range d.arrayLen() {
			partitionCode := d.int16()
			id := d.int32()
			node := d.int32()

			d.skipInt32Array() // replicas
			d.skipInt32Array() // in-sync replicas

			if name != topic || partitionCode != errNone {
				continue
			}

			address, ok := addresses[node]
			if !ok {
				continue // a partition whose leader is not in this response has no route
			}

			leader[id] = address
			order = append(order, id)
		}

		if name == topic && code != errNone {
			return nil, nil, fmt.Errorf("%w: metadata for %s reports error %d", ErrRefused, topic, code)
		}
	}

	if d.err != nil {
		return nil, nil, d.err
	}

	if len(order) == 0 {
		return nil, nil, fmt.Errorf("%w: the cluster reports no usable partition for %s", ErrRefused, topic)
	}

	return leader, order, nil
}

// readProduce reads the partition's outcome from a Produce v3 response and
// turns an error code into an error.
func readProduce(d *decoder) error {
	for range d.arrayLen() {
		d.text() // topic name

		for range d.arrayLen() {
			id := d.int32()
			code := d.int16()

			d.int64() // base offset
			d.int64() // log append time

			if code != errNone {
				return fmt.Errorf("%w: partition %d answered error %d (%s)", ErrRefused, id, code, produceHint(code))
			}
		}
	}

	if d.err != nil {
		return d.err
	}

	return nil
}

// produceHint explains an error code in words, for the log line.
func produceHint(code int16) string {
	switch code {
	case errUnknownTopic:
		return "the topic does not exist and the cluster did not create it"
	case errLeaderNotAvailable, errNotLeaderForPartition:
		return "the partition leader moved, the next publish looks it up again"
	case errNotEnoughReplicas, errNotEnoughAfterAppend:
		return "fewer in-sync replicas than the topic requires"
	default:
		return "see the Kafka protocol error codes"
	}
}
