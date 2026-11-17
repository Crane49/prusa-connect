package resourcehost

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/linkapi"
)

// pathSeparator divides the segments of a node path. The last segment is the
// node the path names and the one before it is the server above that node.
const pathSeparator = "/"

// linkNodes records, for each link instance, the node ids of the servers that
// link connects.
//
// A link may make a call in the name of another server, written node:<id>. No
// other kind of instance may name a principal the host did not give it. A
// link can be a module from a third party, so the host cannot trust the id in
// a call. The host keeps the ids itself and accepts a node:<id> call only from
// the instance that reported that id.
//
// The ids come from the last status the link sent with link.changed: the
// server above this one, which is the segment before the last in the node path
// of the link, and every server below it. A link that has sent no status has
// no ids, so the host refuses every node:<id> call from it until the first
// status arrives.
//
// One id belongs to one link while the host runs that link. When a second
// link sends a status with an id that a running link has, the host refuses
// the whole status. When the host stops a link, its ids are free again, so an
// uninstalled module does not block an id.
//
// A deployment can declare only one link instance: ref.Kind.SoleInstance says
// so, and the loader and the resource API both refuse a second one. The rule
// here is a second protection behind that one.
type linkNodes struct {
	mu sync.Mutex
	// held maps each link to the ids in the last status the host accepted
	// from it.
	held map[ref.Ref]map[string]struct{}
	// by maps each id to the link that has it. It is the reverse of held, so
	// report does not search every instance.
	by map[string]ref.Ref
}

// holds reports whether this instance reported this node id.
func (n *linkNodes) holds(instance ref.Ref, id string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	_, ok := n.held[instance][id]

	return ok
}

// report records the ids of one link's status. It returns the ids the link may
// not have, which are the ids another running link reported. When it returns
// any, it records nothing, and the link keeps the ids of its previous status.
//
// supervised is the set of instances the host is running. report frees the ids
// of a link that is not in the set, so an uninstalled module does not block an
// id until the process ends. The caller passes the whole set, so report never
// takes the lock of the host while it has its own lock.
func (n *linkNodes) report(instance ref.Ref, ids []string, supervised map[ref.Ref]struct{}) []string {
	n.mu.Lock()
	defer n.mu.Unlock()

	var taken []string

	for _, id := range ids {
		holder, held := n.by[id]
		if _, live := supervised[holder]; held && holder != instance && live {
			taken = append(taken, id)
		}
	}

	if len(taken) > 0 {
		return taken
	}

	n.forgetLocked(instance)
	n.pruneLocked(supervised)

	if n.held == nil {
		n.held = map[ref.Ref]map[string]struct{}{}
		n.by = map[string]ref.Ref{}
	}

	held := make(map[string]struct{}, len(ids))

	for _, id := range ids {
		// Another link that still has this id is a link the host stopped.
		// The loop above refuses the status otherwise. Remove the id from
		// that link, so one id is in the set of one instance.
		delete(n.held[n.by[id]], id)

		held[id] = struct{}{}
		n.by[id] = instance
	}

	n.held[instance] = held

	return nil
}

// forget removes every id of one instance. The host calls it when a module
// starts: the new process has sent no status yet, and the ids of the previous
// process do not apply to it.
func (n *linkNodes) forget(instance ref.Ref) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.forgetLocked(instance)
}

func (n *linkNodes) forgetLocked(instance ref.Ref) {
	for id := range n.held[instance] {
		delete(n.by, id)
	}

	delete(n.held, instance)
}

// pruneLocked removes the records of every link the host does not run any
// more, so the maps do not grow with every link a deployment ever ran. The
// caller holds n.mu.
func (n *linkNodes) pruneLocked(supervised map[ref.Ref]struct{}) {
	for instance := range n.held {
		if _, live := supervised[instance]; !live {
			n.forgetLocked(instance)
		}
	}
}

// claimNodes records the servers that one link's status lists.
//
// When the status lists a server that another running link connects,
// claimNodes returns an error and the link keeps the ids of its previous
// status. It also fails the instance that sent the status. Two links with one
// node id is a configuration error that an operator has to correct, and the
// host must not relay calls for a module that reports wrong servers.
func (h *Host) claimNodes(ctx context.Context, instance ref.Ref, status linkapi.Status) error {
	taken := h.flights.nodes.report(instance, nodesInReading(status), h.supervised())
	if len(taken) == 0 {
		return nil
	}

	err := fmt.Errorf("%w: %s reports %s, and another link already answers for %s",
		linkapi.ErrInvalidReading, instance, strings.Join(taken, ", "), plural(taken))

	h.logger().WarnContext(ctx, "a link reported a server another link answers for",
		"resource", instance.String(), "nodes", strings.Join(taken, ","), "error", err.Error())

	// The supervisor is nil only in a test that builds an instance record
	// itself. A real start stores the record after it builds the supervisor.
	if running, ok := h.Instance(instance); ok && running != nil {
		running.Reporter().Permanent(err)
	}

	return err
}

// plural returns `it` for one id and `them` for several, for the error message.
func plural(ids []string) string {
	if len(ids) == 1 {
		return "it"
	}

	return "them"
}

// supervised returns the set of instances the host is running. It reads
// the set under the lock of the host and returns before linkNodes takes its
// own lock, so the two locks are never held at the same time.
func (h *Host) supervised() map[ref.Ref]struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make(map[ref.Ref]struct{}, len(h.running))
	for instance := range h.running {
		out[instance] = struct{}{}
	}

	return out
}

// logger returns the log of the host, or a log that discards every line when
// the host has none, as in a test.
func (h *Host) logger() *slog.Logger {
	if h.cfg.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}

	return h.cfg.Logger
}

// nodesInReading returns every node id in one status: the server above this
// one and every server below it. It leaves out the NodeID of the status,
// because that id names this server.
func nodesInReading(status linkapi.Status) []string {
	ids := make([]string, 0, len(status.Nodes)+1)

	if above := nodeAbove(status.NodePath); above != "" {
		ids = append(ids, above)
	}

	for _, node := range status.Nodes {
		if node.ID != "" {
			ids = append(ids, node.ID.String())
		}
	}

	return ids
}

// nodeAbove returns the id of the server above the server a path names. It
// returns an empty string for a server at the top of its tree.
//
// It splits the path at the separator sdk/fractal uses. For a link whose
// protocol writes a path in another way it finds no server above, and the host
// then refuses every call that link relays for that server.
func nodeAbove(path linkapi.NodePath) string {
	segments := strings.Split(path.String(), pathSeparator)
	if len(segments) < 2 { //nolint:mnd // a path of one segment has nothing above it
		return ""
	}

	return segments[len(segments)-2]
}
