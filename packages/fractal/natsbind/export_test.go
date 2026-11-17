package natsbind

import (
	"context"
	"fmt"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// QueuedFor returns how many method invocations wait in the stream of the
// node below with this Node ID. No API of the binding shows the stream, so
// the tests read it from the server here. It reads the server's monitoring
// data, because a revoked account lets no new user in.
func QueuedFor(b *Binding, id fractal.NodeID) (uint64, error) {
	b.mu.Lock()
	srv := b.srv
	b.mu.Unlock()
	account, err := b.accounts.accountPub(linkAccount(id))
	if err != nil {
		return 0, err
	}
	info, err := srv.Jsz(&server.JSzOptions{Account: account, Streams: true})
	if err != nil {
		return 0, fmt.Errorf("read the jetstream state: %w", err)
	}
	for _, held := range info.AccountDetails {
		for _, stream := range held.Streams {
			if stream.Name == rpcStreamName(id) {
				return stream.State.Msgs, nil
			}
		}
	}
	return 0, fmt.Errorf("no stream %s in the account of %s", rpcStreamName(id), id)
}

// RevokeBeforeTheReply checks one join request, revokes the node it names and
// only then builds the answer. No API of the binding holds a join request
// between the checks and the link, so the tests take that order here.
func RevokeBeforeTheReply(ctx context.Context, b *Binding, envelope []byte) (joinwire.Reply, error) {
	outcome := b.parent.Evaluate(ctx, envelope)
	if !outcome.Granted {
		return joinwire.Reply{}, fmt.Errorf("the request was denied with %s: %w", outcome.Denial.Code, outcome.Cause)
	}
	if err := b.Revoke(ctx, outcome.Grant.NodePath.Last()); err != nil {
		return joinwire.Reply{}, err
	}
	return b.replyFor(ctx, outcome), nil
}
