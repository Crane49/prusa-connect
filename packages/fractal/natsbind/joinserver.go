package natsbind

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/nats-io/nats.go"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/join"
)

// newParent builds the join.Parent of the sdk, which checks join requests. It
// gives it the admissions file with the nodes that joined, the Authorizer that
// decides whether a node may join, and the issuer that signs the connection
// credentials.
func (b *Binding) newParent(ctx context.Context) error {
	authorizer, err := b.authorizer(ctx)
	if err != nil {
		return err
	}
	parent, err := join.NewParent(ctx, join.ParentConfig{
		Identity:         b.ident,
		Path:             b.path,
		Store:            join.NewFileAdmissionStore(filepath.Join(b.cfg.DataDir, admissionFile)),
		Verifier:         joinwire.ProofVerifier{},
		Authorizer:       authorizer,
		Issuer:           issuer{b: b},
		Revocation:       join.RevocationFunc(b.onRevoke),
		Extensions:       b.cfg.Extensions,
		MaxJoinsInFlight: b.cfg.MaxJoinsInFlight,
		JoinsPerSecond:   b.cfg.JoinsPerSecond,
	})
	if err != nil {
		return fmt.Errorf("build the join endpoint: %w", err)
	}
	b.parent = parent
	return nil
}

// authorizer builds the check that decides whether the sender of a join
// request may use the Node ID it asks for. It is join.TOFU or
// joinwire.TokenAuthorizer, as Config.Verification says, inside
// joinwire.AskFirst when Config.AskFirst is set.
func (b *Binding) authorizer(ctx context.Context) (join.Authorizer, error) {
	var base join.Authorizer = join.TOFU{}
	if b.cfg.Verification == ModeJWT {
		base = joinwire.TokenAuthorizer{Keys: b.cfg.KeySource, Now: b.now}
	}
	if !b.cfg.AskFirst {
		return base, nil
	}
	ask := &joinwire.AskFirst{
		Inner:      base,
		Store:      joinwire.NewFileStore(filepath.Join(b.cfg.DataDir, pendingFile)),
		Now:        b.now,
		MaxPending: b.cfg.MaxPendingNodes,
		Expiry:     b.cfg.PendingExpires,
	}
	if err := ask.Restore(ctx); err != nil {
		return nil, err
	}
	b.askFirst = ask
	return ask, nil
}

// mintBootstrap creates the bootstrap account, the NATS account where join
// requests arrive. It returns the sentinel, the user JWT the server uses for
// a client that connects with no credentials. A node that does not accept
// joins creates neither and returns an empty sentinel.
func (b *Binding) mintBootstrap() (string, error) {
	if !b.cfg.AcceptsChildren {
		return "", nil
	}
	if err := b.accounts.mint(accountBootstrap, accountSpec{}); err != nil {
		return "", fmt.Errorf("mint the bootstrap account: %w", err)
	}
	sentinel, err := b.accounts.issueSentinel(accountBootstrap, bootstrapJoining())
	if err != nil {
		return "", fmt.Errorf("issue the joining-node user: %w", err)
	}
	return sentinel, nil
}

// startJoinEndpoint subscribes to fractal.join and answers every join request.
// Its connection uses the join-service user of the bootstrap account, the only
// user that may subscribe to that subject.
//
// bringUp calls it after restoreLinks. The checks for a Node ID that is
// already used, for a repeated join request and for a revoked node all read
// the restored admissions. An answer before that could let a revoked node join
// again.
func (b *Binding) startJoinEndpoint(ctx context.Context) error {
	option, err := b.accounts.connectOption(accountBootstrap, bootstrapService())
	if err != nil {
		return fmt.Errorf("issue the join-service user: %w", err)
	}
	conn, err := b.dialSelf("fractal-join-"+string(b.cfg.NodeID), option)
	if err != nil {
		return fmt.Errorf("connect the join endpoint: %w", err)
	}
	// Each request gets its own goroutine, so the join bound of the node
	// limits the requests in evaluation and denies the rest with
	// ERR_JOIN_UNAVAILABLE. A serial callback would leave them queued.
	if _, subErr := conn.Subscribe(joinwire.SubjectJoin, func(msg *nats.Msg) {
		b.spawn(func() { b.answerJoin(ctx, msg) })
	}); subErr != nil {
		conn.Close()
		return fmt.Errorf("subscribe %s: %w", joinwire.SubjectJoin, subErr)
	}
	b.mu.Lock()
	b.bootstrap = conn
	b.mu.Unlock()
	b.log.InfoContext(ctx, "the join endpoint is open", "subject", joinwire.SubjectJoin)
	return nil
}

// answerJoin checks one join request and sends the answer. It answers every
// request: when this node cannot process one, it sends a denial.
func (b *Binding) answerJoin(ctx context.Context, msg *nats.Msg) {
	outcome := b.parent.Evaluate(ctx, msg.Data)
	reply := b.replyFor(ctx, outcome)
	payload, err := joinwire.EncodeReply(reply)
	if err != nil {
		b.log.ErrorContext(ctx, "encode a join answer", "error", err)
		return
	}
	if respondErr := msg.Respond(payload); respondErr != nil {
		b.log.WarnContext(ctx, "answer a join request", "error", respondErr)
	}
	b.report()
}

// replyFor converts the outcome of the checks into a joinwire.Reply. For a
// grant it first creates the NATS resources of the link. It denies with
// fractal.CodeJoinRevoked when an operator revoked the node in the meantime,
// and with fractal.CodeJoinUnavailable when the link cannot be built.
func (b *Binding) replyFor(ctx context.Context, outcome join.Outcome) joinwire.Reply {
	if !outcome.Granted {
		denial := outcome.Denial
		switch {
		case denial.Code == joinwire.CodeAwaitingApproval:
			denial.Reason = "waiting for an operator to approve this node"
		case errors.Is(outcome.Cause, joinwire.ErrBadType):
			// ERR_JOIN_REJECTED is not retried, so the server that asked
			// reads this sentence and nothing else.
			denial.Reason = "the join request does not name typ " + joinwire.JoinTyp +
				"; update the server that asks to join"
		}
		b.log.InfoContext(ctx, "a join request was denied",
			"check", outcome.Check, "code", denial.Code, "reason", outcome.Cause)
		return joinwire.Deny(denial)
	}
	id := outcome.Grant.NodePath.Last()
	admission, ok := b.parent.Admission(id)
	if !ok {
		return joinwire.Deny(fractal.JoinDenial{
			Version: fractal.Version,
			Code:    fractal.CodeJoinUnavailable,
			Reason:  "the admission was not recorded",
		})
	}
	err := b.provisionLink(ctx, id, admission.Identity, admission.EverConnected)
	if errors.Is(err, join.ErrRevoked) {
		b.log.InfoContext(ctx, "a join request was denied", "code", fractal.CodeJoinRevoked, "reason", err)
		return joinwire.Deny(fractal.JoinDenial{Version: fractal.Version, Code: fractal.CodeJoinRevoked})
	}
	if err != nil {
		b.log.ErrorContext(ctx, "build the resources of a link", "node", outcome.Grant.NodePath, "error", err)
		return joinwire.Deny(fractal.JoinDenial{
			Version: fractal.Version,
			Code:    fractal.CodeJoinUnavailable,
			Reason:  "the link could not be prepared",
		})
	}
	b.log.InfoContext(ctx, "a node joined", "node", outcome.Grant.NodePath)
	return joinwire.Grant(outcome.Grant, b.cfg.AdvertisedLeafRemote, b.cfg.MaxHeartbeatInterval)
}

// issuer signs the connection credentials of a grant. join.Parent calls it
// only after every check has passed, so this node creates no account for a
// node it denies.
type issuer struct{ b *Binding }

// Issue creates the account of the link and signs a user JWT in it. The JWT
// works only with the public key in identity, and it expires after
// Config.CredentialLifetime.
func (i issuer) Issue(
	_ context.Context,
	id fractal.NodeID,
	identity fractal.Identity,
	path fractal.NodePath,
) (fractal.ConnectionCredentials, fractal.Timestamp, error) {
	b := i.b
	account := linkAccount(id)
	if err := b.accounts.mint(account, accountSpec{jetStream: true}); err != nil {
		return "", 0, fmt.Errorf("mint the account of %s: %w", id, err)
	}
	perms := linkLeaf(id, Domain(b.Path()), Domain(path))
	token, expires, err := b.accounts.issueLeafUser(
		account, string(identity), perms, b.cfg.CredentialLifetime, b.now())
	if err != nil {
		return "", 0, err
	}
	return fractal.ConnectionCredentials(token), fractal.TimestampOf(expires), nil
}

// onRevoke runs when an operator revokes a node below. It revokes the node's
// connection credentials, so the server closes the node's open connection and
// refuses a new one.
//
// The node never connects again. So onRevoke also has the engine delete its
// shadow, and deletes the method invocations that wait for it
// (fractal-requirements.md: the data of a removed node SHOULD be deleted). The
// account stays, because it carries the revocation.
func (b *Binding) onRevoke(ctx context.Context, a join.Admission) error {
	b.provisioning.Lock()
	defer b.provisioning.Unlock()
	l, ok := b.linkOf(a.NodeID)
	if ok {
		// onRevoke takes the stream from the link before the purge, and
		// waits for every publish that already holds it. So the purge
		// deletes every method invocation for this node, and SendDurable
		// refuses one from now on.
		l.publishing.Lock()
		l.mu.Lock()
		js := l.js
		l.js = nil
		l.mu.Unlock()
		l.publishing.Unlock()
		// The purge goes first: the revocation of the account also closes
		// this node's own connection in it. A link restored as revoked has
		// no connection to purge through.
		if js != nil {
			if err := js.PurgeStream(rpcStreamName(a.NodeID), nats.Context(ctx)); err != nil {
				b.log.WarnContext(ctx, "the calls that wait for a revoked node were not deleted",
					"node", a.NodeID, "error", err)
			}
		}
	}
	if err := b.revokeAccount(a.NodeID); err != nil {
		return err
	}
	if ok {
		// The revoked node is disconnected. markLost tells the engine, which
		// sends a connection loss message upward and marks the state of that
		// node, and of every node under it, as out of date.
		b.markLost(ctx, l)
		b.engine.ChildRevoked(ctx, a.NodeID)
		l.close()
	}
	b.recordRevoked(a)
	b.log.InfoContext(ctx, "an admission was revoked", "node", a.NodeID)
	return nil
}
