package natsbind

import (
	"github.com/nats-io/jwt/v2"
	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// inbox matches the reply subjects of NATS. A client that sends a request
// receives the answer on a subject under _INBOX.
const inbox = "_INBOX.>"

// ackScoped returns the subjects on which a reader acknowledges a message of
// one stream. Both formats of the ack subject carry the stream name as a
// token: "$JS.ACK.<stream>.…" and "$JS.ACK.<domain>.<account hash>.<stream>.…".
// They give no access to the stream API.
func ackScoped(stream string) []string {
	return []string{"$JS.ACK." + stream + ".>", "$JS.ACK.*.*." + stream + ".>"}
}

// bootstrapJoining returns the permissions of the joining-node user in the
// bootstrap account. A node that wants to join may publish a join request and
// subscribe to its reply subject.
//
// Every joining node can subscribe to the reply subjects of the others, so it
// can read their answers. So an answer must never
// have a secret that works for whoever reads it. The connection credentials
// are safe: they work only with a private key the reader does not have.
//
// The user may not publish on a reply subject. Otherwise one joining node
// could send a false answer to another. A request needs no such permission:
// the reply subject is a field of the NATS message, and the node does not
// publish on it.
func bootstrapJoining() jwt.UserPermissionLimits {
	return permissions([]string{joinwire.SubjectJoin}, []string{inbox})
}

// bootstrapService returns the permissions of the join-service user in the
// bootstrap account. This node uses it to subscribe to join requests and to
// publish the answers.
func bootstrapService() jwt.UserPermissionLimits {
	return permissions([]string{inbox}, []string{joinwire.SubjectJoin})
}

// linkClient returns the permissions of this node's own connection in the
// account of one link. They are the opposite of linkLeaf: this node publishes
// downward and subscribes upward.
//
// The stream API subjects name the stream with the method invocations for
// that link, in this node's own domain, so this node can create, update and
// purge it. The rest reads and acknowledges the result stream of the node
// below. linkLeaf has none of the stream API, so the node below cannot use it.
func linkClient(child fractal.NodeID, childDomain string) jwt.UserPermissionLimits {
	rpc := rpcStreamName(child)
	pub := []string{
		"fractal.down.>",
		inbox,
		"$JS.API.STREAM.INFO." + rpc,
		"$JS.API.STREAM.CREATE." + rpc,
		"$JS.API.STREAM.UPDATE." + rpc,
		"$JS.API.STREAM.PURGE." + rpc,
	}
	pub = append(pub, ackScoped(resStreamName)...)
	pub = append(pub, consumerAPIScoped(childDomain, resStreamName)...)
	sub := []string{"fractal.up.>", inbox}
	return permissions(pub, sub)
}

// linkLeaf returns the permissions in the connection credentials of a node
// below. They apply to its leafnode connection. "fractal.up.>" and
// "fractal.down.>" set the direction: the node below may publish only upward
// and subscribe only downward.
//
// The JetStream permissions name one stream each. The node below may publish
// to the consumer API of the stream with its method invocations, in this
// node's domain, and acknowledge its messages. It may subscribe to the
// consumer API and the acknowledgements of its own result stream, so that
// this node can read that stream. A leafnode connection forwards a request
// only when the other end may subscribe to the subject, so without this
// permission the request of this node would get no answer.
func linkLeaf(child fractal.NodeID, ownDomain, childDomain string) jwt.UserPermissionLimits {
	pub := append([]string{"fractal.up.>", inbox}, ackScoped(rpcStreamName(child))...)
	pub = append(pub, consumerAPIScoped(ownDomain, rpcStreamName(child))...)
	sub := append([]string{"fractal.down.>", inbox}, ackScoped(resStreamName)...)
	sub = append(sub, consumerAPIScoped(childDomain, resStreamName)...)
	return permissions(pub, sub)
}
