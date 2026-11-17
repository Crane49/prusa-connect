package bridge

import (
	"encoding/json"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/shadow"
)

// ShadowView is a read-only view of the shadow, the local copy this node
// keeps of the state of the nodes below it.
type ShadowView struct {
	store *shadow.Store
}

// NewShadowView returns a view of one store. With a nil store, every method
// returns an empty result.
func NewShadowView(store *shadow.Store) *ShadowView {
	return &ShadowView{store: store}
}

// ShadowInterface is one interface of one node below, as the shadow has it.
type ShadowInterface struct {
	// Node is the Node Path of the node that owns the interface.
	Node fractal.NodePath `json:"node"`
	// Interface is the Interface Name.
	Interface fractal.InterfaceName `json:"interface"`
	// Instance is the Interface Instance ID.
	Instance fractal.InstanceID `json:"instance"`
	// Status is "EMPTY" before the first state report, "OK" while the values
	// are current, and "STALE" when they may be out of date.
	Status string `json:"status"`
	// UpdatedAt is the timestamp of the last state report, in unix
	// milliseconds. It is zero while Status is "EMPTY".
	UpdatedAt uint64 `json:"updated_at"`
	// Props are the property values as the node reported them.
	Props map[string]json.RawMessage `json:"props,omitempty"`
}

// Counts is the number of interfaces in the shadow, in total and per status.
type Counts struct {
	// Interfaces is the total.
	Interfaces int `json:"interfaces"`
	// Empty, OK and Stale are the number of interfaces with that status.
	Empty int `json:"empty"`
	OK    int `json:"ok"`
	Stale int `json:"stale"`
}

// ShadowNode is one node the shadow holds interfaces for.
type ShadowNode struct {
	// Path is the Node Path of that node.
	Path fractal.NodePath `json:"path"`
	// Connected is false once the interfaces are marked stale, which happens
	// when the connection to that node closes.
	Connected bool `json:"connected"`
	// Interfaces is how many interfaces of that node the shadow holds.
	Interfaces int `json:"interfaces"`
	// Stale is how many of them are marked stale.
	Stale int `json:"stale"`
}

// Nodes returns every node the shadow has interfaces for, in the order the
// store lists them.
func (v *ShadowView) Nodes() []ShadowNode {
	if v == nil || v.store == nil {
		return nil
	}

	var out []ShadowNode

	for path := range v.store.Nodes() {
		one := ShadowNode{Path: path}

		for key := range v.store.Interfaces(path) {
			status, _, ok := v.store.Status(key)
			if !ok {
				continue
			}

			one.Interfaces++

			if status == shadow.StatusStale {
				one.Stale++
			}
		}

		one.Connected = one.Interfaces > 0 && one.Stale == 0
		out = append(out, one)
	}

	return out
}

// Subtree returns every interface the shadow has for one node and for the
// nodes below that node, sorted by node and then by interface.
func (v *ShadowView) Subtree(path fractal.NodePath) []ShadowInterface {
	if v == nil || v.store == nil {
		return nil
	}

	var out []ShadowInterface

	for key := range v.store.Subtree(path) {
		if one, ok := v.read(key); ok {
			out = append(out, one)
		}
	}

	return out
}

// read returns one interface of the shadow. It returns false when the store
// removed the interface after the caller listed the keys.
func (v *ShadowView) read(key shadow.Key) (ShadowInterface, bool) {
	snapshot, ok := v.store.Get(key)
	if !ok {
		return ShadowInterface{}, false
	}

	one := ShadowInterface{
		Node:      key.Node,
		Interface: key.Interface,
		Instance:  key.Instance,
		Status:    snapshot.Status.String(),
		UpdatedAt: uint64(snapshot.UpdatedAt),
	}

	if len(snapshot.Props) > 0 {
		one.Props = make(map[string]json.RawMessage, len(snapshot.Props))
		for name, value := range snapshot.Props {
			one.Props[string(name)] = value
		}
	}

	return one, true
}

// Count returns the number of interfaces in the shadow, in total and per
// status.
func (v *ShadowView) Count() Counts {
	if v == nil || v.store == nil {
		return Counts{}
	}

	var counted Counts

	for path := range v.store.Nodes() {
		for key := range v.store.Interfaces(path) {
			status, _, ok := v.store.Status(key)
			if !ok {
				continue
			}

			counted.Interfaces++

			switch status {
			case shadow.StatusEmpty:
				counted.Empty++
			case shadow.StatusOK:
				counted.OK++
			case shadow.StatusStale:
				counted.Stale++
			}
		}
	}

	return counted
}
