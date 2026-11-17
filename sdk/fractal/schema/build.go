package schema

import (
	"strconv"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// Build returns a Registry with the interfaces and enumerations of every file
// added so far. It returns a *LoadError for a type name that is neither a
// built-in type nor a declared enumeration. The caller may add more files and
// call Build again. Each call returns a new Registry.
func (b *Builder) Build() (*Registry, error) {
	r := &Registry{
		defs:  make(map[fractal.InterfaceName]*Definition, len(b.files)),
		enums: make(map[fractal.Identifier]*Enum, len(b.files)),
	}
	if err := b.collectEnums(r); err != nil {
		return nil, err
	}
	for _, f := range b.files {
		if f.doc.Interface == "" {
			continue
		}
		d, err := buildDefinition(r, f)
		if err != nil {
			return nil, err
		}
		if _, dup := r.defs[d.name]; dup {
			return nil, &LoadError{Source: f.source, Where: "interface", Err: ErrDuplicateName}
		}
		r.defs[d.name] = d
		r.defOrder = append(r.defOrder, d)
	}
	return r, nil
}

// collectEnums adds every declared enumeration to the registry. It returns
// ErrDuplicateName when two files declare an enumeration of the same name.
func (b *Builder) collectEnums(r *Registry) error {
	for _, f := range b.files {
		for i, e := range f.doc.Enums {
			name := fractal.Identifier(e.Name)
			if _, dup := r.enums[name]; dup {
				where := "enums[" + strconv.Itoa(i) + "].name"
				return &LoadError{Source: f.source, Where: where, Err: ErrDuplicateName}
			}
			enum := &Enum{name: name, values: append([]string(nil), e.Values...)}
			r.enums[name] = enum
			r.enumOrder = append(r.enumOrder, enum)
		}
	}
	return nil
}

// buildDefinition builds the Definition of the interface in one parsed file. It
// replaces every type name with its ValueType.
func buildDefinition(r *Registry, f loadedFile) (*Definition, error) {
	exposure, _ := parseExposure(f.doc.Exposure)
	d := &Definition{
		name:     fractal.InterfaceName(f.doc.Interface),
		exposure: exposure,
		props:    make(map[fractal.PropertyName]*Property, len(f.doc.Properties)),
		methods:  make(map[fractal.MethodName]*Method, len(f.doc.Methods)),
	}
	for i, p := range f.doc.Properties {
		typ, err := r.resolve(p.Type)
		if err != nil {
			where := "properties[" + strconv.Itoa(i) + "].type"
			return nil, &LoadError{Source: f.source, Where: where, Err: err}
		}
		access, _ := ParseAccess(p.Access)
		prop := &Property{
			name:     fractal.PropertyName(p.Name),
			typ:      typ,
			access:   access,
			nullable: p.Nullable,
		}
		d.props[prop.name] = prop
		d.propOrder = append(d.propOrder, prop)
	}
	for i, m := range f.doc.Methods {
		method, err := buildMethod(r, f.source, i, m)
		if err != nil {
			return nil, err
		}
		d.methods[method.name] = method
		d.methodOrder = append(d.methodOrder, method)
	}
	return d, nil
}

// buildMethod builds a Method from one parsed method. It replaces the type name
// of every parameter and of the return value with its ValueType.
func buildMethod(r *Registry, source string, index int, m methodJSON) (*Method, error) {
	where := "methods[" + strconv.Itoa(index) + "]"
	out := &Method{name: fractal.MethodName(m.Name)}
	for i, p := range m.Params {
		typ, err := r.resolve(p.Type)
		if err != nil {
			at := where + ".params[" + strconv.Itoa(i) + "].type"
			return nil, &LoadError{Source: source, Where: at, Err: err}
		}
		out.params = append(out.params, Param{
			name:     fractal.Identifier(p.Name),
			typ:      typ,
			optional: p.Optional,
		})
		if !p.Optional {
			out.required = i + 1
		}
	}
	if m.Returns == "" {
		return out, nil
	}
	typ, err := r.resolve(m.Returns)
	if err != nil {
		return nil, &LoadError{Source: source, Where: where + ".returns", Err: err}
	}
	out.result = typ
	out.returns = true
	return out, nil
}

// resolve returns the ValueType for a type name. It returns ErrUnknownType for
// a name that is neither a built-in type nor an enumeration in the registry.
func (r *Registry) resolve(name string) (ValueType, error) {
	if kind, ok := ParseType(name); ok {
		return ValueType{kind: kind}, nil
	}
	if enum, ok := r.enums[fractal.Identifier(name)]; ok {
		return ValueType{kind: TypeEnum, enum: enum}, nil
	}
	return ValueType{}, ErrUnknownType
}
