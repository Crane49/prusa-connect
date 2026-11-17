package natsbind

import (
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// The subjects on which a node publishes to the node above it. They have no
// Node ID: a node joins at most one other node, so an upward message has one
// possible receiver.
const (
	// SubjectREP is the subject of a state report.
	SubjectREP = "fractal.up.rep"
	// SubjectLOS is the subject of a connection loss message.
	SubjectLOS = "fractal.up.los"
	// SubjectRES is the subject of a method result. A JetStream stream stores
	// the messages on it.
	SubjectRES = "fractal.up.res"
	// SubjectHB is the subject of the heartbeat. A heartbeat is an empty
	// message and not a Fractal message type.
	SubjectHB = "fractal.up.hb"
	// SubjectARBR is the subject of a request for a full state report of the
	// nodes above.
	SubjectARBR = "fractal.up.arbr"
)

// The prefixes of the subjects on which a node publishes to a node below it.
// The Node ID of that node follows the prefix. Without the Node ID, the
// subscription of a node to the messages from above would also match the
// messages it publishes downward.
const (
	prefixRPC  = "fractal.down.rpc."
	prefixDES  = "fractal.down.des."
	prefixRBR  = "fractal.down.rbr."
	prefixAREP = "fractal.down.arep."
	prefixALOS = "fractal.down.alos."
)

// SubjectRPC returns the subject of a method invocation for the node below
// with this Node ID.
func SubjectRPC(id fractal.NodeID) string { return prefixRPC + string(id) }

// SubjectDES returns the subject of a requested property change for the node
// below with this Node ID.
func SubjectDES(id fractal.NodeID) string { return prefixDES + string(id) }

// SubjectRBR returns the subject of a request for a full state report for the
// node below with this Node ID.
func SubjectRBR(id fractal.NodeID) string { return prefixRBR + string(id) }

// SubjectAREP returns the subject of a state report of a node above, for the
// node below with this Node ID.
func SubjectAREP(id fractal.NodeID) string { return prefixAREP + string(id) }

// SubjectALOS returns the subject of the message that a node lost the node
// above it, for the node below with this Node ID.
func SubjectALOS(id fractal.NodeID) string { return prefixALOS + string(id) }

// The names of the JetStream stream and buckets in a node's own account.
const (
	// resStreamName is the work queue stream with the method results that wait
	// for the node above.
	resStreamName = "fractal-res"
	// execBucket is the key-value bucket with the result of each method
	// invocation this node executed. When the same invocation arrives again,
	// the node sends the stored result and does not execute the method again.
	execBucket = "fractal-exec"
	// procBucket is the key-value bucket with the id of each result this node
	// has already processed.
	procBucket = "fractal-proc"
)

// rpcStreamName returns the name of the work queue stream with the method
// invocations for the node below with this Node ID.
func rpcStreamName(id fractal.NodeID) string { return "fractal-rpc-" + string(id) }

// rpcConsumerName returns the name of the durable pull consumer that the node
// with this Node ID creates on the stream the node above has for it.
func rpcConsumerName(id fractal.NodeID) string { return "fractal-rpc-" + string(id) }

// resConsumerName returns the name of the durable pull consumer this node
// creates on the result stream of the node below with this Node ID.
func resConsumerName(id fractal.NodeID) string { return "fractal-res-" + string(id) }

// The names of the NATS accounts on a node's server. Only that server uses the
// names, and no message has them. A NATS account is an isolated set of
// subjects.
const (
	// accountSystem is the account on which the NATS server publishes its own
	// events.
	accountSystem = "$SYS"
	// accountLocal is the node's own account. It has the leafnode connection
	// to the node above, the node's streams and buckets, and the subjects on
	// which the node publishes upward and receives from above.
	accountLocal = "local"
	// accountBootstrap is the account where join requests arrive. A node that
	// wants to join has no credentials yet, so it can enter no other account.
	accountBootstrap = "bootstrap"
)

// linkAccount returns the name of the account of the link to one node below.
// Each link has its own account, so a node below cannot read the subjects or
// the streams of another node below.
func linkAccount(id fractal.NodeID) string { return "link-" + string(id) }

// Domain returns the JetStream domain of a node. It replaces every slash of
// the Node Path with an underscore and doubles every underscore in a Node ID.
//
// The doubling gives two different Node Paths two different domains. A Node ID
// may have an underscore, so "hq/a/b" and "hq/a_b" can both exist in one tree,
// and without the doubling both would become "hq_a_b". A node's JetStream API
// is addressed over a leafnode connection only by its domain. Two nodes with
// one domain would read each other's streams.
func Domain(path fractal.NodePath) string {
	s := string(path)
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		switch s[i] {
		case '/':
			b.WriteByte('_')
		case '_':
			b.WriteString("__")
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// jsAPIPrefix returns the subject prefix of the JetStream API of one domain,
// as a node uses it over a leafnode connection.
func jsAPIPrefix(domain string) string { return "$JS." + domain + ".API" }

// consumerAPIScoped returns the JetStream API subjects a node needs to create,
// inspect, read and delete a pull consumer of one stream in one domain. The
// permissions of a link list only these subjects, so a link gives access to no
// other stream and to no other part of the JetStream API.
func consumerAPIScoped(domain, stream string) []string {
	api := jsAPIPrefix(domain) + ".CONSUMER."
	return []string{
		api + "CREATE." + stream,
		api + "CREATE." + stream + ".>",
		api + "DURABLE.CREATE." + stream + ".>",
		api + "INFO." + stream + ".>",
		api + "MSG.NEXT." + stream + ".>",
		api + "DELETE." + stream + ".>",
		api + "LIST." + stream,
		api + "NAMES." + stream,
	}
}
