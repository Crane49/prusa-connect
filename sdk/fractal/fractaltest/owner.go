package fractaltest

import (
	"context"
	"encoding/json"
	"maps"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/node"
)

// ErrNoSuchMethod is the error StateOwner.Invoke returns for a method name
// that Handle did not register. It has fractal.CodeObjectNotFound, so the
// engine answers the caller with that code.
var ErrNoSuchMethod error = &fractal.Error{Code: fractal.CodeObjectNotFound, Message: "the owner runs no such method"}

// Method is the function a StateOwner runs for one method name. It gets the
// parameters of the invocation and returns the result.
type Method func(ctx context.Context, params []json.RawMessage) (json.RawMessage, error)

// StateOwner is a node.Owner that keeps property values in memory and runs the
// methods registered with Handle. The tests and the example in
// examples/fractal-tree use it in place of a device driver.
type StateOwner struct {
	props      fractal.Props
	methods    map[fractal.MethodName]Method
	executions map[fractal.MethodName]int
	refuse     func(props fractal.Props) error
	mu         sync.Mutex
}

// NewStateOwner builds an owner that starts with the property values in props.
// The owner keeps the map and changes it, so the caller must not use props
// after the call.
func NewStateOwner(props fractal.Props) *StateOwner {
	if props == nil {
		props = fractal.Props{}
	}
	return &StateOwner{
		props:      props,
		methods:    make(map[fractal.MethodName]Method),
		executions: make(map[fractal.MethodName]int),
	}
}

// Handle registers fn as the function Invoke runs for the method name.
func (o *StateOwner) Handle(name fractal.MethodName, fn Method) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.methods[name] = fn
}

// Refuse sets a function that Desire calls with every requested property
// change. When fn returns an error, Desire changes no property and returns
// that error. Without such a function Desire accepts every request.
func (o *StateOwner) Refuse(fn func(props fractal.Props) error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.refuse = fn
}

// Set writes one property value. A test calls it where a device would measure
// a new value. Set sends no report. The test reports the change through the
// node.
func (o *StateOwner) Set(name fractal.PropertyName, value json.RawMessage) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.props[name] = value
}

// Get returns one property value, and false when the owner has none for name.
func (o *StateOwner) Get(name fractal.PropertyName) (json.RawMessage, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	value, ok := o.props[name]
	return value, ok
}

// Executions returns how many times Invoke ran the method name. A test uses it
// to show that a repeated invocation did not run the method again.
func (o *StateOwner) Executions(name fractal.MethodName) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.executions[name]
}

// Snapshot returns a copy of every property value and the timestamp 0, so the
// engine stamps the report with the time of its own clock.
func (o *StateOwner) Snapshot(context.Context) (fractal.Props, fractal.Timestamp, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return maps.Clone(o.props), 0, nil
}

// Desire writes every requested property value and returns the properties it
// wrote. When the function set with Refuse returns an error, Desire writes
// nothing and returns that error.
func (o *StateOwner) Desire(_ context.Context, props fractal.Props) (fractal.Props, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.refuse != nil {
		if err := o.refuse(props); err != nil {
			return nil, err
		}
	}
	changed := fractal.Props{}
	for name, value := range props {
		o.props[name] = value
		changed[name] = value
	}
	return changed, nil
}

// Invoke runs the function registered for call.Method and counts the run. It
// returns ErrNoSuchMethod when Handle registered none.
func (o *StateOwner) Invoke(ctx context.Context, call node.Call) (json.RawMessage, error) {
	o.mu.Lock()
	fn, ok := o.methods[call.Method]
	if ok {
		o.executions[call.Method]++
	}
	o.mu.Unlock()
	if !ok {
		return nil, ErrNoSuchMethod
	}
	return fn(ctx, call.Params)
}
