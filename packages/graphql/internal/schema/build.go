package schema

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

// The two scalars this schema defines beyond what GraphQL builds in. The
// generator writes their names into the model, and the names are here as well
// because the builder has to know which Go value each one carries.
const (
	// ScalarInt64 is the name of the wide integer scalar.
	ScalarInt64 = "Int64"
	// ScalarJSONObject is the name of the open object scalar.
	ScalarJSONObject = "JSONObject"
)

// Resolvers are the root field resolvers, keyed by root and field, such as
// "Query.devices". Build refuses a model with a field nothing resolves, so a
// schema can never publish a field that answers null forever.
type Resolvers map[string]graphql.FieldResolveFn

// Build turns the generated model into a runnable schema.
//
// Object fields carry no resolver of their own. Every root resolver answers
// with plain JSON values, so the library's own field lookup reads a member by
// the name the core spelled it with, and there is no second place where a
// member name could be written differently from the document.
func Build(model Model, resolvers Resolvers) (graphql.Schema, error) {
	built := &builder{named: map[string]graphql.Type{}}

	for _, name := range []string{"String", "Int", "Float", "Boolean", "ID"} {
		built.named[name] = standardScalar(name)
	}

	for _, scalar := range model.Scalars {
		defined, err := customScalar(scalar)
		if err != nil {
			return graphql.Schema{}, err
		}

		built.named[scalar.Name] = defined
	}

	// Every object is registered before any field list is read, because the
	// library reads a field list lazily and a type may name one declared
	// after it.
	for _, object := range model.Objects {
		built.named[object.Name] = built.object(object)
	}

	config := graphql.SchemaConfig{}

	query, err := built.root("Query", model.Queries, resolvers)
	if err != nil {
		return graphql.Schema{}, err
	}

	config.Query = query

	if len(model.Mutations) > 0 {
		mutation, raised := built.root("Mutation", model.Mutations, resolvers)
		if raised != nil {
			return graphql.Schema{}, raised
		}

		config.Mutation = mutation
	}

	schema, err := graphql.NewSchema(config)
	if err != nil {
		return graphql.Schema{}, fmt.Errorf("build the schema: %w", err)
	}

	if built.failed != nil {
		return graphql.Schema{}, built.failed
	}

	return schema, nil
}

// builder holds every named type while the schema is assembled.
type builder struct {
	named map[string]graphql.Type
	// failed is the first refusal a lazily read field list raised. The library
	// reads those inside its own walk and gives a thunk no way to report, so
	// the refusal is kept here and checked once the schema is built.
	failed error
}

// object registers one object type. Its fields are read lazily so that two
// types may name each other.
func (b *builder) object(declared Object) *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name:        declared.Name,
		Description: declared.Description,
		Fields: graphql.FieldsThunk(func() graphql.Fields {
			fields := graphql.Fields{}

			for _, member := range declared.Members {
				carried, err := b.typeOf(member.Type)
				if err != nil {
					b.note(fmt.Errorf("%s.%s: %w", declared.Name, member.Name, err))

					continue
				}

				output, ok := carried.(graphql.Output)
				if !ok {
					b.note(fmt.Errorf("%s.%s: %s is not a type a field may answer with",
						declared.Name, member.Name, member.Type))

					continue
				}

				fields[member.Name] = &graphql.Field{
					Name:        member.Name,
					Description: member.Description,
					Type:        output,
				}
			}

			return fields
		}),
	})
}

// root builds Query or Mutation. A field with no resolver is refused here
// rather than answering null at run time.
func (b *builder) root(name string, declared []Field, resolvers Resolvers) (*graphql.Object, error) {
	fields := graphql.Fields{}

	for _, field := range declared {
		resolve, ok := resolvers[name+"."+field.Name]
		if !ok {
			return nil, fmt.Errorf("%s.%s has no resolver", name, field.Name)
		}

		carried, err := b.typeOf(field.Type)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", name, field.Name, err)
		}

		output, ok := carried.(graphql.Output)
		if !ok {
			return nil, fmt.Errorf("%s.%s: %s is not a type a field may answer with", name, field.Name, field.Type)
		}

		args, err := b.arguments(name, field)
		if err != nil {
			return nil, err
		}

		fields[field.Name] = &graphql.Field{
			Name:        field.Name,
			Description: describeField(field),
			Type:        output,
			Args:        args,
			Resolve:     resolve,
		}
	}

	return graphql.NewObject(graphql.ObjectConfig{Name: name, Fields: fields}), nil
}

// arguments builds one field's arguments.
func (b *builder) arguments(root string, field Field) (graphql.FieldConfigArgument, error) {
	args := graphql.FieldConfigArgument{}
	if len(field.Args) == 0 {
		return args, nil
	}

	for _, arg := range field.Args {
		carried, err := b.typeOf(arg.Type)
		if err != nil {
			return nil, fmt.Errorf("%s.%s(%s): %w", root, field.Name, arg.Name, err)
		}

		input, ok := carried.(graphql.Input)
		if !ok {
			return nil, fmt.Errorf("%s.%s(%s): %s is not a type an argument may take",
				root, field.Name, arg.Name, arg.Type)
		}

		args[arg.Name] = &graphql.ArgumentConfig{Type: input, Description: arg.Description}
	}

	return args, nil
}

// typeOf reads one GraphQL type expression: a name, a list in brackets, and a
// trailing bang for either.
func (b *builder) typeOf(expression string) (graphql.Type, error) {
	expression = strings.TrimSpace(expression)

	if inner, found := strings.CutSuffix(expression, "!"); found {
		carried, err := b.typeOf(inner)
		if err != nil {
			return nil, err
		}

		return graphql.NewNonNull(carried), nil
	}

	if strings.HasPrefix(expression, "[") && strings.HasSuffix(expression, "]") {
		carried, err := b.typeOf(expression[1 : len(expression)-1])
		if err != nil {
			return nil, err
		}

		return graphql.NewList(carried), nil
	}

	named, known := b.named[expression]
	if !known {
		return nil, fmt.Errorf("the schema names no type %q", expression)
	}

	return named, nil
}

// note keeps the first refusal a lazily read field list raised.
func (b *builder) note(err error) {
	if b.failed == nil {
		b.failed = err
	}
}

// standardScalar is one of the five GraphQL defines itself.
func standardScalar(name string) graphql.Type {
	switch name {
	case "Int":
		return graphql.Int
	case "Float":
		return graphql.Float
	case "Boolean":
		return graphql.Boolean
	case "ID":
		return graphql.ID
	default:
		return graphql.String
	}
}

// customScalar builds one of the two scalars this schema adds.
func customScalar(declared Scalar) (*graphql.Scalar, error) {
	config := graphql.ScalarConfig{Name: declared.Name, Description: declared.Description}

	switch declared.Name {
	case ScalarInt64:
		config.Serialize = passThrough
		config.ParseValue = coerceInt64
		config.ParseLiteral = literalInt64
	case ScalarJSONObject:
		config.Serialize = passThrough
		config.ParseValue = coerceObject
		config.ParseLiteral = literalObject
	default:
		return nil, fmt.Errorf("the builder knows no scalar named %q", declared.Name)
	}

	return graphql.NewScalar(config), nil
}

// passThrough writes a value out as it arrived. Both scalars carry values the
// wire already produced as JSON, and re-encoding one here would be a second
// place a number could lose its precision.
func passThrough(value any) any { return value }

// coerceInt64 reads a wide integer from a variable.
func coerceInt64(value any) any {
	switch held := value.(type) {
	case int:
		return int64(held)
	case int64:
		return held
	case float64:
		return int64(held)
	case json.Number:
		read, err := held.Int64()
		if err != nil {
			return nil
		}

		return read
	case string:
		read, err := strconv.ParseInt(held, 10, 64)
		if err != nil {
			return nil
		}

		return read
	}

	return nil
}

// literalInt64 reads a wide integer written into the query text.
func literalInt64(value ast.Value) any {
	written, ok := value.(*ast.IntValue)
	if !ok {
		return nil
	}

	read, err := strconv.ParseInt(written.Value, 10, 64)
	if err != nil {
		return nil
	}

	return read
}

// coerceObject reads an open object from a variable.
func coerceObject(value any) any {
	held, ok := value.(map[string]any)
	if !ok {
		return nil
	}

	return held
}

// literalObject reads an open object written into the query text.
func literalObject(value ast.Value) any {
	written, ok := value.(*ast.ObjectValue)
	if !ok {
		return nil
	}

	out := map[string]any{}

	for _, field := range written.Fields {
		out[field.Name.Value] = literalValue(field.Value)
	}

	return out
}

// literalValue reads one value out of a query's own text.
func literalValue(value ast.Value) any {
	switch held := value.(type) {
	case *ast.StringValue:
		return held.Value
	case *ast.BooleanValue:
		return held.Value
	case *ast.IntValue:
		read, err := strconv.ParseInt(held.Value, 10, 64)
		if err != nil {
			return held.Value
		}

		return read
	case *ast.FloatValue:
		read, err := strconv.ParseFloat(held.Value, 64)
		if err != nil {
			return held.Value
		}

		return read
	case *ast.ObjectValue:
		return literalObject(held)
	case *ast.ListValue:
		out := make([]any, 0, len(held.Values))
		for _, item := range held.Values {
			out = append(out, literalValue(item))
		}

		return out
	}

	return nil
}
