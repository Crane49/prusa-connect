package natsbind

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/join"
)

// reconnectWait is how long a connection from this node to its own NATS server
// waits before it connects again.
const reconnectWait = 250 * time.Millisecond

// link has the NATS resources this node creates for one node below it. A NATS
// account is an isolated set of subjects, and each node below gets one, named
// by accountPub. conn is this node's connection in that account. js gives
// access to the stream with the method invocations for the node below. subs
// are this node's subscriptions in the account.
type link struct {
	id         fractal.NodeID
	path       fractal.NodePath
	domain     string
	accountPub string

	// publishing is held for reading across a durable publish, and for
	// writing while onRevoke takes the stream away. So no publish that read
	// the stream before the revocation lands after the purge.
	publishing sync.RWMutex

	mu        sync.Mutex
	conn      *nats.Conn
	js        nats.JetStreamContext
	subs      []*nats.Subscription
	cancel    context.CancelFunc
	identity  fractal.Identity
	admission Admission
	lastHB    time.Time
	connected bool
	// leafCID is the ID of the leafnode connection pollLeaves last saw the
	// node below listen on.
	leafCID uint64
	ever    bool
	silent  bool
}

// snapshot returns the state of one link for Status.
func (l *link) snapshot() Below {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := Below{
		ID:        l.id,
		Path:      l.path,
		Identity:  l.identity,
		Admission: l.admission,
		Connected: l.connected,
	}
	if !l.lastHB.IsZero() {
		at := l.lastHB
		out.LastHeartbeat = &at
	}
	return out
}

// close stops the goroutines of one link and closes its connection.
func (l *link) close() {
	l.mu.Lock()
	cancel, conn := l.cancel, l.conn
	l.cancel, l.conn, l.js, l.subs = nil, nil, nil, nil
	l.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		conn.Close()
	}
}

// publishDurable publishes one message to a stream of the link and waits
// until JetStream stored it.
func (l *link) publishDurable(subject string, payload []byte, opts []nats.PubOpt) error {
	l.publishing.RLock()
	defer l.publishing.RUnlock()
	l.mu.Lock()
	js := l.js
	l.mu.Unlock()
	if js == nil {
		return fmt.Errorf("%s: %w", l.id, ErrNoLink)
	}
	if _, err := js.Publish(subject, payload, opts...); err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}

// touch records the time a heartbeat arrived on the link. watchHeartbeats
// measures the silence from that time, because only the absence of
// heartbeats counts as a disconnect. A heartbeat also ends the silence, so
// the next poll of the
// leafnode connections can report the node below as connected again.
func (l *link) touch(at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastHB = at
	l.silent = false
}

// linkOf returns the link to the node below with this Node ID.
func (b *Binding) linkOf(id fractal.NodeID) (*link, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	l, ok := b.links[id]
	return l, ok
}

// childIDs returns the Node ID of every node that joined this node and is not
// revoked, sorted as text.
func (b *Binding) childIDs() []fractal.NodeID {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]fractal.NodeID, 0, len(b.links))
	for id, l := range b.links {
		l.mu.Lock()
		admitted := l.admission == AdmissionAdmitted
		l.mu.Unlock()
		if admitted {
			out = append(out, id)
		}
	}
	sortNodeIDs(out)
	return out
}

// restoreLinks builds the link of every node in the admissions file. After a
// restart the node therefore has the same accounts and streams, and the
// messages that waited in the streams.
func (b *Binding) restoreLinks(ctx context.Context) error {
	for _, a := range b.parent.Admissions() {
		if a.Revoked {
			// The resolver is empty at every start. Without a new revocation
			// the server would accept a revoked credential that has not
			// expired.
			if err := b.revokeAccount(a.NodeID); err != nil {
				return err
			}
			b.recordRevoked(a)
			continue
		}
		if err := b.provisionLink(ctx, a.NodeID, a.Identity, a.EverConnected); err != nil {
			return err
		}
	}
	return nil
}

// recordRevoked keeps a revoked node in b.links with AdmissionRevoked, so
// Status still lists it for the operator.
func (b *Binding) recordRevoked(a join.Admission) {
	b.mu.Lock()
	defer b.mu.Unlock()
	path, err := b.path.Child(a.NodeID)
	if err != nil {
		path = ""
	}
	held, ok := b.links[a.NodeID]
	if !ok {
		held = &link{id: a.NodeID, path: path, domain: Domain(path)}
		b.links[a.NodeID] = held
	}
	held.mu.Lock()
	held.identity = a.Identity
	held.admission = AdmissionRevoked
	held.connected = false
	held.mu.Unlock()
}

// provisionLink creates the NATS resources for one node below: its account,
// this node's connection in that account, the stream for the method
// invocations sent to it, this node's subscriptions, and the goroutine that
// reads its results.
//
// A second call for a link with an open connection changes nothing, so a
// repeated join request gets the same answer. It never creates an existing
// stream or consumer again, so the messages that wait in them stay. Join
// requests are answered
// concurrently, so one call runs at a time, and two grants for one node build
// one link. A revocation also holds b.provisioning, so provisionLink returns
// join.ErrRevoked for a node revoked after its checks, and builds no link.
func (b *Binding) provisionLink(
	ctx context.Context,
	id fractal.NodeID,
	identity fractal.Identity,
	ever bool,
) error {
	b.provisioning.Lock()
	defer b.provisioning.Unlock()
	if a, ok := b.parent.Admission(id); ok && a.Revoked {
		return fmt.Errorf("build the link of %s: %w", id, join.ErrRevoked)
	}
	if l, ok := b.linkOf(id); ok {
		l.mu.Lock()
		l.identity = identity
		l.admission = AdmissionAdmitted
		l.ever = l.ever || ever
		live := l.conn != nil && !l.conn.IsClosed()
		l.mu.Unlock()
		if live {
			return nil
		}
		l.close()
	}
	path, err := b.Path().Child(id)
	if err != nil {
		return fmt.Errorf("build the path of %s: %w", id, err)
	}
	account := linkAccount(id)
	if mintErr := b.accounts.mint(account, accountSpec{jetStream: true}); mintErr != nil {
		return fmt.Errorf("mint the account of %s: %w", id, mintErr)
	}
	accountPub, err := b.accounts.accountPub(account)
	if err != nil {
		return err
	}
	childDomain := Domain(path)
	option, err := b.accounts.connectOption(account, linkClient(id, childDomain))
	if err != nil {
		return fmt.Errorf("mint this node's own user in the account of %s: %w", id, err)
	}
	conn, err := b.dialSelf("fractal-link-"+string(id), option)
	if err != nil {
		return fmt.Errorf("connect into the account of %s: %w", id, err)
	}
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return fmt.Errorf("reach jetstream in the account of %s: %w", id, err)
	}
	linkCtx, cancel := context.WithCancel(ctx)
	l := &link{
		id:         id,
		path:       path,
		domain:     childDomain,
		accountPub: accountPub,
		conn:       conn,
		js:         js,
		cancel:     cancel,
		identity:   identity,
		admission:  AdmissionAdmitted,
		ever:       ever,
	}
	if streamErr := b.ensureRPCStream(ctx, js, id); streamErr != nil {
		cancel()
		conn.Close()
		return streamErr
	}
	if subErr := b.subscribeLink(ctx, l); subErr != nil {
		cancel()
		conn.Close()
		return subErr
	}
	b.mu.Lock()
	b.links[id] = l
	b.mu.Unlock()
	b.spawn(func() { b.pullRES(linkCtx, l) })
	b.log.InfoContext(ctx, "a link is provisioned", "node", path)
	return nil
}

// subscribeLink subscribes to the subjects of one link. It passes state
// reports, connection loss messages and requests for a full state report of
// the nodes above to the engine, with the Node ID of the link. A heartbeat
// only updates the link's last-heartbeat time.
func (b *Binding) subscribeLink(ctx context.Context, l *link) error {
	handle := func(kind fractal.Kind) func(*nats.Msg) {
		return func(msg *nats.Msg) {
			b.engine.Deliver(ctx, fractalnodeInbound(kind, msg.Data, l.id, nil))
		}
	}
	subjects := []struct {
		subject string
		on      func(*nats.Msg)
	}{
		{SubjectREP, handle(fractal.KindREP)},
		{SubjectLOS, handle(fractal.KindLOS)},
		{SubjectARBR, handle(fractal.KindARBR)},
		{SubjectHB, func(*nats.Msg) { l.touch(b.now()) }},
	}
	var subs []*nats.Subscription
	for _, s := range subjects {
		sub, err := l.conn.Subscribe(s.subject, s.on)
		if err != nil {
			for _, held := range subs {
				_ = held.Unsubscribe() // The caller closes the connection, and the Subscribe error is returned.
			}
			return fmt.Errorf("subscribe %s on the link to %s: %w", s.subject, l.id, err)
		}
		subs = append(subs, sub)
	}
	l.mu.Lock()
	l.subs = subs
	l.mu.Unlock()
	return nil
}

// revokeAccount revokes every user credential issued so far in the account of
// one link. The server closes the open connection of the node below and
// refuses a new one.
func (b *Binding) revokeAccount(id fractal.NodeID) error {
	if err := b.accounts.revoke(linkAccount(id), b.now(), true); err != nil {
		return fmt.Errorf("invalidate the account of %s: %w", id, err)
	}
	return nil
}

// sortNodeIDs sorts Node IDs as text.
func sortNodeIDs(ids []fractal.NodeID) {
	slices.SortFunc(ids, compareNodeID)
}
