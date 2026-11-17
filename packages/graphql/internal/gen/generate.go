// Package gen turns the core's committed OpenAPI document into the GraphQL
// schema this module serves.
//
// It is a build-time program and never runs inside the module. The module
// carries what this wrote.
package gen

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/prusa3d/graphql/internal/schema"
)

// CoreDocument is where the core's committed document sits, relative to the
// repository root. It is written into the schema header, so a reader knows
// what to regenerate against.
const CoreDocument = "internal/transport/http/httpapi/openapi.json"

// Generate reads the core document and produces the schema model.
//
// It refuses the document rather than writing a schema whenever the two tables
// in bindings.go no longer describe it: an operation neither served nor
// excused, a property on a served schema neither served nor excused, or a name
// the tables use that the document no longer carries. The refusal fires when
// the core moves, rather than when a client notices.
func Generate(document []byte, source string) (schema.Model, error) {
	var read core
	if err := json.Unmarshal(document, &read); err != nil {
		return schema.Model{}, fmt.Errorf("read the core document: %w", err)
	}

	made := &maker{core: read, objects: map[string]objectBinding{}}

	for _, binding := range objects {
		made.objects[binding.Schema] = binding
	}

	model := schema.Model{
		Source:      source,
		CoreVersion: read.Info.Version,
		Scalars:     scalars(),
	}

	if err := made.holdOperations(); err != nil {
		return schema.Model{}, err
	}

	var err error

	if model.Objects, err = made.buildObjects(); err != nil {
		return schema.Model{}, err
	}

	if model.Queries, model.Mutations, err = made.buildFields(); err != nil {
		return schema.Model{}, err
	}

	return model, nil
}

// core is as much of the OpenAPI document as this generator reads.
type core struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]jsonSchema `json:"schemas"`
	} `json:"components"`
}

// jsonSchema is one JSON Schema node, read to the depth this generator needs.
type jsonSchema struct {
	Ref                  string                `json:"$ref"`
	Type                 json.RawMessage       `json:"type"`
	Format               string                `json:"format"`
	Description          string                `json:"description"`
	Properties           map[string]jsonSchema `json:"properties"`
	Required             []string              `json:"required"`
	Items                *jsonSchema           `json:"items"`
	AdditionalProperties json.RawMessage       `json:"additionalProperties"`
}

// operation is the part of an operation object this generator reads.
type operation struct {
	ID          string      `json:"operationId"`
	Summary     string      `json:"summary"`
	Description string      `json:"description"`
	Parameters  []parameter `json:"parameters"`
	RequestBody *struct {
		Content map[string]struct {
			Schema jsonSchema `json:"schema"`
		} `json:"content"`
	} `json:"requestBody"`
	Responses map[string]struct {
		Content map[string]struct {
			Schema jsonSchema `json:"schema"`
		} `json:"content"`
	} `json:"responses"`
}

// parameter is one operation parameter.
type parameter struct {
	Name        string     `json:"name"`
	In          string     `json:"in"`
	Description string     `json:"description"`
	Required    bool       `json:"required"`
	Schema      jsonSchema `json:"schema"`
}

// methods are the path-item members that are operations.
//
//nolint:gochecknoglobals // a fixed set the walk reads.
var methods = []string{"get", "put", "post", "delete", "patch", "head", "options", "trace"}

// maker holds the document and the tables while the model is assembled.
type maker struct {
	core    core
	objects map[string]objectBinding
}

// scalars are the two types this schema adds to the five GraphQL builds in.
func scalars() []schema.Scalar {
	return []schema.Scalar{
		{
			Name: schema.ScalarInt64,
			Description: "A whole number up to 64 bits wide. GraphQL's own Int is 32 bits, " +
				"which a file size, a byte count or an epoch counter outgrows, so every " +
				"core member the document declares as format int64 is this type.",
		},
		{
			Name: schema.ScalarJSONObject,
			Description: "An open JSON object, keyed by strings. The core uses one where the " +
				"keys are not a closed set: a device's state tree, whose paths a driver " +
				"declares, and a label set an operator writes. Selecting members of it is " +
				"not possible, because a schema cannot name keys it does not know.",
		},
	}
}

// holdOperations refuses a document carrying an operation the tables do not
// mention, and a table naming an operation the document no longer carries.
func (m *maker) holdOperations() error {
	known, err := mentioned()
	if err != nil {
		return err
	}

	found, err := m.operationIDs()
	if err != nil {
		return err
	}

	for _, id := range slices.Sorted(maps.Keys(found)) {
		if _, decided := known[id]; !decided {
			return fmt.Errorf("the core serves %q and this module's tables do not mention it. "+
				"Serve it in fields, or list it in unboundGroups with the reason", id)
		}
	}

	for _, id := range slices.Sorted(maps.Keys(known)) {
		if !found[id] {
			return fmt.Errorf("the tables mention %q and the core document no longer carries it", id)
		}
	}

	return nil
}

// mentioned is every operation the tables decided about, each marked with
// which table decided it.
func mentioned() (map[string]string, error) {
	known := map[string]string{}

	for _, binding := range fields {
		known[binding.Operation] = "served"
	}

	for _, group := range unboundGroups {
		for _, id := range group.operations {
			if where, taken := known[id]; taken {
				return nil, fmt.Errorf("the operation %q is listed twice, once as %s", id, where)
			}

			known[id] = "excused"
		}
	}

	return known, nil
}

// operationIDs is every operationId the core document carries.
func (m *maker) operationIDs() (map[string]bool, error) {
	found := map[string]bool{}

	for path, item := range m.core.Paths {
		for method, raw := range item {
			if !slices.Contains(methods, method) {
				continue
			}

			var read operation
			if err := json.Unmarshal(raw, &read); err != nil {
				return nil, fmt.Errorf("read %s %s: %w", strings.ToUpper(method), path, err)
			}

			found[read.ID] = true
		}
	}

	return found, nil
}

// buildObjects renders every bound schema as an object type.
func (m *maker) buildObjects() ([]schema.Object, error) {
	out := make([]schema.Object, 0, len(objects))

	for _, binding := range objects {
		declared, known := m.core.Components.Schemas[binding.Schema]
		if !known {
			return nil, fmt.Errorf("the core document carries no schema named %q", binding.Schema)
		}

		if err := holdProperties(binding.Schema, declared, binding.Served, binding.Absent); err != nil {
			return nil, err
		}

		object := schema.Object{
			Name:        binding.Schema,
			Description: declared.Description,
			Schema:      binding.Schema,
		}

		for _, name := range binding.Served {
			property := declared.Properties[name]

			carried, err := m.typeOf(property, slices.Contains(declared.Required, name))
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", binding.Schema, name, err)
			}

			object.Members = append(object.Members, schema.Member{
				Name:        name,
				Description: property.Description,
				Type:        carried,
			})
		}

		out = append(out, object)
	}

	return out, nil
}

// holdProperties is the per-schema half of the gate: a property the core
// schema declares is either served or excused, and a name the binding uses is
// one the core still carries.
func holdProperties(name string, declared jsonSchema, served []string, absent map[string]string) error {
	covered := map[string]bool{}

	for _, property := range served {
		if _, carried := declared.Properties[property]; !carried {
			return fmt.Errorf("%s.%s is served and the core schema no longer carries it", name, property)
		}

		covered[property] = true
	}

	for property, reason := range absent {
		if _, carried := declared.Properties[property]; !carried {
			return fmt.Errorf("%s.%s is excused and the core schema no longer carries it", name, property)
		}

		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("%s.%s is excused with no reason", name, property)
		}

		covered[property] = true
	}

	missing := make([]string, 0, len(declared.Properties))

	for property := range declared.Properties {
		if !covered[property] {
			missing = append(missing, property)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	sort.Strings(missing)

	return fmt.Errorf("the core schema %s carries %s, which this module neither serves nor excuses. "+
		"Add each to Served or to Absent with the reason", name, strings.Join(missing, ", "))
}

// buildFields renders every bound operation as a root field.
func (m *maker) buildFields() (queries, mutations []schema.Field, err error) {
	for _, binding := range fields {
		read, found := m.operation(binding.Operation)
		if !found {
			return nil, nil, fmt.Errorf("the core document carries no operation %q", binding.Operation)
		}

		field, built := m.buildField(binding, read)
		if built != nil {
			return nil, nil, built
		}

		if binding.Root == rootMutation {
			mutations = append(mutations, field)

			continue
		}

		queries = append(queries, field)
	}

	return queries, mutations, nil
}

// buildField renders one root field out of one operation.
func (m *maker) buildField(binding fieldBinding, read operation) (schema.Field, error) {
	answer, err := m.answerOf(binding, read)
	if err != nil {
		return schema.Field{}, err
	}

	field := schema.Field{
		Name:        binding.Name,
		Description: strings.TrimSpace(read.Summary + "\n\n" + cmp.Or(binding.Description, read.Description)),
		Type:        answer,
		Operation:   binding.Operation,
		Method:      binding.Method,
	}

	if field.Args, err = m.argumentsOf(binding, read); err != nil {
		return schema.Field{}, err
	}

	return field, nil
}

// answerOf is the GraphQL type of what one operation answers with, read off
// its own 200 response.
func (m *maker) answerOf(binding fieldBinding, read operation) (string, error) {
	body, found := read.Responses["200"]
	if !found {
		return "", fmt.Errorf("%s answers no 200", binding.Operation)
	}

	content, found := body.Content["application/json"]
	if !found {
		return "", fmt.Errorf("%s answers no JSON", binding.Operation)
	}

	return m.typeOf(content.Schema, true)
}

// argumentsOf is one field's arguments: the operation's own parameters, and
// the members of its request body where it has one.
func (m *maker) argumentsOf(binding fieldBinding, read operation) ([]schema.Arg, error) {
	declared := map[string]parameter{}
	for _, param := range read.Parameters {
		declared[param.Name] = param
	}

	if err := holdParameters(binding, declared); err != nil {
		return nil, err
	}

	args := make([]schema.Arg, 0, len(binding.Params)+len(binding.BodyServed))

	for _, name := range binding.Params {
		param := declared[name]

		carried, err := m.typeOf(param.Schema, param.Required)
		if err != nil {
			return nil, fmt.Errorf("%s(%s): %w", binding.Operation, name, err)
		}

		args = append(args, schema.Arg{Name: name, Description: param.Description, Type: carried})
	}

	if binding.Body == "" {
		return args, nil
	}

	body, known := m.core.Components.Schemas[binding.Body]
	if !known {
		return nil, fmt.Errorf("the core document carries no schema named %q", binding.Body)
	}

	if err := holdProperties(binding.Body, body, binding.BodyServed, binding.BodyAbsent); err != nil {
		return nil, err
	}

	for _, name := range binding.BodyServed {
		property := body.Properties[name]

		carried, err := m.typeOf(property, slices.Contains(body.Required, name))
		if err != nil {
			return nil, fmt.Errorf("%s(%s): %w", binding.Operation, name, err)
		}

		args = append(args, schema.Arg{Name: name, Description: property.Description, Type: carried})
	}

	return args, nil
}

// holdParameters is the per-operation half of the gate: a parameter the
// operation declares is either an argument or excused.
func holdParameters(binding fieldBinding, declared map[string]parameter) error {
	covered := map[string]bool{}

	for _, name := range binding.Params {
		if _, carried := declared[name]; !carried {
			return fmt.Errorf("%s takes the argument %q and the core operation no longer declares it",
				binding.Operation, name)
		}

		covered[name] = true
	}

	for name, reason := range binding.ParamsAbsent {
		if _, carried := declared[name]; !carried {
			return fmt.Errorf("%s excuses the parameter %q and the core operation no longer declares it",
				binding.Operation, name)
		}

		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("%s excuses the parameter %q with no reason", binding.Operation, name)
		}

		covered[name] = true
	}

	missing := make([]string, 0, len(declared))

	for name := range declared {
		if !covered[name] {
			missing = append(missing, name)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	sort.Strings(missing)

	return fmt.Errorf("the core operation %s declares %s, which this field neither takes nor excuses",
		binding.Operation, strings.Join(missing, ", "))
}

// operation finds one operation in the document by its id.
func (m *maker) operation(id string) (operation, bool) {
	for _, item := range m.core.Paths {
		for method, raw := range item {
			if !slices.Contains(methods, method) {
				continue
			}

			var read operation
			if err := json.Unmarshal(raw, &read); err != nil {
				continue
			}

			if read.ID == id {
				return read, true
			}
		}
	}

	return operation{}, false
}

// typeOf is the GraphQL type expression for one JSON Schema node.
func (m *maker) typeOf(node jsonSchema, required bool) (string, error) {
	if name, referenced := refName(node.Ref); referenced {
		if _, served := m.objects[name]; !served {
			return "", fmt.Errorf("it refers to the schema %q, which this module does not serve "+
				"as a type. Add it to the objects table", name)
		}

		return bang(name, required), nil
	}

	kinds, err := typeNames(node.Type)
	if err != nil {
		return "", err
	}

	// A type list carrying null is a member the core may answer with null,
	// whatever the required list says: required means the member is present,
	// not that it holds a value.
	if slices.Contains(kinds, "null") {
		required = false
	}

	kinds = slices.DeleteFunc(kinds, func(kind string) bool { return kind == "null" })

	if len(kinds) != 1 {
		return "", fmt.Errorf("its type reads as %v, and one GraphQL type has to follow from it", kinds)
	}

	switch kinds[0] {
	case "string":
		return bang("String", required), nil
	case "boolean":
		return bang("Boolean", required), nil
	case "number":
		return bang("Float", required), nil
	case "integer":
		if node.Format == "int64" {
			return bang(schema.ScalarInt64, required), nil
		}

		return bang("Int", required), nil
	case "object":
		return bang(schema.ScalarJSONObject, required), nil
	case "array":
		if node.Items == nil {
			return "", errors.New("it is an array and the document names no item schema")
		}

		item, nested := m.typeOf(*node.Items, true)
		if nested != nil {
			return "", nested
		}

		return bang("["+item+"]", required), nil
	}

	return "", fmt.Errorf("the JSON Schema type %q has no GraphQL type here", kinds[0])
}

// typeNames reads the type member, which JSON Schema writes as one name or as
// a list of them.
func typeNames(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, errors.New("it declares no type and no $ref")
	}

	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return []string{one}, nil
	}

	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, fmt.Errorf("its type is %s, which is neither a name nor a list of them", raw)
	}

	return many, nil
}

// refName is the schema a local reference names.
func refName(ref string) (string, bool) {
	return strings.TrimPrefix(ref, "#/components/schemas/"), strings.HasPrefix(ref, "#/components/schemas/")
}

// bang adds the non-null marker where the document says the value is always
// there.
func bang(expression string, required bool) string {
	if required {
		return expression + "!"
	}

	return expression
}
