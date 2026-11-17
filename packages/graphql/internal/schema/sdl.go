package schema

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The two files the generator writes and the gate compares.
const (
	// SDLFile is the schema definition language a client reads.
	SDLFile = "schema.graphql"
	// ModelFile is the same schema as data, which the module embeds.
	ModelFile = "model.json"
)

// Files is what the generator writes, keyed by name.
//
// The gate calls this rather than the command, so what it compares is the
// bytes the command would have written.
func Files(model Model) (map[string][]byte, error) {
	data, err := json.MarshalIndent(model, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render the model: %w", err)
	}

	return map[string][]byte{
		SDLFile:   []byte(Render(model)),
		ModelFile: append(data, '\n'),
	}, nil
}

// Render writes the model as GraphQL schema definition language.
//
// This is the file a client reads, shipped beside the module's OpenAPI
// fragment. It is written by the generator and never by hand, and a test
// compares the committed file with what the generator produces from the core
// document that is in the tree now.
func Render(model Model) string {
	var out strings.Builder

	out.WriteString("# The GraphQL surface of Prusa Connect, in the core's own vocabulary.\n")
	out.WriteString("#\n")
	fmt.Fprintf(&out, "# Generated from %s, which the core publishes as version %s.\n",
		model.Source, model.CoreVersion)
	out.WriteString("# Run `go generate ./...` in packages/graphql to write it again.\n")
	out.WriteString("# Nothing here is edited by hand: a gate refuses a copy that has fallen\n")
	out.WriteString("# behind the document.\n")

	for _, scalar := range model.Scalars {
		out.WriteString("\n")
		writeDescription(&out, scalar.Description, "")
		fmt.Fprintf(&out, "scalar %s\n", scalar.Name)
	}

	for _, object := range model.Objects {
		out.WriteString("\n")
		writeDescription(&out, object.Description, "")
		fmt.Fprintf(&out, "type %s {\n", object.Name)

		for index, member := range object.Members {
			if index > 0 {
				out.WriteString("\n")
			}

			writeDescription(&out, member.Description, "  ")
			fmt.Fprintf(&out, "  %s: %s\n", member.Name, member.Type)
		}

		out.WriteString("}\n")
	}

	writeRoot(&out, "Query", model.Queries)
	writeRoot(&out, "Mutation", model.Mutations)

	return out.String()
}

// writeRoot writes one root type. A root with no fields is written as nothing
// at all rather than as an empty type, because an empty type is not a legal
// schema and a client would rather read that the root is absent.
func writeRoot(out *strings.Builder, name string, fields []Field) {
	if len(fields) == 0 {
		return
	}

	fmt.Fprintf(out, "\ntype %s {\n", name)

	for index, field := range fields {
		if index > 0 {
			out.WriteString("\n")
		}

		writeDescription(out, describeField(field), "  ")
		fmt.Fprintf(out, "  %s%s: %s\n", field.Name, renderArgs(field.Args), field.Type)
	}

	out.WriteString("}\n")
}

// describeField puts the core operation and the wire method at the end of the
// field's own sentences. Where a field came from is the first thing a reader
// asks of a generated schema, and the description is the only place a GraphQL
// client shows it.
func describeField(field Field) string {
	return field.Description +
		"\n\nIt answers the core operation " + field.Operation +
		" and resolves through the module wire method " + field.Method + "."
}

// renderArgs writes one field's arguments, on one line when they are short and
// one per line when they are not. A schema is read far more often than it is
// generated, so the shape it takes on the page is worth the branch.
func renderArgs(args []Arg) string {
	if len(args) == 0 {
		return ""
	}

	var out strings.Builder

	out.WriteString("(\n")

	for index, arg := range args {
		if index > 0 {
			out.WriteString("\n")
		}

		writeDescription(&out, arg.Description, "    ")
		fmt.Fprintf(&out, "    %s: %s\n", arg.Name, arg.Type)
	}

	out.WriteString("  )")

	return out.String()
}

// writeDescription writes one block string, indented, and nothing at all for
// an empty description.
func writeDescription(out *strings.Builder, description, indent string) {
	description = strings.TrimSpace(description)
	if description == "" {
		return
	}

	// A block string cannot hold three quotes, and a description that did
	// would end the string early and leave the rest as broken syntax.
	description = strings.ReplaceAll(description, `"""`, `'''`)

	fmt.Fprintf(out, "%s\"\"\"\n", indent)

	for line := range strings.SplitSeq(description, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if line == "" {
			out.WriteString("\n")

			continue
		}

		fmt.Fprintf(out, "%s%s\n", indent, line)
	}

	fmt.Fprintf(out, "%s\"\"\"\n", indent)
}
