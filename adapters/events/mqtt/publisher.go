package mqtt

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// message is one queued publish. key is the topic before routing, the key of
// the change filter. topic is what is sent.
type message struct {
	key     string
	topic   string
	payload string
	qos     byte
	retain  bool
}

// Publisher owns the broker connection and has a Publish method for every
// frozen topic. Processor calls only some of them. The others are ready for
// telemetry that eventsapi does not deliver yet.
type Publisher struct {
	client  *client
	router  *Router
	changes *changeFilter
	// tools is every tool channel holding a value, for PublishToolTree.
	tools toolTopics

	queue   chan message
	done    chan struct{}
	stopped chan struct{}

	dropped atomic.Uint64
	failed  atomic.Uint64

	closeOnce sync.Once
}

// Warnings reports the routing rules that were skipped or shadowed at startup.
func (p *Publisher) Warnings() []string {
	return p.router.Warnings()
}

// Close stops the sender, disconnects from the broker, and reports what never
// arrived. The sender stops first, so the queue can drain. The client then
// publishes "0" on v1/server/online itself and does not leave it to the will.
// It is safe to call more than once.
func (p *Publisher) Close() error {
	p.closeOnce.Do(func() {
		close(p.done)
		<-p.stopped

		p.client.close()
	})

	dropped, failed := p.dropped.Load(), p.failed.Load()
	if dropped == 0 && failed == 0 {
		return nil
	}

	return fmt.Errorf("mqtt: %d event(s) dropped, %d publish attempt(s) failed", dropped, failed)
}

// offer is the path of every state Publish method: change filter, routing,
// then a bounded queue that never blocks the caller.
func (p *Publisher) offer(topic Topic, resolved, payload string) {
	if !p.changes.accept(resolved, payload) {
		return
	}

	p.enqueue(topic, resolved, payload)
}

// enqueue routes and queues an accepted message. A topic that a rule drops
// never reaches the queue.
func (p *Publisher) enqueue(topic Topic, resolved, payload string) {
	routed, drop := p.router.Route(resolved)
	if drop {
		return
	}

	msg := message{key: resolved, topic: routed, payload: payload, qos: topic.QoS, retain: topic.Retain}

	select {
	case <-p.done:
		p.discard(msg)
	case p.queue <- msg:
	default:
		p.discard(msg)
	}
}

// discard counts a message that never reached the broker and invalidates its
// change-filter entry, so the same value is published next time.
func (p *Publisher) discard(msg message) {
	p.dropped.Add(1)
	p.changes.invalidate(msg.key)
}

// run is the only goroutine that publishes from the queue, so the broker sees
// messages in the order they were offered.
func (p *Publisher) run() {
	defer close(p.stopped)

	for {
		select {
		case <-p.done:
			p.drain()

			return
		case msg := <-p.queue:
			p.deliver(msg)
		}
	}
}

// drain publishes what is queued, for at most shutdownGrace. It does not
// guarantee delivery.
func (p *Publisher) drain() {
	deadline := time.NewTimer(shutdownGrace)
	defer deadline.Stop()

	for {
		select {
		case <-deadline.C:
			p.dropped.Add(uint64(len(p.queue)))

			return
		case msg := <-p.queue:
			p.deliver(msg)
		default:
			return
		}
	}
}

// deliver publishes one message. A failure invalidates its change-filter
// entry, so the value is sent again on the next update.
func (p *Publisher) deliver(msg message) {
	if err := p.client.publish(msg.topic, msg.payload, msg.qos, msg.retain); err != nil {
		p.failed.Add(1)
		p.changes.invalidate(msg.key)
	}
}

// publishServerOnline publishes the server-online topic directly, not through
// the queue. Its callers run on the connection supervisor and cannot wait
// behind the queue.
func (p *Publisher) publishServerOnline(online bool) {
	err := p.client.publish(ServerOnline.Pattern, boolPayload(online), ServerOnline.QoS, ServerOnline.Retain)
	if err != nil && !errors.Is(err, ErrNotConnected) {
		p.failed.Add(1)
	}
}

// onConnect runs on every connection and reconnection. It resets the change
// filter, see changeFilter.reset, and announces the server.
func (p *Publisher) onConnect() {
	p.changes.reset()
	p.publishServerOnline(true)
}

// onDisconnect runs once, before a clean disconnect.
func (p *Publisher) onDisconnect() {
	p.publishServerOnline(false)
}
