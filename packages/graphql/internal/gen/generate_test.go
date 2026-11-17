package gen_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/graphql/internal/gen"
	"github.com/prusa3d/graphql/internal/schema"
)

// The generator is tested against the core document that is in the tree, and
// against copies of it with one thing changed. A gate that has never been
// watched failing is a gate nobody should trust, so every refusal below is
// produced by moving the document under it.

// document is the core's committed document, read from the checkout.
func document(t *testing.T) []byte {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", gen.CoreDocument))
	if err != nil {
		t.Fatalf("read the core document: %v", err)
	}

	return raw
}

// mutate reads the document, hands it to change, and renders it again.
func mutate(t *testing.T, change func(read map[string]any)) []byte {
	t.Helper()

	var read map[string]any
	if err := json.Unmarshal(document(t), &read); err != nil {
		t.Fatalf("read the document: %v", err)
	}

	change(read)

	written, err := json.Marshal(read)
	if err != nil {
		t.Fatalf("render the document: %v", err)
	}

	return written
}

// schemas is the components.schemas object of a document being mutated.
func schemas(read map[string]any) map[string]any {
	components, _ := read["components"].(map[string]any)
	out, _ := components["schemas"].(map[string]any)

	return out
}

func TestTheSchemaIsGeneratedFromTheDocumentInTheTree(t *testing.T) {
	t.Parallel()

	model, err := gen.Generate(document(t), gen.CoreDocument)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if model.Source != gen.CoreDocument {
		t.Errorf("the model names %q as its source", model.Source)
	}

	if len(model.Queries) == 0 || len(model.Mutations) == 0 {
		t.Fatalf("the model carries %d queries and %d mutations",
			len(model.Queries), len(model.Mutations))
	}

	devices, found := field(model.Queries, "devices")
	if !found {
		t.Fatal("the model carries no devices field")
	}

	if devices.Type != "DevicePage!" || devices.Operation != "list-devices" {
		t.Errorf("the devices field reads as %+v", devices)
	}

	// The page is the core's own shape, and the member names are the ones the
	// document spells, which is the snake_case ADR-20260826-c77 fixed.
	page, found := object(model.Objects, "DevicePage")
	if !found {
		t.Fatal("the model carries no DevicePage type")
	}

	for _, member := range []string{"items", "total", "limit", "offset"} {
		if !slices.ContainsFunc(page.Members, func(held schema.Member) bool {
			return held.Name == member
		}) {
			t.Errorf("DevicePage carries no %s: %+v", member, page.Members)
		}
	}
}

// TestAMemberKeepsTheDocumentsOwnSentence holds that nothing here rewrites the
// core's words. A reader of the REST reference and a reader of the schema meet
// the same sentence.
func TestAMemberKeepsTheDocumentsOwnSentence(t *testing.T) {
	t.Parallel()

	model, err := gen.Generate(document(t), gen.CoreDocument)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	device, found := object(model.Objects, "Device")
	if !found {
		t.Fatal("the model carries no Device type")
	}

	for _, member := range device.Members {
		if strings.TrimSpace(member.Description) == "" && member.Name != "items" {
			t.Errorf("Device.%s carries no description, so the document has none either", member.Name)
		}
	}
}

// TestAnOperationTheCoreGrowsFailsTheGenerator is the operation half of
// ADR-20260910-cqq rule 3's gate, watched failing.
func TestAnOperationTheCoreGrowsFailsTheGenerator(t *testing.T) {
	t.Parallel()

	grown := mutate(t, func(read map[string]any) {
		paths, _ := read["paths"].(map[string]any)
		paths["/api/v1/widgets"] = map[string]any{
			"get": map[string]any{
				"operationId": "list-widgets",
				"summary":     "The widgets",
				"description": "Every widget.",
				"responses": map[string]any{
					"200": map[string]any{"content": map[string]any{
						"application/json": map[string]any{"schema": map[string]any{"type": "object"}},
					}},
				},
			},
		}
	})

	_, err := gen.Generate(grown, gen.CoreDocument)
	if err == nil {
		t.Fatal("the generator wrote a schema for a document carrying an operation it knows nothing of")
	}

	if !strings.Contains(err.Error(), "list-widgets") {
		t.Errorf("the refusal reads %q, and it does not name the operation", err)
	}
}

// TestAPropertyTheCoreGrowsFailsTheGenerator is the property half of the same
// gate. A member added to a schema this module serves has to be either served
// or excused, because a member nobody decided about is a surface that has
// fallen behind the document.
func TestAPropertyTheCoreGrowsFailsTheGenerator(t *testing.T) {
	t.Parallel()

	grown := mutate(t, func(read map[string]any) {
		device, _ := schemas(read)["Device"].(map[string]any)
		properties, _ := device["properties"].(map[string]any)
		properties["colour"] = map[string]any{
			"type": "string", "description": "What colour the machine is.",
		}
	})

	_, err := gen.Generate(grown, gen.CoreDocument)
	if err == nil {
		t.Fatal("the generator wrote a schema for a Device carrying a property it knows nothing of")
	}

	if !strings.Contains(err.Error(), "colour") {
		t.Errorf("the refusal reads %q, and it does not name the property", err)
	}
}

// TestAPropertyTheCoreDropsFailsTheGenerator is the other direction: a member
// this module serves that the core no longer has.
func TestAPropertyTheCoreDropsFailsTheGenerator(t *testing.T) {
	t.Parallel()

	dropped := mutate(t, func(read map[string]any) {
		device, _ := schemas(read)["Device"].(map[string]any)
		properties, _ := device["properties"].(map[string]any)
		delete(properties, "serial")
	})

	_, err := gen.Generate(dropped, gen.CoreDocument)
	if err == nil {
		t.Fatal("the generator served a member the core document no longer carries")
	}

	if !strings.Contains(err.Error(), "serial") {
		t.Errorf("the refusal reads %q, and it does not name the member", err)
	}
}

// TestAParameterTheCoreGrowsFailsTheGenerator holds the same rule over an
// operation's own parameters.
func TestAParameterTheCoreGrowsFailsTheGenerator(t *testing.T) {
	t.Parallel()

	grown := mutate(t, func(read map[string]any) {
		paths, _ := read["paths"].(map[string]any)
		item, _ := paths["/api/v1/devices"].(map[string]any)
		list, _ := item["get"].(map[string]any)
		parameters, _ := list["parameters"].([]any)
		list["parameters"] = append(parameters, map[string]any{
			"name": "colour", "in": "query", "description": "Keep the machines of one colour.",
			"schema": map[string]any{"type": "string"},
		})
	})

	_, err := gen.Generate(grown, gen.CoreDocument)
	if err == nil {
		t.Fatal("the generator wrote a field for an operation carrying a parameter it knows nothing of")
	}

	if !strings.Contains(err.Error(), "colour") {
		t.Errorf("the refusal reads %q, and it does not name the parameter", err)
	}
}

// TestTheRenderedSchemaIsReadableGraphQL holds the SDL to the shapes a reader
// and a parser both need: the header that says where it came from, a type per
// object, and the two roots.
func TestTheRenderedSchemaIsReadableGraphQL(t *testing.T) {
	t.Parallel()

	model, err := gen.Generate(document(t), gen.CoreDocument)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	rendered := schema.Render(model)

	for _, wanted := range []string{
		"# Generated from " + gen.CoreDocument,
		"scalar Int64",
		"scalar JSONObject",
		"type Device {",
		"type Query {",
		"type Mutation {",
		"devices(",
		"): DevicePage!",
	} {
		if !strings.Contains(rendered, wanted) {
			t.Errorf("the schema does not carry %q", wanted)
		}
	}

	// No subscription root, and nothing builds one. ADR-20260910-cqq rule 11.
	if strings.Contains(rendered, "type Subscription") {
		t.Error("the schema declares a subscription root, and the wire carries no stream")
	}
}

// TestBothFilesComeFromOneModel is what makes the SDL and the embedded model
// impossible to disagree: one call writes both.
func TestBothFilesComeFromOneModel(t *testing.T) {
	t.Parallel()

	model, err := gen.Generate(document(t), gen.CoreDocument)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	files, err := schema.Files(model)
	if err != nil {
		t.Fatalf("render the files: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("the generator writes %d files", len(files))
	}

	var read schema.Model
	if err = json.Unmarshal(files[schema.ModelFile], &read); err != nil {
		t.Fatalf("read the model back: %v", err)
	}

	if len(read.Objects) != len(model.Objects) || len(read.Queries) != len(model.Queries) {
		t.Errorf("the model read back as %d objects and %d queries",
			len(read.Objects), len(read.Queries))
	}
}

// field finds one root field by name.
func field(held []schema.Field, name string) (schema.Field, bool) {
	for _, one := range held {
		if one.Name == name {
			return one, true
		}
	}

	return schema.Field{}, false
}

// object finds one type by name.
func object(held []schema.Object, name string) (schema.Object, bool) {
	for _, one := range held {
		if one.Name == name {
			return one, true
		}
	}

	return schema.Object{}, false
}
