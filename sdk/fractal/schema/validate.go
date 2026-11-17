package schema

import (
	"encoding/json"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// ValidateDES checks the props of a DES against this interface. Every name
// must be a writable property of the interface, and the value must have its
// type. It returns a *PropertyError for one invalid property, not always the
// same one. The node must then reject the whole message.
func (d *Definition) ValidateDES(props fractal.Props) error {
	for name, raw := range props {
		p, ok := d.props[name]
		if !ok {
			return &PropertyError{Interface: d.name, Property: name, Err: ErrUnknownProperty}
		}
		if !p.Writable() {
			return &PropertyError{Interface: d.name, Property: name, Err: ErrReadOnly}
		}
		if err := p.Check(raw); err != nil {
			return &PropertyError{Interface: d.name, Property: name, Err: err}
		}
	}
	return nil
}

// ValidateREP checks the props of a REP against this interface. A read-only
// property is valid in a REP.
func (d *Definition) ValidateREP(props fractal.Props) error {
	for name, raw := range props {
		p, ok := d.props[name]
		if !ok {
			return &PropertyError{Interface: d.name, Property: name, Err: ErrUnknownProperty}
		}
		if err := p.Check(raw); err != nil {
			return &PropertyError{Interface: d.name, Property: name, Err: err}
		}
	}
	return nil
}

// ValidateParams checks the params of an RPC against the method of this
// interface with the given name. params must be in the order the method
// declares its parameters. A caller may leave optional parameters off the end
// or send null for one. Every error is a *ParamError, and the node answers
// the RPC with fractal.CodeInvalidParams.
func (d *Definition) ValidateParams(name fractal.MethodName, params []json.RawMessage) error {
	m, ok := d.methods[name]
	if !ok {
		return &ParamError{Interface: d.name, Method: name, Index: NoParamIndex, Err: ErrUnknownMethod}
	}
	if len(params) < m.required || len(params) > len(m.params) {
		return &ParamError{Interface: d.name, Method: name, Index: NoParamIndex, Err: ErrParamCount}
	}
	for i, raw := range params {
		p := m.params[i]
		if err := p.typ.Check(raw, p.optional); err != nil {
			return &ParamError{
				Interface: d.name,
				Method:    name,
				Param:     p.name,
				Index:     i,
				Err:       err,
			}
		}
	}
	return nil
}

// ValidateResult checks the result field of a RES against the return type of
// the method with the given name. Pass an empty or nil result when the RES has
// no result field. A method that returns nothing must have no result. Any
// other method must have a value of its return type, not null. Every error is
// a *ResultError.
func (d *Definition) ValidateResult(name fractal.MethodName, result json.RawMessage) error {
	m, ok := d.methods[name]
	if !ok {
		return &ResultError{Interface: d.name, Method: name, Err: ErrUnknownMethod}
	}
	if !m.returns {
		if len(result) > 0 {
			return &ResultError{Interface: d.name, Method: name, Err: ErrUnexpectedResult}
		}
		return nil
	}
	if len(result) == 0 {
		return &ResultError{Interface: d.name, Method: name, Err: ErrMissingResult}
	}
	if err := m.result.Check(result, false); err != nil {
		return &ResultError{Interface: d.name, Method: name, Err: err}
	}
	return nil
}

// ValidateDES checks a DES against the definition of the interface the DES
// names. It returns a *PropertyError with ErrUnknownInterface when the registry
// has no such interface.
func (r *Registry) ValidateDES(m fractal.DES) error {
	d, ok := r.defs[m.Interface]
	if !ok {
		return &PropertyError{Interface: m.Interface, Err: ErrUnknownInterface}
	}
	return d.ValidateDES(m.Props)
}

// ValidateREP checks a REP against the definition of the interface the REP
// names. It returns a *PropertyError with ErrUnknownInterface when the registry
// has no such interface.
func (r *Registry) ValidateREP(m fractal.REP) error {
	d, ok := r.defs[m.Interface]
	if !ok {
		return &PropertyError{Interface: m.Interface, Err: ErrUnknownInterface}
	}
	return d.ValidateREP(m.Props)
}

// ValidateRPC checks the method name and the params of an RPC against the
// definition of the interface the RPC names. Every error it returns is a
// *ParamError, also for an unknown interface and an unknown method.
func (r *Registry) ValidateRPC(m fractal.RPC) error {
	d, ok := r.defs[m.Interface]
	if !ok {
		return &ParamError{
			Interface: m.Interface,
			Method:    m.Method,
			Index:     NoParamIndex,
			Err:       ErrUnknownInterface,
		}
	}
	return d.ValidateParams(m.Method, m.Params)
}

// ValidateRES checks the result of a RES against the return type of a method. A
// RES does not name its interface or its method, so the caller passes both.
// It does not check a RES with an error. Every error is a *ResultError.
func (r *Registry) ValidateRES(name fractal.InterfaceName, method fractal.MethodName, m fractal.RES) error {
	d, ok := r.defs[name]
	if !ok {
		return &ResultError{Interface: name, Method: method, Err: ErrUnknownInterface}
	}
	if m.Failed() {
		return nil
	}
	return d.ValidateResult(method, m.Result)
}
