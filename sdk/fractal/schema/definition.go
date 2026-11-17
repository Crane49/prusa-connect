package schema

import (
	"iter"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// Property is the definition of one property of an interface: its name, its
// type, its access mode and whether it may be null.
type Property struct {
	name     fractal.PropertyName
	typ      ValueType
	access   Access
	nullable bool
}

// Name returns the property name.
func (p *Property) Name() fractal.PropertyName { return p.name }

// Type returns the type of the property value.
func (p *Property) Type() ValueType { return p.typ }

// Access returns the access mode: AccessRO or AccessRW.
func (p *Property) Access() Access { return p.access }

// Writable reports whether a DES may change the property. It is true for
// AccessRW.
func (p *Property) Writable() bool { return p.access.Writable() }

// Nullable reports whether the value may be JSON null. A node sends null when
// it has no value for the property.
func (p *Property) Nullable() bool { return p.nullable }

// Check returns nil when raw is a valid value for this property. Otherwise it
// returns the reason, for example ErrWrongType or ErrNotNullable.
func (p *Property) Check(raw []byte) error { return p.typ.Check(raw, p.nullable) }

// Param is the definition of one parameter of a method. A caller may leave an
// optional parameter off the end of the params array, or send it as null.
type Param struct {
	name     fractal.Identifier
	typ      ValueType
	optional bool
}

// Name returns the parameter name.
func (p Param) Name() fractal.Identifier { return p.name }

// Type returns the type of the parameter value.
func (p Param) Type() ValueType { return p.typ }

// Optional reports whether a caller may leave the parameter out.
func (p Param) Optional() bool { return p.optional }

// Method is the definition of one method of an interface: its name, its
// parameters and its return type. An RPC sends the parameters in their order.
type Method struct {
	name     fractal.MethodName
	params   []Param
	result   ValueType
	required int
	returns  bool
}

// Name returns the method name.
func (m *Method) Name() fractal.MethodName { return m.name }

// NumParams returns the number of parameters, with the optional ones.
func (m *Method) NumParams() int { return len(m.params) }

// RequiredParams returns the number of parameters a caller must send. The
// required parameters come first and the optional ones come after them.
func (m *Method) RequiredParams() int { return m.required }

// Param returns the parameter at position i, counting from zero. It panics when
// i is out of range, like a slice index.
func (m *Method) Param(i int) Param { return m.params[i] }

// Params returns an iterator over the parameters, in the order a caller sends
// them.
func (m *Method) Params() iter.Seq[Param] {
	return func(yield func(Param) bool) {
		for _, p := range m.params {
			if !yield(p) {
				return
			}
		}
	}
}

// Result returns the return type of the method. ok is false when the method
// returns nothing. A RES for such a method has no result field.
func (m *Method) Result() (t ValueType, ok bool) {
	if !m.returns {
		return ValueType{}, false
	}
	return m.result, true
}

// Definition is the definition of one interface: its name, its exposure, its
// properties and its methods.
type Definition struct {
	name        fractal.InterfaceName
	exposure    Exposure
	props       map[fractal.PropertyName]*Property
	propOrder   []*Property
	methods     map[fractal.MethodName]*Method
	methodOrder []*Method
}

// Name returns the interface name.
func (d *Definition) Name() fractal.InterfaceName { return d.name }

// Exposure returns the directions in which a node reports every instance of
// the interface.
func (d *Definition) Exposure() Exposure { return d.exposure }

// Property returns the property with the given name. ok is false when the
// interface has no such property.
func (d *Definition) Property(name fractal.PropertyName) (p *Property, ok bool) {
	p, ok = d.props[name]
	return p, ok
}

// Properties returns an iterator over the properties, in the order the
// definition file lists them.
func (d *Definition) Properties() iter.Seq[*Property] {
	return func(yield func(*Property) bool) {
		for _, p := range d.propOrder {
			if !yield(p) {
				return
			}
		}
	}
}

// NumProperties returns the number of properties.
func (d *Definition) NumProperties() int { return len(d.propOrder) }

// Method returns the method with the given name. ok is false when the interface
// has no such method.
func (d *Definition) Method(name fractal.MethodName) (m *Method, ok bool) {
	m, ok = d.methods[name]
	return m, ok
}

// Methods returns an iterator over the methods, in the order the definition
// file lists them.
func (d *Definition) Methods() iter.Seq[*Method] {
	return func(yield func(*Method) bool) {
		for _, m := range d.methodOrder {
			if !yield(m) {
				return
			}
		}
	}
}

// NumMethods returns the number of methods.
func (d *Definition) NumMethods() int { return len(d.methodOrder) }
