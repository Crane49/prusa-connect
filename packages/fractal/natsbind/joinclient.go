package natsbind

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/join"
)

// joinTimeout is how long this node waits for the answer to one join request.
// With no answer the request fails, and the joiner sends it again after a
// growing delay.
const joinTimeout = 15 * time.Second

// ErrNoRelocation means the node above assigned a different Node Path while
// this node was running. The JetStream domain, the streams and the store
// directory are named after the Node Path, and a running node cannot rename
// them, so it has to be started again.
var ErrNoRelocation = errors.New("a new node path needs the node started again")

// newJoiner builds the join.Joiner of the sdk. The joiner sends the first join
// request, sends it again when the denial code allows a retry, and sends it
// again before the connection credentials expire.
func (b *Binding) newJoiner() (*join.Joiner, error) {
	joiner, err := join.NewJoiner(join.JoinerConfig{
		Requester:   join.RequesterFunc(b.request),
		Store:       b.membership,
		NodeID:      b.cfg.NodeID,
		Identity:    b.ident,
		Credential:  b.cfg.JoinCredential,
		Endpoint:    join.Endpoint(b.cfg.ParentAddr),
		HasChildren: b.hasChildren,
		Retry:       retryOf,
		OnGrant:     b.onGrant,

		Extensions:      b.cfg.JoinExtensions,
		CheckExtensions: b.cfg.CheckExtensions,
	})
	if err != nil {
		return nil, fmt.Errorf("build the join exchange: %w", err)
	}
	return joiner, nil
}

// hasChildren reports whether a node below holds an admission that is not
// revoked. It reads the admissions file rather than the links, because Run
// joins before Start builds the links.
func (b *Binding) hasChildren() bool {
	if b.parent == nil {
		return false
	}
	for _, a := range b.parent.Admissions() {
		if !a.Revoked {
			return true
		}
	}
	return false
}

// request signs one join request with this node's identity key, sends it to
// the node above over a new connection, and returns the decoded answer. A node
// refused permanently sends nothing and returns its refusal. The joiner calls
// it for one Join at a time, so the refusal readReply keeps is there before
// the next request. A held denial comes back as a denial, so the joiner ends
// that Join as it ended the first one and does not wait to ask again.
func (b *Binding) request(ctx context.Context, req fractal.JoinRequest) (join.Reply, error) {
	if refusal := b.permanentRefusal(); refusal != nil {
		if denial, ok := errors.AsType[*join.DenialError](refusal); ok {
			return join.Reply{Denial: denial.Denial}, nil
		}
		return join.Reply{}, refusal
	}
	envelope, err := joinwire.Sign(joinwire.Request{
		Version:   req.Version,
		NodeID:    req.NodeID,
		JoinToken: req.Credential,
	}, b.identity)
	if err != nil {
		return join.Reply{}, err
	}
	conn, err := b.dialParent(ctx)
	if err != nil {
		b.joinFailed(ctx, err)
		return join.Reply{}, err
	}
	defer conn.Close()

	askCtx, cancel := context.WithTimeout(ctx, joinTimeout)
	defer cancel()
	msg, err := conn.RequestWithContext(askCtx, joinwire.SubjectJoin, envelope)
	if err != nil {
		err = fmt.Errorf("ask %s to join: %w", b.cfg.ParentAddr, err)
		b.joinFailed(ctx, err)
		return join.Reply{}, err
	}
	reply, err := joinwire.DecodeReply(msg.Data)
	if err != nil {
		return join.Reply{}, err
	}
	return b.readReply(ctx, reply)
}

// retryOf tells the joiner how to retry after a denial code that this binding
// defines. It returns false for every other code, and the joiner then uses the
// retry table of the protocol.
//
// CodeAwaitingApproval means the node above recorded the join request and an
// operator has not approved it yet. It is a sub-code of
// fractal.CodeJoinUnauthorized, and for that code the table says to wait for a
// new join credential. Here no new credential will come, so the node sends the
// request again after a growing delay.
func retryOf(c fractal.Code) (fractal.Retry, bool) {
	if c == joinwire.CodeAwaitingApproval {
		return fractal.RetryWithBackoff, true
	}
	return 0, false
}

// retried reports whether the joiner sends the join request again after a
// denial with the code c. It follows the retry rule of the code, with retryOf
// first. This binding gives the joiner no source of new join credentials, so a
// denial that waits for one ends the joining as RetryNever does.
func retried(c fractal.Code) bool {
	retry, known := retryOf(c)
	if !known {
		retry, known = fractal.JoinRetry(c)
	}
	return !known || retry == fractal.RetryWithBackoff
}

// readReply converts a joinwire.Reply into the join.Reply the joiner uses. It
// records a denial in the Status, and keeps a denial the joiner does not retry
// with holdRefusal. On a revocation it tells the engine and stops the
// connection to the node above.
func (b *Binding) readReply(ctx context.Context, reply joinwire.Reply) (join.Reply, error) {
	if !reply.Granted {
		b.logDenial(ctx, reply.Code, reply.Reason)
		b.denyState(reply.Code, reply.Reason)
		if !retried(reply.Code) {
			b.holdRefusal(ctx, &join.DenialError{Denial: reply.ToDenial()},
				refusal{Code: reply.Code, Reason: reply.Reason})
		}
		if reply.Code == fractal.CodeJoinRevoked {
			b.stopUplink(ctx)
			b.forgetParentExtensions(ctx)
			b.engine.ForgetAncestors(ctx)
		}
		return join.Reply{Denial: reply.ToDenial()}, nil
	}
	grant, extra, err := reply.ToGrant()
	if err != nil {
		return join.Reply{}, err
	}
	// The grant also has the leafnode address and the longest heartbeat
	// interval. The node above sends them in no other message, so the joiner
	// saves them with the Node Path and the credentials.
	encoded, err := json.Marshal(extra)
	if err != nil {
		return join.Reply{}, fmt.Errorf("encode the leafnode address and the heartbeat interval: %w", err)
	}
	return join.Reply{Granted: true, Grant: grant, Extra: encoded}, nil
}

// dialParent connects to the client listener of the node above, to send a
// join request. It sends no credentials. The server above puts such a client
// in the bootstrap account, as the joining-node user.
func (b *Binding) dialParent(ctx context.Context) (*nats.Conn, error) {
	opts := []nats.Option{
		nats.Name("fractal-joining-" + string(b.cfg.NodeID)),
		nats.Timeout(joinTimeout),
		nats.MaxReconnects(0),
		nats.SetCustomDialer(contextDialer{ctx: ctx}),
	}
	if b.cfg.TLS != nil {
		opts = append(opts, nats.Secure(ClientTLS(b.anchors, "")))
	}
	conn, err := nats.Connect(joinURL(b.cfg.ParentAddr, b.cfg.TLS != nil), opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", b.cfg.ParentAddr, err)
	}
	return conn, nil
}

// contextDialer dials within a context and closes the connection when the
// context ends. The NATS client takes no context, and would otherwise wait
// its whole timeout for a node above that accepts and never answers.
type contextDialer struct {
	ctx context.Context
}

func (d contextDialer) Dial(network, address string) (net.Conn, error) {
	// nats.Timeout bounds only its own dialer, so a dropped SYN would wait for the kernel.
	dialer := net.Dialer{Timeout: joinTimeout}
	conn, err := dialer.DialContext(d.ctx, network, address)
	if err != nil {
		return nil, err
	}
	// Closing is how a cancelled join ends the connection; its error tells nothing.
	return &closeOnDone{Conn: conn, stop: context.AfterFunc(d.ctx, func() { _ = conn.Close() })}, nil
}

// closeOnDone is a connection that a context closes when it ends.
type closeOnDone struct {
	net.Conn

	stop func() bool
}

func (c *closeOnDone) Close() error {
	c.stop()
	return c.Conn.Close()
}

// joinURL returns the address of the node above as a URL the NATS client
// accepts. An operator writes that address either as a host and a port, such as
// "hq:4222", or with the scheme in front, such as "nats://hq:4222". Both forms
// read as an address to a person, so both are accepted here. An address that
// already names a scheme is returned unchanged, because prefixing it again
// would produce "nats://nats://hq:4222", which resolves to the host "nats".
//
// secure selects the scheme for an address with none: "tls" when this node has
// TLS material and "nats" without it.
func joinURL(addr string, secure bool) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	if secure {
		return "tls://" + addr
	}
	return "nats://" + addr
}

// joinFailed records that one join request did not reach the node above, and
// writes the reason to the log. Without it a node whose address is wrong sends
// the request again for ever and reports nothing: the joiner retries inside
// itself, and this node's status stays at the reading it had before the first
// request.
func (b *Binding) joinFailed(ctx context.Context, err error) {
	b.log.ErrorContext(ctx, "this node could not ask the node above to join",
		"address", b.cfg.ParentAddr, "node", b.cfg.NodeID, "error", err)
	b.mu.Lock()
	// The newest reason replaces the one before it, whatever state the
	// connection is in. A node denied once and then unable to reach the node
	// above would otherwise keep showing the denial, and an operator reading
	// the links page would look for the wrong thing.
	if b.uplink.State != UplinkConnected {
		b.uplink.State = UplinkJoining
		b.uplink.Code = 0
		b.uplink.Reason = err.Error()
		b.uplink.Refused = false
	}
	b.mu.Unlock()
	b.report()
}

// onGrant runs after every granted join request. It records the Node Path,
// the connection credentials, the leafnode address and the heartbeat interval
// from the grant.
//
// A renewal has the same Node Path and new credentials. A leafnode connection
// authenticates only when it connects, so onGrant closes the connection, and
// the server connects again with the new credentials. A grant with a
// different Node Path while the node runs is logged and sent to Relocated.
func (b *Binding) onGrant(ctx context.Context, d join.Decision) {
	b.mu.Lock()
	was, running := b.path, b.running
	b.mu.Unlock()
	b.adopt(d.Membership)
	path := d.Membership.NodePath

	switch {
	case !running:
		// A node drops the caches of its former parent before it brings up
		// the connection, so Start refuses while the engine could not take
		// the path.
		refused := b.tellPath(ctx, path)
		b.mu.Lock()
		b.pathRefused = refused
		b.mu.Unlock()
	case was != path:
		b.log.ErrorContext(ctx, "this node was granted a different node path",
			"was", was, "is", path, "error", ErrNoRelocation)
		b.relocated(path)
	default:
		_ = b.tellPath(ctx, path) // logged; a running node keeps its connection under the same path
		b.dropUplinkConnection(ctx)
	}
	b.report()
}

// adopt copies the Node Path, the connection credentials, the leafnode address
// and the heartbeat interval of a membership into the Binding.
func (b *Binding) adopt(m join.Membership) {
	var extra joinwire.Extra
	if len(m.Binding) > 0 {
		_ = json.Unmarshal(m.Binding, &extra) // On an error extra stays zero and the current values stay.
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.path = m.NodePath
	b.leafToken = string(m.Credentials)
	b.extensions = maps.Clone(m.Extensions)
	if extra.LeafRemote != "" {
		b.leafRemot = extra.LeafRemote
	}
	if granted := extra.HeartbeatInterval(); granted > 0 {
		b.heartbeat = heartbeatEvery(b.cfg.HeartbeatInterval, granted)
	}
	if b.uplink.State == UplinkJoining || b.uplink.State == UplinkDenied {
		b.uplink.State = UplinkJoining
		b.uplink.Code, b.uplink.Reason, b.uplink.Refused = 0, "", false
	}
}

// tellPath passes this node's Node Path to the engine, when it implements
// PathSetter, and to the join endpoint. It returns the error of the engine.
func (b *Binding) tellPath(ctx context.Context, path fractal.NodePath) error {
	var refused error
	if setter, ok := b.engine.(PathSetter); ok {
		if err := setter.SetPath(ctx, path); err != nil {
			b.log.ErrorContext(ctx, "give the engine this node's path", "path", path, "error", err)
			refused = fmt.Errorf("give the engine this node's path: %w", err)
		}
	}
	if b.parent != nil {
		if err := b.parent.SetPath(path); err != nil {
			b.log.ErrorContext(ctx, "give the join endpoint this node's path", "path", path, "error", err)
		}
	}
	return refused
}

// logDenial logs a denial. A wait for an operator and a busy node above need
// no action on this node, so they are warnings.
func (b *Binding) logDenial(ctx context.Context, code fractal.Code, reason string) {
	level := slog.LevelError
	if code == joinwire.CodeAwaitingApproval || code.Is(fractal.CodeJoinUnavailable) {
		level = slog.LevelWarn
	}
	b.log.Log(ctx, level, "the node above denied this node", "code", code, "reason", reason)
}

// denyState records a denial in Status.Uplink, so an operator sees the code
// and the reason the node above sent.
func (b *Binding) denyState(code fractal.Code, reason string) {
	b.mu.Lock()
	b.uplink.State = UplinkDenied
	b.uplink.Code = code
	b.uplink.Reason = reason
	b.uplink.Refused = false
	b.mu.Unlock()
	b.report()
}

// denied records the denial in err, when Join failed with a join.DenialError,
// and the refusal, with no code, when this node refused the grant. It keeps
// both with holdRefusal.
func (b *Binding) denied(ctx context.Context, err error) {
	var denial *join.DenialError
	switch {
	case errors.As(err, &denial):
		b.denyState(denial.Denial.Code, denial.Denial.Reason)
		b.holdRefusal(ctx, err, refusal{Code: denial.Denial.Code, Reason: denial.Denial.Reason})
	case refusedGrant(err):
		b.mu.Lock()
		b.uplink.State = UplinkDenied
		b.uplink.Code = 0
		b.uplink.Reason = refusedReason(err)
		b.uplink.Refused = true
		b.mu.Unlock()
		b.report()
		b.holdRefusal(ctx, err, refusal{Reason: refusedReason(err), Refused: true})
	}
}

// refusedReason is the Status.Uplink reason of a grant this node refused.
func refusedReason(err error) string {
	return "this node refused the grant of the node above: " + err.Error()
}

// relocated sends the new Node Path to the Relocated channel. It does not
// block: when the channel already has a path, it drops the new one.
func (b *Binding) relocated(path fractal.NodePath) {
	select {
	case b.relocate <- path:
	default:
	}
}

// Relocated returns a channel that receives a Node Path when the node above
// assigns one that differs from the path this node runs under. The JetStream
// domain, the streams and the store directory are named after the Node Path.
// The code that owns the Binding has to close it and start a new one, which
// creates them again under the new path.
func (b *Binding) Relocated() <-chan fractal.NodePath { return b.relocate }

// signLeafNonce authenticates the leafnode connection to the node above. It
// returns this node's connection credentials and its signature over the nonce,
// the random value the server above sends. The NATS server calls it at every
// connect, so the next connect after a renewal uses the new credentials.
func (b *Binding) signLeafNonce(nonce []byte) (string, []byte, error) {
	b.mu.Lock()
	token := b.leafToken
	b.mu.Unlock()
	if token == "" {
		return "", nil, errors.New("this node holds no connection credentials")
	}
	sig, err := b.identity.Sign(nonce)
	if err != nil {
		return "", nil, fmt.Errorf("sign the server nonce: %w", err)
	}
	return token, sig, nil
}

// ParentExtension returns the join data the node above sent for one protocol
// extension in its last grant. It is false when the grant carried none, and
// after the node above answered ERR_JOIN_REVOKED. A grant with a new Node Path
// replaces the data of the former parent, so none of it is kept.
func (b *Binding) ParentExtension(name fractal.Identifier) (json.RawMessage, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.extensions[name]
	return data, ok
}

// forgetParentExtensions drops the join data of the node above, in memory and
// in the saved membership, so a restart does not bring it back. A node the
// parent revoked must not use what that parent offered.
func (b *Binding) forgetParentExtensions(ctx context.Context) {
	b.mu.Lock()
	b.extensions = nil
	b.mu.Unlock()
	held, joined, err := b.membership.LoadMembership(ctx)
	if err != nil || !joined || len(held.Extensions) == 0 {
		if err != nil {
			b.log.ErrorContext(ctx, "read the membership to drop the join data of the node above", "error", err)
		}
		return
	}
	held.Extensions = nil
	if saveErr := b.membership.SaveMembership(ctx, held); saveErr != nil {
		b.log.ErrorContext(ctx, "drop the join data of the node above", "error", saveErr)
	}
}
