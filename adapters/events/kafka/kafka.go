// Package kafka publishes domain events to a Kafka topic.
//
// It is a reference implementation of sdk/eventsapi. It implements the protocol
// itself, with the standard library only: Metadata and Produce. It is a
// producer only, with no transactions, no idempotent producer id and no
// compression.
//
// The caller is the event bus, so a call never blocks. The queue is bounded, a
// full queue drops and counts, and Close reports what never arrived.
package kafka

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
const Type = "kafka"

const (
	defaultQueueDepth     = 256
	defaultTopic          = "prusa.events"
	defaultClientID       = "prusa-connect"
	defaultConnectTimeout = 10 * time.Second
	defaultProduceTimeout = 5 * time.Second
	defaultPort           = "9092"

	// acksAll is the default. With acks=1 a failover can lose a record the
	// sink counted as delivered.
	acksAll = -1

	// shutdownGrace is how long Close lets the queue drain. A failed produce
	// is never retried: a retry would reorder events behind a slow broker.
	shutdownGrace = 2 * time.Second
)

// ErrMissingBrokers rejects a spec with no brokers.
var ErrMissingBrokers = errors.New("kafka: config.brokers is required")

//nolint:gochecknoinits // registration has to happen before main; the host reaches it through a blank import.
func init() { eventsapi.Register(Type, doc(), New) }

// doc is this sink's entry in the published extension reference. The kinds
// come from kinds.go, the table the sink publishes from, so the reference
// cannot differ from the wire. They are declared as destinations.
func doc() extension.Doc {
	return extension.Doc{
		Summary: "Publishes each domain event to a Kafka topic, keyed by the printer it is about.",
		Description: "One JSON record per event, with the printer id as the record key so a printer's " +
			"history stays in order on one partition and a compacted topic keeps its last state. It " +
			"produces with acks=all: a sink that asked the leader alone would report events delivered " +
			"that a failover loses. It does not wait for the cluster: a sink that refused to start " +
			"because a broker is down would turn an optional integration into a boot dependency.",
		Version:   "1.1.0",
		Contract:  eventsapi.Contract,
		Stability: extension.StabilityStable,
		Publishes: destinations(),
		Options: []extension.Option{
			{
				Key:      "brokers",
				Type:     extension.OptionList,
				Required: true,
				Summary:  "The bootstrap brokers, as host:port. Any one of them is enough to find the rest.",
			},
			{
				Key:     "topic",
				Type:    extension.OptionString,
				Default: "prusa.events",
				Summary: "The topic events are produced to.",
			},
			{
				Key:     "client_id",
				Type:    extension.OptionString,
				Default: "prusa-connect",
				Summary: "The client id the cluster logs and applies quotas to.",
			},
			{
				Key:     "acks",
				Type:    extension.OptionInt,
				Default: "-1",
				Summary: "How many replicas must take a record: -1 for all, 1 for the leader, 0 for none.",
			},
			{
				Key:     "create_topic",
				Type:    extension.OptionBool,
				Default: "false",
				Summary: "Ask the cluster to create the topic when it is missing, where the cluster allows it.",
			},
			{
				Key:     "queue_depth",
				Type:    extension.OptionInt,
				Default: "256",
				Summary: "How many events may wait for a slow cluster before the oldest are dropped.",
			},
			{
				Key:     "connect_timeout",
				Type:    extension.OptionDuration,
				Default: "10s",
				Summary: "How long one request to a broker waits, dial included.",
			},
			{
				Key:     "produce_timeout",
				Type:    extension.OptionDuration,
				Default: "5s",
				Summary: "How long the leader waits for the replicas it was told to wait for.",
			},
			{
				Key:     "tls",
				Type:    extension.OptionBool,
				Default: "false",
				Summary: "Wrap every broker connection in TLS.",
			},
			{
				Key:     "tls_skip_verify",
				Type:    extension.OptionBool,
				Default: "false",
				Summary: "Accept any broker certificate. For a lab cluster, never for a deployment.",
			},
		},
	}
}

// Config is what the operator writes under `events.config`.
type Config struct {
	Brokers        []string
	Topic          string
	ClientID       string
	Acks           int16
	CreateTopic    bool
	QueueDepth     int
	ConnectTimeout time.Duration
	ProduceTimeout time.Duration
	TLS            bool
	TLSSkipVerify  bool
}

// record is one event on its way to the cluster.
type record struct {
	key   []byte
	value []byte
}

// event is the wire shape. Consumers depend on the field names. A field whose
// meaning changes gets a new name.
type event struct {
	Kind string `json:"kind"`
	Data any    `json:"data"`
}

// Processor implements eventsapi.Processor.
type Processor struct {
	client *client

	queue   chan record
	done    chan struct{}
	stopped chan struct{}

	dropped atomic.Uint64
	failed  atomic.Uint64

	closeOnce sync.Once
}

// New builds a Processor from its spec. It does not contact the cluster, so a
// broker that is down does not stop the server from starting. The topology is
// looked up on the first publish.
func New(spec eventsapi.ProviderSpec) (eventsapi.Processor, error) {
	cfg, err := configFrom(spec.Config)
	if err != nil {
		return nil, err
	}

	broker := brokerConfig{
		seeds:          cfg.Brokers,
		topic:          cfg.Topic,
		clientID:       cfg.ClientID,
		acks:           cfg.Acks,
		autoCreate:     cfg.CreateTopic,
		produceTimeout: cfg.ProduceTimeout,
		connectTimeout: cfg.ConnectTimeout,
	}

	if cfg.TLS {
		//nolint:gosec // G402: skipping verification is the operator's choice, for a cluster behind a private certificate authority. It defaults to off.
		broker.tls = &tls.Config{InsecureSkipVerify: cfg.TLSSkipVerify, MinVersion: tls.VersionTLS12}
	}

	p := &Processor{
		client:  newClient(broker),
		queue:   make(chan record, cfg.QueueDepth),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}

	go p.run()

	return p, nil
}

func (p *Processor) PrinterStateChanged(e eventsapi.PrinterState) {
	p.offer(e.ID, PrinterStateChanged.Name, e)
}

func (p *Processor) PrinterLabelsChanged(e eventsapi.PrinterLabels) {
	p.offer(e.ID, PrinterLabelsChanged.Name, e)
}

func (p *Processor) PrinterDeleted(id string) {
	p.offer(id, PrinterDeleted.Name, map[string]string{"id": id})
}

func (p *Processor) JobUpdated(e eventsapi.JobInfo) {
	p.offer(e.PrinterID, JobUpdated.Name, e)
}

func (p *Processor) LabelDeleted(id string) {
	p.offer(id, LabelDeleted.Name, map[string]string{"id": id})
}

// FactObserved produces one durable bus event. The record key is the origin,
// so every fact about one machine is on one partition and a consumer reads
// them in the order the host published them.
func (p *Processor) FactObserved(fact eventsapi.Fact) {
	p.offer(fact.Origin.String(), FactObserved.Name, fact)
}

// offer never blocks, because the caller is the event bus. When the queue is
// full the event is dropped and counted.
func (p *Processor) offer(key, kind string, data any) {
	value, err := json.Marshal(event{Kind: kind, Data: data})
	if err != nil {
		p.failed.Add(1)

		return
	}

	select {
	case <-p.done:
		p.dropped.Add(1)
	case p.queue <- record{key: []byte(key), value: value}:
	default:
		p.dropped.Add(1)
	}
}

// run is the only goroutine that produces, so the cluster sees events in the
// order they were offered.
func (p *Processor) run() {
	defer close(p.stopped)

	for {
		select {
		case <-p.done:
			p.drain()

			return
		case r := <-p.queue:
			p.deliver(r)
		}
	}
}

// drain produces what is queued, for at most shutdownGrace. It does not
// guarantee delivery.
func (p *Processor) drain() {
	deadline := time.NewTimer(shutdownGrace)
	defer deadline.Stop()

	for {
		select {
		case <-deadline.C:
			p.dropped.Add(uint64(len(p.queue)))

			return
		case r := <-p.queue:
			p.deliver(r)
		default:
			return
		}
	}
}

// deliver is the method to replace when copying this adapter.
func (p *Processor) deliver(r record) {
	if err := p.client.publish(r.key, r.value); err != nil {
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
	if dropped == 0 && failed == 0 {
		return nil
	}

	return fmt.Errorf("kafka: %d event(s) dropped, %d produce attempt(s) failed", dropped, failed)
}

func configFrom(raw map[string]any) (Config, error) {
	cfg := Config{
		Topic:          defaultTopic,
		ClientID:       defaultClientID,
		Acks:           acksAll,
		QueueDepth:     defaultQueueDepth,
		ConnectTimeout: defaultConnectTimeout,
		ProduceTimeout: defaultProduceTimeout,
	}

	cfg.Brokers = brokersFrom(raw["brokers"])
	if len(cfg.Brokers) == 0 {
		return Config{}, ErrMissingBrokers
	}

	if topic, ok := raw["topic"].(string); ok && topic != "" {
		cfg.Topic = topic
	}

	if id, ok := raw["client_id"].(string); ok && id != "" {
		cfg.ClientID = id
	}

	cfg.CreateTopic = boolAt(raw, "create_topic", cfg.CreateTopic)
	cfg.TLS = boolAt(raw, "tls", cfg.TLS)
	cfg.TLSSkipVerify = boolAt(raw, "tls_skip_verify", cfg.TLSSkipVerify)

	// A number from YAML is an int or a float64, depending on the decoder.
	switch acks := raw["acks"].(type) {
	case int:
		if err := cfg.setAcks(acks); err != nil {
			return Config{}, err
		}
	case float64:
		if err := cfg.setAcks(int(acks)); err != nil {
			return Config{}, err
		}
	}

	switch depth := raw["queue_depth"].(type) {
	case int:
		cfg.QueueDepth = depth
	case float64:
		cfg.QueueDepth = int(depth)
	}

	if cfg.QueueDepth < 1 {
		return Config{}, fmt.Errorf("kafka: queue_depth must be at least 1, got %d", cfg.QueueDepth)
	}

	var err error

	if cfg.ConnectTimeout, err = durationAt(raw, "connect_timeout", cfg.ConnectTimeout); err != nil {
		return Config{}, err
	}

	if cfg.ProduceTimeout, err = durationAt(raw, "produce_timeout", cfg.ProduceTimeout); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// brokersFrom reads the bootstrap list written as a YAML sequence or as one
// comma-separated string. A broker with no port gets the default port.
func brokersFrom(raw any) []string {
	var written []string

	switch value := raw.(type) {
	case string:
		written = strings.Split(value, ",")
	case []string:
		written = value
	case []any:
		for _, item := range value {
			if s, ok := item.(string); ok {
				written = append(written, s)
			}
		}
	}

	out := make([]string, 0, len(written))

	for _, one := range written {
		one = strings.TrimSpace(one)
		if one == "" {
			continue
		}

		if _, _, err := net.SplitHostPort(one); err != nil {
			one = net.JoinHostPort(one, defaultPort)
		}

		out = append(out, one)
	}

	return out
}

// setAcks accepts only the three values Kafka defines.
func (c *Config) setAcks(acks int) error {
	switch acks {
	case acksAll, 0, 1:
		c.Acks = int16(acks)

		return nil
	default:
		return fmt.Errorf("kafka: acks must be -1, 0 or 1, got %d", acks)
	}
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
			return 0, fmt.Errorf("kafka: config.%s: %w", key, err)
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
