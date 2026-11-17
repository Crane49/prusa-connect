package openapidoc_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/codegen/openapidoc"
)

// One document with the shapes the two generators disagree on: a path item
// declaring a parameter, an operation restating one, a body offering both
// octet-stream and sdp, and a success answering JSON beside a refusal.
const fixture = `{
  "tags": [{"name": "things", "description": "The things."}, {"name": "nothing"}],
  "paths": {
    "/things/{id}": {
      "parameters": [{"name": "id", "in": "path", "required": true,
                      "schema": {"type": "string", "examples": ["t-1"]}}],
      "get": {
        "operationId": "get-thing",
        "summary": "One thing.",
        "parameters": [{"name": "verbose", "in": "query", "example": true, "schema": {"type": "boolean"}}],
        "x-sample-query": "verbose=true",
        "responses": {
          "200": {"description": "OK",
                  "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Thing"},
                                                   "example": {"id": "t-1"}}}},
          "404": {"description": "Not Found",
                  "content": {"application/problem+json": {"schema": {"$ref": "#/components/schemas/ProblemError"}}}}
        }
      },
      "put": {
        "operationId": "put-thing",
        "parameters": [{"name": "id", "in": "path", "required": true, "description": "restated"}],
        "requestBody": {"required": true, "content": {"application/octet-stream": {},
                                                      "application/sdp": {"example": "v=0"}}},
        "security": [],
        "responses": {"201": {"description": "Created",
                              "content": {"application/sdp": {"schema": {"type": "string"}, "example": "v=0 a=sendonly"}}}}
      }
    },
    "/nothing": {
      "delete": {"operationId": "delete-nothing", "x-sample-excuse": "204. Nothing comes back.",
                 "responses": {"204": {"description": "No Content"}}}
    }
  },
  "components": {"schemas": {
    "Thing": {"type": "object", "required": ["id"],
              "properties": {"id": {"type": "string"}, "parts": {"type": "array", "items": {"$ref": "#/components/schemas/Part"}}}},
    "Part": {"type": "object", "properties": {"name": {"type": "string"}}}
  }}
}`

// TestComponentResolvesASchemaReference is what the compatibility gate leans
// on: a $ref names a component, and a component declares its properties.
func TestComponentResolvesASchemaReference(t *testing.T) {
	t.Parallel()

	doc, err := openapidoc.Decode([]byte(fixture))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	thing, ok := doc.Component("#/components/schemas/Thing")
	if !ok {
		t.Fatal("Thing is declared and was not resolved")
	}

	if thing.Required[0] != "id" || thing.Properties["parts"].Items.Ref != "#/components/schemas/Part" {
		t.Errorf("Thing resolved to %+v", thing)
	}

	if _, ok = doc.Component("#/components/schemas/Nothing"); ok {
		t.Error("an undeclared component resolved")
	}

	if _, ok = doc.Component("#/components/parameters/id"); ok {
		t.Error("a reference outside the schemas resolved")
	}
}

func TestOperationsMergeThePathItemParameters(t *testing.T) {
	t.Parallel()

	doc, err := openapidoc.Decode([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}

	ops, err := doc.Operations()
	if err != nil {
		t.Fatal(err)
	}

	if len(ops) != 3 || ops[0].ID != "delete-nothing" || ops[1].ID != "get-thing" || ops[2].ID != "put-thing" {
		t.Fatalf("operations are not sorted by id: %+v", ops)
	}

	get, put := ops[1], ops[2]

	if get.Method != http.MethodGet || get.Path != "/things/{id}" || get.Name() != "getThing" {
		t.Errorf("get-thing read as %s %s named %s", get.Method, get.Path, get.Name())
	}

	if params := get.ParametersIn("path"); len(params) != 1 || params[0].Name != "id" || !params[0].Required {
		t.Errorf("the path item's parameter did not reach get-thing: %+v", params)
	}

	if params := put.ParametersIn("path"); len(params) != 1 || params[0].Description != "restated" {
		t.Errorf("the operation's restated parameter did not win: %+v", params)
	}

	if !put.Anonymous() || get.Anonymous() {
		t.Error("an empty security list is anonymous and an absent one is not")
	}
}

func TestSuccessAndSentMedia(t *testing.T) {
	t.Parallel()

	doc, err := openapidoc.Decode([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}

	ops, err := doc.Operations()
	if err != nil {
		t.Fatal(err)
	}

	byID := map[string]openapidoc.Operation{}
	for _, op := range ops {
		byID[op.ID] = op
	}

	kind, statuses := byID["get-thing"].Success()
	if kind != openapidoc.ResponseJSON || len(statuses) != 1 ||
		statuses[0].Status != "200" || statuses[0].Schema != "Thing" {
		t.Errorf("get-thing reads as %s %+v", kind, statuses)
	}

	if kind, _ = byID["put-thing"].Success(); kind != openapidoc.ResponseText {
		t.Errorf("an sdp answer reads as %s, want text", kind)
	}

	if kind, _ = byID["delete-nothing"].Success(); kind != openapidoc.ResponseNone {
		t.Errorf("no content reads as %s, want none", kind)
	}

	if media := byID["put-thing"].RequestBody.SentMedia(); media != "application/sdp" {
		t.Errorf("the body sent is %q, want application/sdp over the octet-stream alias", media)
	}

	if media := byID["get-thing"].RequestBody.SentMedia(); media != "" {
		t.Errorf("an operation without a body sends %q", media)
	}
}

// TestTagsKeepTheDocumentsOrder holds the one fact a reference takes from the
// tags block: the order the document lists them in, which is the order the
// resource pages are listed in.
func TestTagsKeepTheDocumentsOrder(t *testing.T) {
	t.Parallel()

	doc, err := openapidoc.Decode([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}

	if len(doc.Tags) != 2 || doc.Tags[0].Name != "things" || doc.Tags[1].Name != "nothing" {
		t.Fatalf("tags = %+v, want things then nothing", doc.Tags)
	}

	if doc.Tags[0].Description != "The things." {
		t.Fatalf("description = %q", doc.Tags[0].Description)
	}
}

func TestADocumentWithoutOperationsIsRefused(t *testing.T) {
	t.Parallel()

	doc, err := openapidoc.Decode([]byte(`{"paths": {"/x": {"summary": "not an operation"}}}`))
	if err != nil {
		t.Fatal(err)
	}

	if _, err = doc.Operations(); !errors.Is(err, openapidoc.ErrNoOperations) {
		t.Fatalf("got %v, want ErrNoOperations", err)
	}

	doc, err = openapidoc.Decode([]byte(
		`{"paths": {"/x": {"get": {"operationId": "a"}, "post": {"operationId": "a"}}}}`))
	if err != nil {
		t.Fatal(err)
	}

	if _, err = doc.Operations(); !errors.Is(err, openapidoc.ErrDuplicateID) {
		t.Fatalf("got %v, want ErrDuplicateID", err)
	}
}

func TestSchemaName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  string
		want string
	}{
		{`{"$ref": "#/components/schemas/Device"}`, "Device"},
		{`{"type": "string"}`, "string"},
		{`{"type": ["string", "null"]}`, "string or null"},
		{`{"type": "array", "items": {"type": "integer"}}`, "array of integer"},
		{`{"type": "string", "format": "binary"}`, "binary"},
		{`{"type": "string", "enum": ["a", "b"]}`, "one of a, b"},
		{`{}`, "object"},
	}

	for _, tt := range tests {
		doc, err := openapidoc.Decode([]byte(`{"paths": {"/x": {"get": {"operationId": "x",
			"parameters": [{"name": "p", "in": "query", "schema": ` + tt.raw + `}]}}}}`))
		if err != nil {
			t.Fatal(err)
		}

		ops, err := doc.Operations()
		if err != nil {
			t.Fatal(err)
		}

		if got := ops[0].Parameters[0].Schema.Name(); got != tt.want {
			t.Errorf("%s names as %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestCamelCase(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"list-device-files": "listDeviceFiles",
		"logout":            "logout",
		"a--b":              "aB",
	}

	for id, want := range tests {
		if got := openapidoc.CamelCase(id); got != want {
			t.Errorf("CamelCase(%q) = %q, want %q", id, got, want)
		}
	}
}

// TestSampleReadsTheExamplesOffTheOperation is the reading a rendered sample
// is built from: the request example under the media type the runtime sends,
// the response example under the success status that carries one, the query
// the sample sends, the excuse an operation without a sample gives, and the
// value each parameter is filled with.
func TestSampleReadsTheExamplesOffTheOperation(t *testing.T) {
	t.Parallel()

	doc, err := openapidoc.Decode([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}

	ops, err := doc.Operations()
	if err != nil {
		t.Fatal(err)
	}

	byID := map[string]openapidoc.Operation{}
	for _, op := range ops {
		byID[op.ID] = op
	}

	get, carried := byID["get-thing"].Sample()
	if !carried || get.Status != "200" || get.ResponseMedia != openapidoc.MediaJSON ||
		string(get.Response) != `{"id": "t-1"}` || get.RequestMedia != "" || len(get.Request) != 0 {
		t.Errorf("get-thing's sample reads as %+v", get)
	}

	if byID["get-thing"].SampleQuery != "verbose=true" {
		t.Errorf("get-thing's sample query reads as %q", byID["get-thing"].SampleQuery)
	}

	put, carried := byID["put-thing"].Sample()
	if !carried || put.Status != "201" || put.ResponseMedia != "application/sdp" ||
		put.RequestMedia != "application/sdp" || string(put.Request) != `"v=0"` {
		t.Errorf("put-thing's sample reads as %+v", put)
	}

	if _, carried = byID["delete-nothing"].Sample(); carried {
		t.Error("an operation with no example carries a sample")
	}

	if byID["delete-nothing"].SampleExcuse != "204. Nothing comes back." {
		t.Errorf("the excuse reads as %q", byID["delete-nothing"].SampleExcuse)
	}

	params := byID["get-thing"].Parameters

	if value, ok := params[0].ExampleValue(); !ok || string(value) != `"t-1"` {
		t.Errorf("the schema's example did not reach the path parameter: %s", value)
	}

	if value, ok := params[1].ExampleValue(); !ok || string(value) != `true` {
		t.Errorf("the parameter's own example was not read: %s", value)
	}

	if _, ok := byID["put-thing"].Parameters[0].ExampleValue(); ok {
		t.Error("a restated parameter with no example reports one")
	}
}
