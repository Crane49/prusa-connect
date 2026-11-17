package schema

import (
	"iter"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// Registry has the loaded interface definitions and the enumerations they use.
// Nothing writes to it after Build returns it, so it is safe for concurrent
// reads.
type Registry struct {
	defs      map[fractal.InterfaceName]*Definition
	defOrder  []*Definition
	enums     map[fractal.Identifier]*Enum
	enumOrder []*Enum
}

// Definition returns the definition of the interface with the given name. ok is
// false when the registry has no such interface.
func (r *Registry) Definition(name fractal.InterfaceName) (d *Definition, ok bool) {
	d, ok = r.defs[name]
	return d, ok
}

// Definitions returns an iterator over the interfaces, in the order their files
// were added to the Builder.
func (r *Registry) Definitions() iter.Seq[*Definition] {
	return func(yield func(*Definition) bool) {
		for _, d := range r.defOrder {
			if !yield(d) {
				return
			}
		}
	}
}

// NumDefinitions returns the number of interfaces.
func (r *Registry) NumDefinitions() int { return len(r.defOrder) }

// Enum returns the enumeration with the given name. ok is false when the
// registry has no such enumeration.
func (r *Registry) Enum(name fractal.Identifier) (e *Enum, ok bool) {
	e, ok = r.enums[name]
	return e, ok
}

// Enums returns an iterator over the enumerations, in the order their files
// were added to the Builder.
func (r *Registry) Enums() iter.Seq[*Enum] {
	return func(yield func(*Enum) bool) {
		for _, e := range r.enumOrder {
			if !yield(e) {
				return
			}
		}
	}
}

// NumEnums returns the number of enumerations.
func (r *Registry) NumEnums() int { return len(r.enumOrder) }
