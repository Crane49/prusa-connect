// Package schema is the generated GraphQL schema as data, the SDL it renders
// as, and the runtime types it builds into.
//
// The generator writes a Model. The module embeds it and builds a runnable
// schema out of it at start-up, and the same Model renders the SDL that ships
// beside the binary. One description of the surface produces both, so the
// document a client reads and the schema the server executes cannot say
// different things.
package schema

// Model is the whole surface: the scalars it defines, the object types it
// serves, and the two root fields lists.
//
// Every name here is the core's own spelling. A member keeps the snake_case
// the OpenAPI document gave it, so a person reading the REST reference and a
// person reading this schema read the same words.
type Model struct {
	// Source is the core document this was generated from, as a path relative
	// to the repository root. It is written into the SDL header so a reader
	// knows what to regenerate against.
	Source string `json:"source"`
	// CoreVersion is the `info.version` of that document.
	CoreVersion string `json:"core_version"`
	// Scalars are the types this schema defines beyond the five GraphQL
	// builds in.
	Scalars []Scalar `json:"scalars"`
	// Objects are the core's schemas as object types, in a fixed order.
	Objects []Object `json:"objects"`
	// Queries are the read fields, one per core read operation the module can
	// answer.
	Queries []Field `json:"queries"`
	// Mutations are the state-changing fields, one per core write operation
	// the module can answer.
	Mutations []Field `json:"mutations"`
}

// Scalar is one custom scalar. The module defines two: an integer wider than
// GraphQL's own Int, and an open object.
type Scalar struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Object is one of the core's schemas as a GraphQL object type.
type Object struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Schema is the component schema in the core document this came from, so
	// a reader can go and look at the source of every member below.
	Schema string `json:"schema"`
	// Members are the properties served, in the order the binding lists them.
	Members []Member `json:"members"`
}

// Member is one field on an object type.
type Member struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Type is the GraphQL type expression, such as String!, [Device!] or
	// JSONObject.
	Type string `json:"type"`
}

// Field is one root field, on Query or on Mutation.
type Field struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Args        []Arg  `json:"args,omitempty"`
	// Operation is the core operationId this field answers, and Method the
	// wire method its resolver calls. Both are written into the SDL as a
	// comment, because the first question anybody asks of a generated schema
	// is where a field came from.
	Operation string `json:"operation"`
	Method    string `json:"method"`
}

// Arg is one argument on a root field.
type Arg struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
}
