package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// The errors about interface instances.
var (
	// ErrNoOwner is the error Register returns when Instance.Owner is nil.
	ErrNoOwner = errors.New("the interface instance has no owner")
	// ErrDuplicateInstance is the error Register returns when the node already
	// owns an instance with that interface name and instance id.
	ErrDuplicateInstance = errors.New("the node already owns this interface instance")
	// ErrExposureMismatch is the error Register returns when Instance.Exposure
	// and the exposure of Instance.Definition are both set and differ.
	ErrExposureMismatch = errors.New("the exposure of the instance differs from its definition")
)

// Call is one method invocation. The engine passes it to Owner.Invoke and to
// Guard.Allow.
type Call struct {
	// Method is the method name. When the instance has a Definition, the
	// engine has checked that the Definition declares this method.
	Method fractal.MethodName
	// Params are the parameters in the order the method declares them. When
	// the instance has a Definition, the engine has validated them against it.
	Params []json.RawMessage
	// Caller is the Node Path of the node that sent the request.
	Caller fractal.NodePath
	// ID is the id of the request. The owner may log it.
	ID fractal.RPCID
}

// Owner is the application code behind one interface instance.
//
// The engine calls the methods on its own goroutines. A node runs one Invoke
// at a time, so return quickly and do long work on another goroutine.
// Snapshot and Desire can run at the same time as an Invoke and as each
// other, so an owner must protect its own data.
type Owner interface {
	// Snapshot returns the current value of every property of the instance,
	// and the time the owner read those values, never the time a value last
	// changed. Return zero to let the engine use the current time.
	Snapshot(ctx context.Context) (fractal.Props, fractal.Timestamp, error)
	// Desire applies a requested property change and returns the properties
	// whose values changed. The engine reports them. An error rejects the
	// request: change nothing.
	Desire(ctx context.Context, props fractal.Props) (fractal.Props, error)
	// Invoke runs a method and returns its result, nil for a method that
	// returns nothing. ctx ends at the expires timestamp of the request, and
	// the method must stop then.
	//
	// Return a *fractal.Error to choose the code of the answer, such as
	// fractal.CodeDenied. Any other error, and a panic, is answered with
	// fractal.CodeInternal. An owner of an instance with no Definition must
	// answer an unknown method with fractal.CodeObjectNotFound.
	Invoke(ctx context.Context, call Call) (json.RawMessage, error)
}

// Guard is an optional interface of an Owner. An owner implements it to refuse
// a method call before the method runs. The engine calls Allow after it has
// validated the parameters and before it calls Invoke.
type Guard interface {
	// Allow returns nil when the call may run. When it returns an error, the
	// engine does not call Invoke. It answers the caller with the code of a
	// *fractal.Error the error is or wraps, and with fractal.CodeDenied for
	// any other error.
	Allow(ctx context.Context, call Call) error
}

// Instance describes one interface instance to Register.
type Instance struct {
	// Definition describes the properties and methods of the interface. The
	// engine validates every DES, RPC, result and report against it. When it
	// is nil, the owner must check its own input.
	Definition *schema.Definition
	// Owner is the application code behind the instance. It is required.
	Owner Owner
	// Name is the Interface Name.
	Name fractal.InterfaceName
	// Instance is the Interface Instance ID. The pair of name and id is unique
	// within a node.
	Instance fractal.InstanceID
	// Exposure is the set of directions the node reports the instance in.
	// Zero takes the exposure of Definition, and schema.ExposureUpward when
	// there is no Definition.
	Exposure schema.Exposure
}

// instanceKey is the map key of one owned interface instance.
type instanceKey struct {
	name     fractal.InterfaceName
	instance fractal.InstanceID
}

// Interface is the handle Register returns for one interface instance. The
// application calls Report on it when a property value of that instance
// changes.
type Interface struct {
	node     *Node
	owner    Owner
	def      *schema.Definition
	name     fractal.InterfaceName
	instance fractal.InstanceID
	exposure schema.Exposure
	// sent is the timestamp of the last report this node sent for this
	// instance, in unix milliseconds.
	sent atomic.Uint64
	// sendMu is locked from the stamp of a report until it is sent, so reports
	// leave in the order of their stamps. reports counts them under sendMu.
	// The node never calls the owner with sendMu locked.
	sendMu  sync.Mutex
	reports uint64
}

// Name returns the Interface Name.
func (i *Interface) Name() fractal.InterfaceName { return i.name }

// Instance returns the Interface Instance ID.
func (i *Interface) Instance() fractal.InstanceID { return i.instance }

// Definition returns the Definition the instance was registered with, or nil.
func (i *Interface) Definition() *schema.Definition { return i.def }

// Exposure returns the set of directions the node reports the instance in.
func (i *Interface) Exposure() schema.Exposure { return i.exposure }

// Report sends changed property values in the directions the instance is
// exposed in. Call it for every change. props has only the properties that
// changed, and at is the time they had these values. When at is earlier than
// the last report sent, Report sends that last timestamp, so no receiver
// discards the values.
//
// Report returns an error when props does not match the Definition, when the
// upward send fails, and ErrNoNodePath while the node has no Node Path.
func (i *Interface) Report(ctx context.Context, props fractal.Props, at fractal.Timestamp) error {
	return i.node.report(ctx, i, props, at, schema.ExposureBoth)
}

// stamp returns the timestamp for the next report of this instance and stores
// it. It returns the timestamp of the last report when at is earlier.
func (i *Interface) stamp(at fractal.Timestamp) fractal.Timestamp {
	for {
		last := fractal.Timestamp(i.sent.Load())
		if at <= last {
			return last
		}
		if i.sent.CompareAndSwap(uint64(last), uint64(at)) {
			return at
		}
	}
}

// Register adds an interface instance to the node. It returns the handle the
// application calls Report on. It returns ErrNoOwner when inst.Owner is nil,
// ErrDuplicateInstance when the node already owns the same name and instance
// id, ErrExposureMismatch when the exposure of the instance differs from its
// Definition, and an error when the name or the instance id is not valid.
func (n *Node) Register(inst Instance) (*Interface, error) {
	if inst.Owner == nil {
		return nil, fmt.Errorf("%s/%s: %w", inst.Name, inst.Instance, ErrNoOwner)
	}
	if err := inst.Name.Validate(); err != nil {
		return nil, err
	}
	if err := inst.Instance.Validate(); err != nil {
		return nil, err
	}
	exposure, err := exposureOf(inst)
	if err != nil {
		return nil, err
	}
	key := instanceKey{name: inst.Name, instance: inst.Instance}
	handle := &Interface{
		node:     n,
		owner:    inst.Owner,
		def:      inst.Definition,
		name:     inst.Name,
		instance: inst.Instance,
		exposure: exposure,
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.owned[key]; ok {
		return nil, fmt.Errorf("%s/%s: %w", inst.Name, inst.Instance, ErrDuplicateInstance)
	}
	n.owned[key] = handle
	n.ownedOrder = append(n.ownedOrder, handle)
	return handle, nil
}

// exposureOf returns the exposure Register gives an instance: the one it
// states, else the one of its Definition, else upward.
func exposureOf(inst Instance) (schema.Exposure, error) {
	if inst.Exposure != 0 && !inst.Exposure.Valid() {
		return 0, fmt.Errorf("%s/%s: exposure %d: %w",
			inst.Name, inst.Instance, inst.Exposure, schema.ErrUnknownExposure)
	}
	if inst.Definition == nil {
		if inst.Exposure == 0 {
			return schema.ExposureUpward, nil
		}
		return inst.Exposure, nil
	}
	defined := inst.Definition.Exposure()
	if inst.Exposure != 0 && inst.Exposure != defined {
		return 0, fmt.Errorf("%s/%s is %s and its definition is %s: %w",
			inst.Name, inst.Instance, inst.Exposure, defined, ErrExposureMismatch)
	}
	return defined, nil
}

// Interfaces returns the interface instances this node owns, in the order they
// were registered. The caller owns the returned slice.
func (n *Node) Interfaces() []*Interface {
	n.mu.Lock()
	defer n.mu.Unlock()
	list := make([]*Interface, len(n.ownedOrder))
	copy(list, n.ownedOrder)
	return list
}

// lookup finds an owned interface instance by name and instance id.
func (n *Node) lookup(name fractal.InterfaceName, instance fractal.InstanceID) (*Interface, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	handle, ok := n.owned[instanceKey{name: name, instance: instance}]
	return handle, ok
}

// report validates props and sends them in the directions of legs that the
// instance is exposed in. One encoding serves the REP and the AREP.
func (n *Node) report(
	ctx context.Context,
	i *Interface,
	props fractal.Props,
	at fractal.Timestamp,
	legs schema.Exposure,
) error {
	i.sendMu.Lock()
	defer i.sendMu.Unlock()
	return n.reportLocked(ctx, i, props, at, legs)
}

// reportLocked is report for a caller that holds i.sendMu.
func (n *Node) reportLocked(
	ctx context.Context,
	i *Interface,
	props fractal.Props,
	at fractal.Timestamp,
	legs schema.Exposure,
) error {
	i.reports++
	state := n.state.Load()
	if state == nil {
		return ErrNoNodePath
	}
	if i.def != nil {
		if err := i.def.ValidateREP(props); err != nil {
			return fmt.Errorf("report %s/%s: %w", i.name, i.instance, err)
		}
	}
	// Exposure is a set of two bits, upward and downward.
	legs &= i.Exposure()
	up := legs.Up() && state.hasParent
	if !up && !legs.Down() {
		return nil
	}
	payload, err := json.Marshal(fractal.REP{
		Version:   fractal.Version,
		Sender:    state.path,
		Interface: i.name,
		Instance:  i.instance,
		Timestamp: i.stamp(at),
		Props:     props,
	})
	if err != nil {
		return fmt.Errorf("encode rep: %w", err)
	}
	if legs.Down() {
		n.fanOutDown(ctx, fractal.KindAREP, payload)
	}
	if !up {
		return nil
	}
	return n.send(ctx, Dispatch{Kind: fractal.KindREP, Payload: payload})
}
