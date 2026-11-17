package fractal

// Direction says whether a message travels toward the root node or away from
// it.
type Direction uint8

// The directions a message travels.
const (
	// Up is toward the root node.
	Up Direction = iota + 1
	// Down is away from the root node, toward the nodes that have no node
	// below them.
	Down
)

// String returns "up", "down" or "unknown".
func (d Direction) String() string {
	switch d {
	case Up:
		return "up"
	case Down:
		return "down"
	}
	return "unknown"
}

// Routing says whether a message is addressed to one node or to every node in
// its direction.
type Routing uint8

// How a message is addressed.
const (
	// Unicast means the message is addressed to one node, named in the
	// receiver field.
	Unicast Routing = iota + 1
	// Broadcast means the message is addressed to every node in one
	// direction. The message has no receiver field, and every node that
	// receives it processes it.
	Broadcast
)

// String returns "unicast", "broadcast" or "unknown".
func (r Routing) String() string {
	switch r {
	case Unicast:
		return "unicast"
	case Broadcast:
		return "broadcast"
	}
	return "unknown"
}

// Delivery is the delivery guarantee the transport must give a message.
type Delivery uint8

// The delivery guarantees.
const (
	// BestEffort means the transport may drop the message. The transport
	// neither queues it nor sends it again.
	BestEffort Delivery = iota + 1
	// Durable means the transport delivers the message once and in order.
	// While the next node on the way is unreachable, the transport keeps the
	// message.
	Durable
)

// String returns "best effort", "durable" or "unknown".
func (d Delivery) String() string {
	switch d {
	case BestEffort:
		return "best effort"
	case Durable:
		return "durable"
	}
	return "unknown"
}

// Kind is one of the message types the protocol defines.
type Kind uint8

// The message types.
const (
	// KindREP is the message in which a node reports the state of one of its
	// own interfaces.
	KindREP Kind = iota + 1
	// KindDES asks the receiver to change property values.
	KindDES
	// KindRPC asks the receiver to run a method.
	KindRPC
	// KindRES answers an RPC.
	KindRES
	// KindLOS tells the nodes above the sender that a node directly below the
	// sender disconnected.
	KindLOS
	// KindRBR asks the nodes below the sender for a full state report.
	KindRBR
	// KindAREP is the message in which a node reports the state of one of its
	// own interfaces to the nodes below it.
	KindAREP
	// KindALOS tells the nodes below the sender that the sender lost the node
	// directly above it.
	KindALOS
	// KindARBR asks the nodes above the sender for a full state report of the
	// interfaces they expose downward.
	KindARBR
)

// kindRow is the row of the message table for one Kind.
type kindRow struct {
	name     string
	dir      Direction
	routing  Routing
	delivery Delivery
}

// kinds is the message table of the protocol, indexed by Kind. Index 0 is
// unused.
var kinds = [...]kindRow{
	KindREP:  {"REP", Up, Broadcast, BestEffort},
	KindDES:  {"DES", Down, Unicast, BestEffort},
	KindRPC:  {"RPC", Down, Unicast, Durable},
	KindRES:  {"RES", Up, Unicast, Durable},
	KindLOS:  {"LOS", Up, Broadcast, BestEffort},
	KindRBR:  {"RBR", Down, Broadcast, BestEffort},
	KindAREP: {"AREP", Down, Broadcast, BestEffort},
	KindALOS: {"ALOS", Down, Broadcast, BestEffort},
	KindARBR: {"ARBR", Up, Broadcast, BestEffort},
}

// Valid reports whether k names a message type.
func (k Kind) Valid() bool { return int(k) > 0 && int(k) < len(kinds) }

// String returns the protocol name, such as REP or AREP, or "unknown" for a
// value that is not a message type.
func (k Kind) String() string {
	if !k.Valid() {
		return "unknown"
	}
	return kinds[k].name
}

// Direction returns the direction a message of this type travels, or 0 for a
// value that is not a message type.
func (k Kind) Direction() Direction {
	if !k.Valid() {
		return 0
	}
	return kinds[k].dir
}

// Routing returns whether a message of this type is addressed to one node or to
// every node in its direction, or 0 for a value that is not a message type.
func (k Kind) Routing() Routing {
	if !k.Valid() {
		return 0
	}
	return kinds[k].routing
}

// Delivery returns the delivery guarantee the transport must give a message of
// this type, or 0 for a value that is not a message type.
func (k Kind) Delivery() Delivery {
	if !k.Valid() {
		return 0
	}
	return kinds[k].delivery
}

// Kinds returns every message type, in the order the specification defines
// them. The caller owns the returned slice.
func Kinds() []Kind {
	out := make([]Kind, 0, len(kinds)-1)
	for k := 1; k < len(kinds); k++ {
		out = append(out, Kind(k))
	}
	return out
}

// ParseKind returns the message type with the name s. ok is false when s is not
// such a name. The comparison is case-sensitive.
func ParseKind(s string) (k Kind, ok bool) {
	for i := 1; i < len(kinds); i++ {
		if kinds[i].name == s {
			return Kind(i), true
		}
	}
	return 0, false
}
