package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/codegen/openapidoc"
)

// The sample renderer, held to a document small enough to read: an anonymous
// GET with a path and a query, a POST with a JSON body, a POST with a text
// body, an excused operation, and the three shapes it refuses.
const sampleFixture = `{
  "tags": [{"name": "things", "description": "The things."}],
  "paths": {
    "/things/{id}": {
      "get": {
        "operationId": "get-thing", "tags": ["things"], "security": [],
        "parameters": [
          {"name": "id", "in": "path", "required": true, "schema": {"type": "string", "examples": ["t/1"]}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "examples": [10]}},
          {"name": "verbose", "in": "query", "schema": {"type": "boolean", "examples": [true]}}
        ],
        "x-sample-query": "limit=25",
        "responses": {"200": {"description": "OK", "content": {"application/json": {
          "schema": {"$ref": "#/components/schemas/Thing"},
          "example": {"id": "t/1", "/state": "IDLE", "tags": [], "meta": {}}}}}}
      },
      "post": {
        "operationId": "rename-thing", "tags": ["things"],
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string", "examples": ["t/1"]}}],
        "requestBody": {"required": true, "content": {"application/json": {
          "schema": {"type": "object"}, "example": {"name": "O'Brien's", "labels": ["a", "b"]}}}},
        "responses": {"201": {"description": "Created", "content": {"application/json": {
          "schema": {"type": "object"}, "example": {"name": "O'Brien's"}}}}}
      },
      "delete": {
        "operationId": "delete-thing", "tags": ["things"], "x-sample-excuse": "204. Nothing comes back.",
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string", "examples": ["t/1"]}}],
        "responses": {"204": {"description": "No Content"}}
      }
    },
    "/things/{id}/offer": {
      "post": {
        "operationId": "offer-thing", "tags": ["things"],
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string", "examples": ["t/1"]}}],
        "requestBody": {"required": true, "content": {"application/sdp": {"schema": {"type": "string"}, "example": "v=0\nm=video"}}},
        "responses": {"201": {"description": "Created", "content": {"application/sdp": {"schema": {"type": "string"}, "example": "v=0\na=sendonly\n"}}}}
      }
    },
    "/bare": {
      "get": {"operationId": "no-sample", "tags": ["things"], "responses": {"200": {"description": "OK"}}}
    },
    "/needs-query": {
      "get": {
        "operationId": "needs-query", "tags": ["things"],
        "parameters": [{"name": "path", "in": "query", "required": true, "schema": {"type": "string", "examples": ["a"]}}],
        "responses": {"200": {"description": "OK", "content": {"application/json": {"example": {}}}}}
      }
    },
    "/unfilled/{name}": {
      "get": {
        "operationId": "unfilled", "tags": ["things"],
        "parameters": [{"name": "name", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "OK", "content": {"application/json": {"example": {}}}}}
      }
    }
  }
}`

func fixtureOperations(t *testing.T) map[string]operation {
	t.Helper()

	doc, err := openapidoc.Decode([]byte(sampleFixture))
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := doc.Operations()
	if err != nil {
		t.Fatal(err)
	}

	byID := map[string]operation{}
	for _, op := range operationsOf(decoded) {
		byID[op.ID] = op
	}

	return byID
}

func renderSample(t *testing.T, op operation) string {
	t.Helper()

	var body strings.Builder

	if err := writeSample(&body, op, anchors{}); err != nil {
		t.Fatalf("%s: %v", op.ID, err)
	}

	return body.String()
}

// TestASampleIsThreeFencesUnderOneHeading is ADR-20260910-he3 rule 6 on one GET: the
// curl line with the path filled and the query the sample declares, the
// client call with the same values typed as the parameters are, and the
// answer under its status. An anonymous operation sends no token.
func TestASampleIsThreeFencesUnderOneHeading(t *testing.T) {
	t.Parallel()

	got := renderSample(t, fixtureOperations(t)["get-thing"])

	for _, want := range []string{
		"#### Sample {#sample-get-thing}\n",
		"```sh\ncurl \"http://127.0.0.1:8080/things/t%2F1?limit=25\"\n```",
		"```ts\nawait client.getThing({\n  path: { id: \"t/1\" },\n  query: { limit: 25 },\n});\n```",
		"The server answers `200`:\n\n```json\n{\n  \"id\": \"t/1\",\n  \"/state\": \"IDLE\",\n  \"tags\": [],\n  \"meta\": {}\n}\n```",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the sample lacks\n%s\nin\n%s", want, got)
		}
	}

	if strings.Contains(got, "Authorization") || strings.Contains(got, "verbose") {
		t.Errorf("an anonymous GET carries a token, or a query the sample did not declare:\n%s", got)
	}
}

// TestABodyIsSentAsJSONAndAsAnObjectLiteral holds the two renderings of one
// request body together: indented JSON inside single quotes on the curl
// line, with a quote in a value escaped for the shell, and a TypeScript
// literal with bare member names.
func TestABodyIsSentAsJSONAndAsAnObjectLiteral(t *testing.T) {
	t.Parallel()

	got := renderSample(t, fixtureOperations(t)["rename-thing"])

	for _, want := range []string{
		"curl -X POST \"http://127.0.0.1:8080/things/t%2F1\" \\\n  -H \"Authorization: Bearer $TOKEN\" \\\n" +
			"  -H \"Content-Type: application/json\" \\\n  -d '{\n    \"name\": \"O'\\''Brien'\\''s\",\n" +
			"    \"labels\": [\n      \"a\",\n      \"b\"\n    ]\n  }'",
		"await client.renameThing({\n  path: { id: \"t/1\" },\n  body: {\n    name: \"O'Brien's\",\n" +
			"    labels: [\n      \"a\",\n      \"b\",\n    ],\n  },\n});",
		"The server answers `201`:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the sample lacks\n%s\nin\n%s", want, got)
		}
	}
}

// TestATextBodyIsAFileForCurlAndATemplateLiteralForTheClient covers the
// negotiation the camera operation carries as SDP: the shell reads the offer
// from a file, the client sends it as a string, and the answer is shown as
// text under its media type.
func TestATextBodyIsAFileForCurlAndATemplateLiteralForTheClient(t *testing.T) {
	t.Parallel()

	got := renderSample(t, fixtureOperations(t)["offer-thing"])

	for _, want := range []string{
		"-H \"Content-Type: application/sdp\" \\\n  --data-binary @offer.sdp\n",
		"body: `v=0\nm=video`,\n",
		"The server answers `201` as `application/sdp`:\n\n```text\nv=0\na=sendonly\n```",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the sample lacks\n%s\nin\n%s", want, got)
		}
	}
}

// TestAnExcusedOperationRendersItsExcuseAndNothingElse is the honest half of
// the slot: the server's own sentence, and no fence a reader could mistake
// for a request.
func TestAnExcusedOperationRendersItsExcuseAndNothingElse(t *testing.T) {
	t.Parallel()

	got := renderSample(t, fixtureOperations(t)["delete-thing"])

	if got != "\nThis operation has no sample: 204. Nothing comes back.\n" {
		t.Errorf("the excuse rendered as %q", got)
	}
}

// TestTheRendererRefusesASampleItCannotFill is what keeps a page from carrying
// a hole: no sample and no excuse, a required query parameter the sample does
// not send, and a path parameter with nothing to put in it are each refused
// by name.
func TestTheRendererRefusesASampleItCannotFill(t *testing.T) {
	t.Parallel()

	ops := fixtureOperations(t)

	for id, want := range map[string]error{
		"no-sample":   errNoSample,
		"needs-query": errQueryMissing,
		"unfilled":    errNoParameter,
	} {
		var body strings.Builder

		err := writeSample(&body, ops[id], anchors{})
		if !errors.Is(err, want) {
			t.Errorf("%s: got %v, want %v", id, err, want)
		}

		if !strings.Contains(err.Error(), id) && id != "no-sample" {
			t.Errorf("%s: the refusal does not name the operation: %v", id, err)
		}
	}
}

// TestEveryCommittedOperationRendersASampleOrAnExcuse reads the committed
// document, so an operation that reaches the pages with neither fails here
// before it fails a page.
func TestEveryCommittedOperationRendersASampleOrAnExcuse(t *testing.T) {
	t.Parallel()

	api, err := readAPIInputs(committed(t).documentPath)
	if err != nil {
		t.Fatal(err)
	}

	samples, excuses := 0, 0

	for _, op := range api.operations {
		got := renderSample(t, op)

		switch {
		case strings.Contains(got, "#### Sample {#sample-"):
			samples++

			if strings.Count(got, "```") != 6 {
				t.Errorf("%s renders %d fences, not three", op.ID, strings.Count(got, "```")/2)
			}
		case strings.Contains(got, "This operation has no sample: "):
			excuses++
		default:
			t.Errorf("%s renders neither a sample nor an excuse:\n%s", op.ID, got)
		}
	}

	requireAtLeast(t, "rendered samples", samples, 60)
	requireAtLeast(t, "rendered excuses", excuses, 10)
}
