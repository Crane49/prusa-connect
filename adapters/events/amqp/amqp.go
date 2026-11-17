// Package amqp publishes domain events to an AMQP 0-9-1 broker such as
// RabbitMQ.
//
// It is a reference implementation of sdk/eventsapi. It implements the protocol
// itself, with the standard library only: the opening handshake, one channel
// and Basic.Publish.
//
// The caller is the event bus, so a call never blocks. The queue is bounded, a
// full queue drops and counts, and Close reports what never arrived.
package amqp

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// Type is the `events.type` that selects this adapter.
const Type = "amqp"

const (
	defaultQueueDepth     = 256
	defaultHeartbeat      = 30 * time.Second
	defaultConnectTimeout = 10 * time.Second
	defaultExchange       = "prusa.events"
	defaultExchangeKind   = "topic"
	defaultVhost          = "/"

	// shutdownGrace is how long Close lets the queue drain. A failed publish
	// is never retried: a retry would reorder events behind a slow broker.
	shutdownGrace = 2 * time.Second
)

// ErrMissingURL rejects a spec with no broker.
var ErrMissingURL = errors.New("amqp: config.url is required")

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { eventsapi.Register(Type, doc(), New) }

// doc is this sink's entry in the published extension reference. The routing
// keys come from kinds.go, the table the sink publishes from, so the reference
// cannot differ from the wire. They are declared as destinations.
func doc() extension.Doc {
	return extension.Doc{
		Summary: "Publishes each domain event to an AMQP 0-9-1 exchange, keyed by the event type.",
		Description: "One JSON message per event on a topic exchange, with the event type as the routing " +
			"key, so a consumer binds to printer.# and gets printer events and nothing else. Publisher " +
			"confirms are on, so a message the broker refused is counted and reported at Close rather " +
			"than written to a socket and forgotten. It does not wait for the broker: a sink that refused " +
			"to start because a broker is down would turn an optional integration into a boot dependency.",
		Version:   "1.1.0",
		Contract:  eventsapi.Contract,
		Stability: extension.StabilityStable,
		Publishes: destinations(),
		Options: []extension.Option{
			{
				Key:      "url",
				Type:     extension.OptionURL,
				Required: true,
				Summary:  "The broker, as amqp://user:pass@host:port/vhost or amqps:// for TLS.",
			},
			{
				Key:     "exchange",
				Type:    extension.OptionString,
				Default: "prusa.events",
				Summary: "The exchange events are published to. An empty value publishes to the default exchange.",
			},
			{
				Key:     "exchange_type",
				Type:    extension.OptionString,
				Default: "topic",
				Summary: "The exchange type declared at connect: topic, direct, fanout or headers.",
			},
			{
				Key:     "declare_exchange",
				Type:    extension.OptionBool,
				Default: "true",
				Summary: "Declare the exchange on connect. Turn it off where the topology is owned elsewhere.",
			},
			{
				Key:     "routing_key_prefix",
				Type:    extension.OptionString,
				Summary: "Prepended to every routing key, for a broker shared with other publishers.",
			},
			{
				Key:     "persistent",
				Type:    extension.OptionBool,
				Default: "true",
				Summary: "Mark messages persistent, so a broker restart does not lose what is queued.",
			},
			{
				Key:     "queue_depth",
				Type:    extension.OptionInt,
				Default: "256",
				Summary: "How many events may wait for a slow broker before the oldest are dropped.",
			},
			{
				Key:     "heartbeat",
				Type:    extension.OptionDuration,
				Default: "30s",
				Summary: "The AMQP heartbeat interval proposed to the broker.",
			},
			{
				Key:     "connect_timeout",
				Type:    extension.OptionDuration,
				Default: "10s",
				Summary: "How long one connection attempt and its handshake wait.",
			},
			{
				Key:     "tls_skip_verify",
				Type:    extension.OptionBool,
				Default: "false",
				Summary: "Accept any broker certificate. For a lab broker, never for a deployment.",
			},
		},
	}
}

// Config is what the operator writes under `events.config`.
type Config struct {
	URL              string
	Exchange         string
	ExchangeKind     string
	DeclareExchange  bool
	RoutingKeyPrefix string
	Persistent       bool
	QueueDepth       int
	Heartbeat        time.Duration
	ConnectTimeout   time.Duration
	TLSSkipVerify    bool
}

// message is one event on its way to the broker.
type message struct {
	key  string
	body []byte
}

// event is the wire shape. Consumers depend on the field names. A field whose
// meaning changes gets a new name.
type event struct {
	Kind string `json:"kind"`
	Data any    `json:"data"`
}

// Processor implements eventsapi.Processor.
type Processor struct {
	client   *client
	exchange string
	prefix   string

	queue   chan message
	done    chan struct{}
	stopped chan struct{}

	dropped atomic.Uint64
	failed  atomic.Uint64

	closeOnce sync.Once
}

// New builds a Processor from its spec. It does not wait for the broker, so a
// broker that is down does not stop the server from starting. The connection
// is retried in the background.
func New(spec eventsapi.ProviderSpec) (eventsapi.Processor, error) {
	cfg, err := configFrom(spec.Config)
	if err != nil {
		return nil, err
	}

	broker, err := brokerConfigFrom(cfg)
	if err != nil {
		return nil, err
	}

	p := &Processor{
		client:   newClient(broker),
		exchange: cfg.Exchange,
		prefix:   cfg.RoutingKeyPrefix,
		queue:    make(chan message, cfg.QueueDepth),
		done:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}

	go p.run()

	return p, nil
}

func (p *Processor) PrinterStateChanged(e eventsapi.PrinterState) {
	p.offer(PrinterStateChanged.Name, e)
}

func (p *Processor) PrinterLabelsChanged(e eventsapi.PrinterLabels) {
	p.offer(PrinterLabelsChanged.Name, e)
}

func (p *Processor) PrinterDeleted(id string) {
	p.offer(PrinterDeleted.Name, map[string]string{"id": id})
}

func (p *Processor) JobUpdated(e eventsapi.JobInfo) {
	p.offer(JobUpdated.Name, e)
}

func (p *Processor) LabelDeleted(id string) {
	p.offer(LabelDeleted.Name, map[string]string{"id": id})
}

// FactObserved publishes one durable bus event. Every fact uses the same
// routing key, so a consumer's binding does not change when the host gains an
// event type.
func (p *Processor) FactObserved(fact eventsapi.Fact) {
	p.offer(FactObserved.Name, fact)
}

// offer never blocks, because the caller is the event bus. When the queue is
// full the event is dropped and counted.
func (p *Processor) offer(kind string, data any) {
	body, err := json.Marshal(event{Kind: kind, Data: data})
	if err != nil {
		p.failed.Add(1)

		return
	}

	select {
	case <-p.done:
		p.dropped.Add(1)
	case p.queue <- message{key: p.prefix + kind, body: body}:
	default:
		p.dropped.Add(1)
	}
}

// run is the only goroutine that publishes, so the broker sees events in the
// order they were offered.
func (p *Processor) run() {
	defer close(p.stopped)

	for {
		select {
		case <-p.done:
			p.drain()

			return
		case m := <-p.queue:
			p.deliver(m)
		}
	}
}

// drain publishes what is queued, for at most shutdownGrace. It does not
// guarantee delivery.
func (p *Processor) drain() {
	deadline := time.NewTimer(shutdownGrace)
	defer deadline.Stop()

	for {
		select {
		case <-deadline.C:
			p.dropped.Add(uint64(len(p.queue)))

			return
		case m := <-p.queue:
			p.deliver(m)
		default:
			return
		}
	}
}

// deliver is the method to replace when copying this adapter.
func (p *Processor) deliver(m message) {
	if err := p.client.publish(p.exchange, m.key, m.body); err != nil {
		p.failed.Add(1)
	}
}

// Close stops the sender and reports what never arrived. It is safe to call
// more than once.
func (p *Processor) Close() error {
	p.closeOnce.Do(func() { close(p.done) })

	<-p.stopped

	p.client.stop()

	dropped, failed := p.dropped.Load(), p.failed.Load()
	_, refused := p.client.counts()

	if dropped == 0 && failed == 0 && refused == 0 {
		return nil
	}

	return fmt.Errorf("amqp: %d event(s) dropped, %d publish attempt(s) failed, %d refused by the broker",
		dropped, failed, refused)
}

func configFrom(raw map[string]any) (Config, error) {
	cfg := Config{
		Exchange:        defaultExchange,
		ExchangeKind:    defaultExchangeKind,
		DeclareExchange: true,
		Persistent:      true,
		QueueDepth:      defaultQueueDepth,
		Heartbeat:       defaultHeartbeat,
		ConnectTimeout:  defaultConnectTimeout,
	}

	url, _ := raw["url"].(string)
	if url == "" {
		return Config{}, ErrMissingURL
	}

	cfg.URL = url

	if exchange, ok := raw["exchange"].(string); ok {
		cfg.Exchange = exchange
	}

	if kind, ok := raw["exchange_type"].(string); ok && kind != "" {
		cfg.ExchangeKind = kind
	}

	if prefix, ok := raw["routing_key_prefix"].(string); ok {
		cfg.RoutingKeyPrefix = prefix
	}

	cfg.DeclareExchange = boolAt(raw, "declare_exchange", cfg.DeclareExchange)
	cfg.Persistent = boolAt(raw, "persistent", cfg.Persistent)
	cfg.TLSSkipVerify = boolAt(raw, "tls_skip_verify", cfg.TLSSkipVerify)

	// A number from YAML is an int or a float64, depending on the decoder.
	switch depth := raw["queue_depth"].(type) {
	case int:
		cfg.QueueDepth = depth
	case float64:
		cfg.QueueDepth = int(depth)
	}

	if cfg.QueueDepth < 1 {
		return Config{}, fmt.Errorf("amqp: queue_depth must be at least 1, got %d", cfg.QueueDepth)
	}

	var err error

	if cfg.Heartbeat, err = durationAt(raw, "heartbeat", cfg.Heartbeat); err != nil {
		return Config{}, err
	}

	if cfg.ConnectTimeout, err = durationAt(raw, "connect_timeout", cfg.ConnectTimeout); err != nil {
		return Config{}, err
	}

	if cfg.Heartbeat < time.Second {
		return Config{}, fmt.Errorf("amqp: heartbeat must be at least a second, got %s", cfg.Heartbeat)
	}

	return cfg, nil
}

func boolAt(raw map[string]any, key string, fallback bool) bool {
	if value, ok := raw[key].(bool); ok {
		return value
	}

	return fallback
}

// durationAt reads a duration written as a Go duration string or as a number
// of seconds.
func durationAt(raw map[string]any, key string, fallback time.Duration) (time.Duration, error) {
	switch value := raw[key].(type) {
	case string:
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return 0, fmt.Errorf("amqp: config.%s: %w", key, err)
		}

		return parsed, nil
	case int:
		return time.Duration(value) * time.Second, nil
	case float64:
		return time.Duration(value) * time.Second, nil
	default:
		return fallback, nil
	}
}

// brokerConfigFrom turns the operator's URL into an address, a vhost, the
// credentials and a TLS setting.
func brokerConfigFrom(cfg Config) (brokerConfig, error) {
	address, vhost, creds, secure, err := brokerAddress(cfg.URL)
	if err != nil {
		return brokerConfig{}, err
	}

	broker := brokerConfig{
		address:        address,
		vhost:          vhost,
		creds:          creds,
		heartbeat:      cfg.Heartbeat,
		connectTimeout: cfg.ConnectTimeout,
		exchange:       cfg.Exchange,
		exchangeKind:   cfg.ExchangeKind,
		declare:        cfg.DeclareExchange,
		persistent:     cfg.Persistent,
	}

	if secure {
		//nolint:gosec // G402: skipping verification is the operator's choice, for a broker behind a self-signed certificate. It defaults to off.
		broker.tls = &tls.Config{InsecureSkipVerify: cfg.TLSSkipVerify, MinVersion: tls.VersionTLS12}
	}

	return broker, nil
}

// brokerAddress reads the amqp URI form RabbitMQ documents, and a bare
// host:port for a broker with no credentials.
func brokerAddress(raw string) (address, vhost string, creds credentials, secure bool, err error) {
	creds = credentials{username: "guest", password: "guest"}
	vhost = defaultVhost

	scheme, rest, found := strings.Cut(raw, "://")
	if !found {
		scheme, rest = "amqp", raw
	}

	defaultPort := "5672"

	switch scheme {
	case "amqp":
	case "amqps":
		secure, defaultPort = true, "5671"
	default:
		return "", "", creds, false, fmt.Errorf(
			"amqp: config.url scheme %q is not supported, use amqp:// or amqps://", scheme)
	}

	if authority, remainder, hasAuth := strings.Cut(rest, "@"); hasAuth {
		user, password, _ := strings.Cut(authority, ":")
		creds = credentials{username: unescape(user), password: unescape(password)}
		rest = remainder
	}

	if hostPort, path, hasPath := strings.Cut(rest, "/"); hasPath {
		rest = hostPort

		// An empty path is the default vhost. The RabbitMQ URI spec writes
		// the vhost "/" as %2F.
		if path != "" {
			vhost = unescape(path)
		}
	}

	host, port, splitErr := net.SplitHostPort(rest)
	if splitErr != nil {
		host, port = rest, defaultPort
	}

	if host == "" {
		return "", "", creds, false, fmt.Errorf("amqp: config.url %q names no host", raw)
	}

	return net.JoinHostPort(host, port), vhost, creds, secure, nil
}

// unescape decodes percent-encoding, so a password with an at sign and a
// vhost written as %2F both work. An invalid escape is left as written.
func unescape(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}

	var out strings.Builder

	for i := 0; i < len(s); i++ {
		if s[i] != '%' || i+2 >= len(s) {
			out.WriteByte(s[i])

			continue
		}

		high, highOK := hexDigit(s[i+1])
		low, lowOK := hexDigit(s[i+2])

		if !highOK || !lowOK {
			out.WriteByte(s[i])

			continue
		}

		out.WriteByte(high<<4 | low)

		i += 2
	}

	return out.String()
}

func hexDigit(c byte) (byte, bool) {
	const pastNine = 10

	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + pastNine, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + pastNine, true
	default:
		return 0, false
	}
}
