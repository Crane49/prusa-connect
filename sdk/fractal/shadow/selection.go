package shadow

import (
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// Key identifies one shadow interface in the whole network.
type Key struct {
	// Node is the Node Path of the node that has the interface.
	Node fractal.NodePath
	// Interface is the Interface Name, for example "FDMPrinter1".
	Interface fractal.InterfaceName
	// Instance is the Interface Instance ID. In Selection.Interfaces an empty
	// Instance selects every instance of the interface on that node.
	Instance fractal.InstanceID
}

// Validate returns an error when Node, Interface or Instance is not a valid
// name of its kind.
func (k Key) Validate() error {
	if err := k.Node.Validate(); err != nil {
		return fmt.Errorf("node: %w", err)
	}
	if err := k.Interface.Validate(); err != nil {
		return err
	}
	return k.Instance.Validate()
}

// String returns the Node Path, a space, the interface name, a slash and the
// instance id, for example "hq/brno/printer-1 FDMPrinter1/1".
func (k Key) String() string {
	return string(k.Node) + " " + string(k.Interface) + "/" + string(k.Instance)
}

// validateEntry is Validate for an entry of Selection.Interfaces, where an
// empty Instance stands for every instance.
func (k Key) validateEntry() error {
	if k.Instance != "" {
		return k.Validate()
	}
	if err := k.Node.Validate(); err != nil {
		return fmt.Errorf("node: %w", err)
	}
	return k.Interface.Validate()
}

// Selection says which interfaces a Store keeps: named one by one, by
// patterns, or both. An empty Selection is valid, and the Store then keeps
// nothing.
type Selection struct {
	// Interfaces lists the interfaces the node names one by one. New creates
	// each of them with StatusEmpty. An entry with no Instance selects every
	// instance, and the Store creates each on its first report.
	Interfaces []Key
	// Patterns select interfaces by their interface name, on every node, in
	// every instance. The Store creates such an interface on its first
	// report.
	Patterns []fractal.Pattern
}

// Validate returns an error when a Key in Interfaces or a pattern in Patterns
// is not valid. A Key in Interfaces may leave Instance empty.
func (s Selection) Validate() error {
	for _, k := range s.Interfaces {
		if err := k.validateEntry(); err != nil {
			return fmt.Errorf("selected interface %s: %w", k, err)
		}
	}
	for _, p := range s.Patterns {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("selected pattern: %w", err)
		}
	}
	return nil
}

// clone copies both slices.
func (s Selection) clone() Selection {
	out := Selection{}
	if len(s.Interfaces) > 0 {
		out.Interfaces = make([]Key, len(s.Interfaces))
		copy(out.Interfaces, s.Interfaces)
	}
	if len(s.Patterns) > 0 {
		out.Patterns = make([]fractal.Pattern, len(s.Patterns))
		copy(out.Patterns, s.Patterns)
	}
	return out
}
