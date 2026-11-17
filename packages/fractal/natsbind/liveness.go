package natsbind

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// watchLeaves asks the NATS server every PollInterval which leafnode
// connections are open. A leafnode connection is the NATS connection from the
// server of a node below to this server. It tells the engine when a node
// below connects or disconnects.
//
// The node detects a lost node in three ways: the server's disconnect event,
// this poll, and watchHeartbeats. A closed leafnode connection is a disconnect
// however briefly it lasts, and a poll misses a drop that ends before the next
// poll, so the event marks the link lost at once. The poll still decides when
// a link is up, and it settles the state when an event was dropped. A node
// that stops sending heartbeats while its connection stays open has hung, and
// only the timeout sees that.
//
// Every event wakes the poll, so the poll after a disconnect event finds the
// connection to the node above closed at once.
//
// After the server reports a new leafnode connection, the poll runs every
// leafEventPoll until the other end listens on that connection, for at most
// one PollInterval, so a link that comes back is up before it can drop again.
func (b *Binding) watchLeaves(ctx context.Context) {
	b.mu.Lock()
	events := b.leafEvents
	b.mu.Unlock()
	var heard map[uint64]bool
	var opened openedLeaves
	var polled time.Time
	for {
		waitFor(ctx, time.Until(polled.Add(leafEventPoll)))
		if ctx.Err() != nil {
			return
		}
		polled = time.Now()
		hears := b.pollLeaves(ctx, heard)
		opened.settle(heard, hears)
		heard = hears
		wait := b.cfg.PollInterval
		if opened.waiting() {
			wait = min(leafEventPoll, wait)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
		case <-timer.C:
		case msg := <-events:
			if id, open, ok := b.applyLeafEvent(ctx, msg); ok && open {
				opened.add(id, b.cfg.PollInterval)
			}
		}
		timer.Stop()
	}
}

// openedLeaves holds the leafnode connections the server reported open that
// pollLeaves has not yet seen listen, each until one window after its report.
// A connection that never listens keeps the fast polls for one window, and a
// report of another connection does not extend it.
type openedLeaves map[uint64]time.Time

// add records a connection the server reported open, for one window.
func (o *openedLeaves) add(id uint64, window time.Duration) {
	if *o == nil {
		*o = openedLeaves{}
	}
	(*o)[id] = time.Now().Add(window)
}

// settle forgets the connections that two polls in a row saw listen, and the
// ones whose window has passed.
func (o *openedLeaves) settle(heard, hears map[uint64]bool) {
	now := time.Now()
	for id, until := range *o {
		if (heard[id] && hears[id]) || !now.Before(until) {
			delete(*o, id)
		}
	}
}

// waiting reports whether a connection still waits within its window.
func (o *openedLeaves) waiting() bool { return len(*o) > 0 }

// leafEventPoll is the poll interval after a leafnode event. Two polls this
// far apart see every subscription of one write, as pollLeaves explains.
const leafEventPoll = 5 * time.Millisecond

// The subjects of the system account on which the NATS server reports a
// connection that opened or closed, leafnode connections included
// (connectEventSubj and disconnectEventSubj in events.go of nats-server
// v2.14.3). The server sends them because it runs with a system account.
const (
	subjectConnectEvents    = "$SYS.ACCOUNT.*.CONNECT"
	subjectDisconnectEvents = "$SYS.ACCOUNT.*.DISCONNECT"
)

// leafEventBuffer is how many events wait for watchLeaves. The client drops
// an event when the buffer is full, and the next poll settles the state.
const leafEventBuffer = 64

// leafKind is the connection kind the server writes in an event about a
// leafnode connection.
const leafKind = "Leafnode"

// subscribeLeafEvents connects this node to its own server in the system
// account and subscribes to the connect and disconnect events.
func (b *Binding) subscribeLeafEvents() error {
	option, err := b.accounts.connectOption(accountSystem,
		permissions(nil, []string{subjectConnectEvents, subjectDisconnectEvents}))
	if err != nil {
		return fmt.Errorf("mint this node's system user: %w", err)
	}
	conn, err := b.dialSelf("fractal-events-"+string(b.cfg.NodeID), option)
	if err != nil {
		return fmt.Errorf("connect to this node's own server in the system account: %w", err)
	}
	events := make(chan *nats.Msg, leafEventBuffer)
	for _, subject := range []string{subjectConnectEvents, subjectDisconnectEvents} {
		if _, subErr := conn.ChanSubscribe(subject, events); subErr != nil {
			conn.Close()
			return fmt.Errorf("subscribe %s: %w", subject, subErr)
		}
	}
	b.mu.Lock()
	b.events, b.leafEvents = conn, events
	b.mu.Unlock()
	return nil
}

// applyLeafEvent reads one connect or disconnect event of the server. It
// returns the ID of the connection and whether it opened, and ok when the
// connection is a leafnode connection. When the server reports that the
// leafnode connection pollLeaves last saw a node below listen on closed, it
// marks that link lost. An event about an older connection of the link
// changes nothing.
func (b *Binding) applyLeafEvent(ctx context.Context, msg *nats.Msg) (id uint64, open, ok bool) {
	var event server.DisconnectEventMsg
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		b.log.WarnContext(ctx, "read a connection event of the server", "subject", msg.Subject, "error", err)
		return 0, false, false
	}
	if event.Client.Kind != leafKind {
		return 0, false, false
	}
	if !strings.HasSuffix(msg.Subject, ".DISCONNECT") {
		return event.Client.ID, true, true
	}
	for _, l := range b.allLinks() {
		l.mu.Lock()
		closed := l.accountPub == event.Client.Account && l.leafCID == event.Client.ID
		l.mu.Unlock()
		if closed {
			b.markLost(ctx, l)
		}
	}
	return event.Client.ID, false, true
}

// pollLeaves reads the list of leafnode connections once. It marks each link
// connected when its connection is open and the node below listens on it, and
// lost otherwise. It records whether the connection to the node above is open
// and the node above listens on it.
//
// A node listens when this poll and the one before saw the subject every
// release subscribes to on that connection: SubjectREP from the node above,
// SubjectRBR from a node below. The other end sends all its subscriptions in
// one write (initLeafNodeSmapAndSendSubs in leafnode.go of nats-server
// v2.14.3), but this server applies them one at a time, so one poll can see
// part of them. Its read loop starts with a buffer of 512 bytes, so a larger
// write is applied over a few passes, microseconds apart. Two polls are at
// least leafEventPoll apart, so the second sees all of it. heard holds the
// connection IDs the poll before saw with that subject, and pollLeaves returns
// the ones this poll saw.
func (b *Binding) pollLeaves(ctx context.Context, heard map[uint64]bool) map[uint64]bool {
	b.mu.Lock()
	srv := b.srv
	b.mu.Unlock()
	if srv == nil {
		return nil
	}
	leafz, err := srv.Leafz(&server.LeafzOptions{Subscriptions: true})
	if err != nil {
		b.log.WarnContext(ctx, "read the leaf connections", "error", err)
		return heard
	}
	hears := make(map[uint64]bool, len(leafz.Leafs))
	listens := func(leaf *server.LeafInfo, subject string) bool {
		if !slices.ContainsFunc(leaf.Subs, func(sub string) bool { return server.SubjectMatchesFilter(subject, sub) }) {
			return false
		}
		hears[leaf.ID] = true
		return heard[leaf.ID]
	}
	leafOf := make(map[string]*server.LeafInfo, len(leafz.Leafs))
	uplink := false
	for _, leaf := range leafz.Leafs {
		if leaf.IsSpoke {
			// IsSpoke means this server opened the connection, so it is the
			// connection to the node above.
			uplink = listens(leaf, SubjectREP) || uplink
			continue
		}
		leafOf[leaf.Account] = leaf
	}
	b.applyUplink(ctx, uplink)
	for _, l := range b.allLinks() {
		if leaf, open := leafOf[l.accountPub]; open && listens(leaf, SubjectRBR(l.id)) {
			l.mu.Lock()
			l.leafCID = leaf.ID
			l.mu.Unlock()
			b.markConnected(ctx, l)
			continue
		}
		b.markLost(ctx, l)
	}
	return hears
}

// minPendingSweep is the shortest time between two sweeps of the waiting join
// requests, and sweepShare is the share of the expiry one sweep interval is.
// A sweep walks a map of at most MaxPendingNodes entries, so it is cheap, and
// a record is removed within a tenth of the expiry after it lapsed.
const (
	minPendingSweep = time.Second
	sweepShare      = 10
)

// watchPending removes the waiting join requests that are older than
// PendingExpires. A machine that reaches the join endpoint can fill the
// waiting list, and a full list refuses the node its operator is waiting for.
// The next join request removes the lapsed records too, so this timer is for a
// node nobody asks about again.
func (b *Binding) watchPending(ctx context.Context) {
	every := max(b.cfg.PendingExpires/sweepShare, minPendingSweep)
	for ctx.Err() == nil {
		if removed := b.askFirst.Expire(ctx); removed > 0 {
			b.log.InfoContext(ctx, "waiting join requests were removed because they lapsed",
				"removed", removed, "after", b.cfg.PendingExpires.String())
			b.report()
		}
		waitFor(ctx, every)
	}
}

// watchHeartbeats checks every PollInterval when the last heartbeat arrived
// on each link. When a connected link has received no heartbeat for longer
// than livenessTimeout, it marks the link lost.
func (b *Binding) watchHeartbeats(ctx context.Context) {
	for ctx.Err() == nil {
		b.markSilentLinks(ctx)
		waitFor(ctx, b.cfg.PollInterval)
	}
}

// markSilentLinks checks the last-heartbeat time of every link once and marks
// the silent ones lost. A link that has received no heartbeat for longer than
// livenessTimeout stays lost until a heartbeat on it arrives. So the open
// socket pollLeaves sees does not make the node connected again.
func (b *Binding) markSilentLinks(ctx context.Context) {
	timeout := b.cfg.livenessTimeout()
	now := b.now()
	for _, l := range b.allLinks() {
		l.mu.Lock()
		silent := !l.silent && !l.lastHB.IsZero() && now.Sub(l.lastHB) > timeout
		if silent {
			l.silent = true
		}
		connected := l.connected
		l.mu.Unlock()
		if !silent {
			continue
		}
		if connected {
			b.log.InfoContext(ctx, "a node below stopped sending heartbeats", "node", l.path)
		}
		b.markLost(ctx, l)
	}
}

// sendHeartbeats publishes a heartbeat to the node above, at least as often
// as that node required in its grant. A heartbeat is an empty message. It is
// not a Fractal message type, so it has no version.
func (b *Binding) sendHeartbeats(ctx context.Context) {
	for ctx.Err() == nil {
		b.mu.Lock()
		conn, every := b.local, b.heartbeat
		b.mu.Unlock()
		if conn != nil {
			if err := conn.Publish(SubjectHB, nil); err != nil {
				b.log.DebugContext(ctx, "the heartbeat was not published", "error", err)
			}
		}
		waitFor(ctx, every)
	}
}

// allLinks returns every link, revoked ones included.
func (b *Binding) allLinks() []*link {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*link, 0, len(b.links))
	for _, l := range b.links {
		out = append(out, l)
	}
	return out
}

// markConnected records that a node below connected. On the first connection
// of that node it saves the fact in the admissions file. It then calls
// Engine.ChildConnected, and the engine asks the node for a full state
// report. It does nothing for a link that was already connected or is revoked.
//
// It also does nothing while the link is silent. The socket of a node that has
// stopped sending heartbeats stays open, so without this the node would be
// reported connected on every poll and lost again on the next one.
func (b *Binding) markConnected(ctx context.Context, l *link) {
	l.mu.Lock()
	if l.silent {
		l.mu.Unlock()
		return
	}
	was, ever := l.connected, l.ever
	l.connected = true
	l.ever = true
	if l.lastHB.IsZero() {
		l.lastHB = b.now()
	}
	admission := l.admission
	l.mu.Unlock()
	if was || admission != AdmissionAdmitted {
		return
	}
	if !ever && b.parent != nil {
		if err := b.parent.MarkConnected(ctx, l.id); err != nil {
			b.log.WarnContext(ctx, "record the first connection of a node below", "node", l.path, "error", err)
		}
	}
	b.log.InfoContext(ctx, "a link is up", "node", l.path)
	b.engine.ChildConnected(ctx, l.id)
	b.report()
}

// markLost records that a node below disconnected and calls Engine.ChildLost.
// The engine then marks the state it has for that node, and for every node
// under it, as out of date, and sends a connection loss message upward.
//
// A node that joined and never connected causes no loss message. Only the
// binding knows whether a node has ever connected, so the binding applies this
// rule.
func (b *Binding) markLost(ctx context.Context, l *link) {
	l.mu.Lock()
	was, ever, admission := l.connected, l.ever, l.admission
	l.connected = false
	l.mu.Unlock()
	if !was || !ever || admission != AdmissionAdmitted {
		return
	}
	b.log.InfoContext(ctx, "a link is down", "node", l.path)
	b.engine.ChildLost(ctx, l.id)
	b.report()
}

// applyUplink sets Status.Uplink to connected or lost when the leafnode
// connection to the node above opened or closed, and tells the engine. The
// engine hears of a loss only after the connection was open, so a node that
// never reached the node above sends no ALOS.
func (b *Binding) applyUplink(ctx context.Context, up bool) {
	if b.joiner == nil {
		return
	}
	b.mu.Lock()
	moved := b.parentUp != up
	b.parentUp = up
	state := b.uplink.State
	changed := false
	switch {
	case b.permanent != nil:
		// The last leafnode connection of a node refused permanently can still
		// be open, and the operator reads the refusal.
	case up && state != UplinkConnected:
		at := b.now()
		b.uplink.State = UplinkConnected
		b.uplink.ConnectedAt = &at
		b.uplink.Code, b.uplink.Reason = 0, ""
		changed = true
	case !up && state == UplinkConnected:
		b.uplink.State = UplinkLost
		changed = true
	}
	path := b.path
	b.mu.Unlock()
	switch {
	case moved && up:
		b.engine.ParentConnected(ctx)
	case moved:
		b.engine.ParentLost(ctx)
	}
	if !changed {
		return
	}
	if up {
		b.log.InfoContext(ctx, "the connection to the node above is up", "node", path)
	} else {
		b.log.InfoContext(ctx, "the connection to the node above is down", "node", path)
	}
	b.report()
}

// dropUplinkConnection closes the leafnode connection to the node above. The
// NATS server connects again and uses the renewed credentials. A leafnode
// connection authenticates only when it connects, so there is no other way to
// use them.
func (b *Binding) dropUplinkConnection(ctx context.Context) {
	b.mu.Lock()
	srv := b.srv
	b.mu.Unlock()
	if srv == nil {
		return
	}
	leafz, err := srv.Leafz(&server.LeafzOptions{})
	if err != nil {
		b.log.WarnContext(ctx, "read the leaf connections", "error", err)
		return
	}
	for _, leaf := range leafz.Leafs {
		if !leaf.IsSpoke {
			continue
		}
		if dropErr := srv.DisconnectClientByID(leaf.ID); dropErr != nil {
			b.log.WarnContext(ctx, "close the connection to the node above", "error", dropErr)
		}
	}
}

// stopUplink closes the leafnode connection to the node above, and the NATS
// server does not dial it again. A node the node above revoked must not
// retry, and the server would otherwise dial every second
// with credentials that no longer work. The denial stands until an operator
// acts, so only a start of the node dials the node above again.
func (b *Binding) stopUplink(ctx context.Context) {
	b.mu.Lock()
	srv, opts := b.srv, b.srvOpts
	b.mu.Unlock()
	if srv == nil || len(opts.LeafNode.Remotes) == 0 {
		return
	}
	without := opts.Clone()
	without.LeafNode.Remotes = nil
	// The server filled TrustedKeys from TrustedOperators when it started, and
	// it refuses options that set both.
	without.TrustedKeys = nil
	if err := srv.ReloadOptions(without); err != nil {
		b.log.ErrorContext(ctx, "stop the connection to the node above", "error", err)
		return
	}
	b.mu.Lock()
	b.srvOpts = without
	b.mu.Unlock()
}

// heartbeatEvery returns how often this node sends a heartbeat: the shorter of
// Config.HeartbeatInterval and the longest interval the node above allows.
func heartbeatEvery(configured, granted time.Duration) time.Duration {
	if granted > 0 && granted < configured {
		return granted
	}
	return configured
}
