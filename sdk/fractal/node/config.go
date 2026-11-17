package node

import (
	"errors"
	"log/slog"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// The values New uses when a Config field is zero or less.
const (
	// DefaultWorkers is the number of goroutines that handle a DES for this
	// node and a RES for a call this node made. They call Owner.Desire.
	DefaultWorkers = 2
	// DefaultWorkQueue is the number of those messages that can wait for a
	// free worker. The engine drops a message that arrives when the queue is
	// full.
	DefaultWorkQueue = 64
	// DefaultRPCQueue is the number of method invocations that can wait to
	// run. A node runs one method at a time.
	DefaultRPCQueue = 16
	// DefaultMaxPendingCalls is the highest number of calls that can wait for
	// an answer at the same time.
	DefaultMaxPendingCalls = 1024
	// DefaultCallGrace is how long Call still waits for the answer after the
	// request has expired, so a CodeExpired answer has time to arrive.
	DefaultCallGrace = 30 * time.Second
	// DefaultARBRWindow is how long a node collects the ARBR messages from the
	// nodes below before it answers all of them once.
	DefaultARBRWindow = 200 * time.Millisecond
	// DefaultARBRMinInterval is the shortest time between two ARBR messages a
	// node sends of its own.
	DefaultARBRMinInterval = 5 * time.Second
)

// The errors New and the methods of Node return.
var (
	// ErrNoBinding is the error New returns when Config.Binding is nil.
	ErrNoBinding = errors.New("the node has no binding")
	// ErrNoNodePath is the error Call, Desire and Report return while the node
	// has no Node Path. A node with no Node Path sends and processes no
	// message except the join exchange.
	ErrNoNodePath = errors.New("the node holds no node path")
	// ErrAlreadyStarted is the error Start returns when the node is running.
	ErrAlreadyStarted = errors.New("the node is already running")
	// ErrTooManyCalls is the error Call returns when MaxPendingCalls calls
	// already wait for an answer.
	ErrTooManyCalls = errors.New("the node waits for its cap of answers")
	// ErrNotBelow is the error Call and Desire return when the target is not a
	// node below this one. A node can control only the nodes below it.
	ErrNotBelow = errors.New("the target is not a node below this one")
	// ErrSharedShadow is the error New returns when Config.Ancestors and
	// Config.Shadow are the same store.
	ErrSharedShadow = errors.New("the ancestor shadow and the shadow are one store")
)

// Config lists the parts and the limits New builds a Node from. Binding is
// required. Every other field has a default.
type Config struct {
	// Binding is the transport the node sends messages through.
	Binding Binding
	// Admissions tells the node which nodes are directly below it. The default
	// is NoChildren, which fits a leaf of the tree.
	Admissions Admissions
	// Shadow stores the property values the nodes below this one reported. The
	// default is a store with an empty selection, which stores nothing.
	Shadow *shadow.Store
	// Ancestors stores the property values the nodes above this one reported:
	// the ancestor shadow. It must not be the store in Shadow. The default is
	// a store with an empty selection, which stores nothing.
	Ancestors *shadow.Store
	// Executed stores the answer to each method invocation this node ran. The
	// node answers a repeated request from it and does not run the method
	// again. The default is a MemoryDedup.
	Executed ExecutedStore
	// Processed stores the id of each answer this node received for a call it
	// made, so it handles a repeated answer only once. The default is the
	// same MemoryDedup as Executed.
	Processed ProcessedStore
	// Clock gives the node the current time and its timers. The default is
	// SystemClock.
	Clock Clock
	// Logger receives the node's log records. The default discards them.
	Logger *slog.Logger
	// Metrics counts the messages the node handles and drops. The default
	// counts nothing.
	Metrics Metrics
	// Path is this node's Node Path. For a root node it is the node's
	// configured Node ID. Any other node leaves it empty, and its application
	// calls SetPath when the node above accepts its join request.
	Path fractal.NodePath
	// Workers is the number of goroutines that handle a DES for this node and
	// a RES for a call this node made. Zero or less means DefaultWorkers.
	Workers int
	// WorkQueue is the number of those messages that can wait for a free
	// worker. Zero or less means DefaultWorkQueue.
	WorkQueue int
	// RPCQueue is the number of method invocations that can wait to run. Zero
	// or less means DefaultRPCQueue.
	RPCQueue int
	// MaxPendingCalls is the highest number of calls that can wait for an
	// answer at the same time. Zero or less means DefaultMaxPendingCalls.
	MaxPendingCalls int
	// CallGrace is how long Call still waits for the answer after the request
	// has expired. Zero or less means DefaultCallGrace.
	CallGrace time.Duration
	// ARBRWindow is how long the node collects ARBR messages from the nodes
	// below before it answers them once. Zero or less means
	// DefaultARBRWindow.
	ARBRWindow time.Duration
	// ARBRMinInterval is the shortest time between two ARBR messages the node
	// sends of its own. A new Node Path starts it again. Zero or less means
	// DefaultARBRMinInterval.
	ARBRMinInterval time.Duration
}
