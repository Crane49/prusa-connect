package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// The contracts the client package is generated from are also the reference a
// caller reads, so an entry without its sentence ships as a name and nothing
// else. This is the gate over that: every operation carries a summary and a
// description, every schema property an operation reaches carries a
// description, every event type carries a summary and a description, and every
// error code carries a summary and an action.
//
// It reads the committed documents rather than the Go source, because the
// documents are what a client is generated from and what the reference page
// renders. The fix is in the Go source, and each refusal names the file and
// line where it goes.

// The Go package that registers every operation and declares every body.
const httpapiDir = "../../internal/transport/http/httpapi"

// The sdk package whose descriptor table renders the event types.
const eventDir = "../../sdk/event"

// The sdk package whose registrations render the error codes, and whose
// Problem type is the body every refusal carries.
const faultDir = "../../sdk/fault"

// embedDepth bounds how far the field search follows embedded structs.
const embedDepth = 4

// contractDocument is the slice of the OpenAPI document this gate reads. The
// schemas are kept raw because a property is walked for $ref as well as read
// for its description.
type contractDocument struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]json.RawMessage `json:"schemas"`
	} `json:"components"`
}

type describedOperation struct {
	ID          string                   `json:"operationId"`
	Summary     string                   `json:"summary"`
	Description string                   `json:"description"`
	Parameters  []describedParameter     `json:"parameters"`
	RequestBody *describedBody           `json:"requestBody"`
	Responses   map[string]describedBody `json:"responses"`
}

type describedParameter struct {
	Name        string          `json:"name"`
	In          string          `json:"in"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

type describedBody struct {
	Content map[string]struct {
		Schema json.RawMessage `json:"schema"`
	} `json:"content"`
}

// describedSchema is the part of a schema object the walk reads.
type describedSchema struct {
	Ref                  string                     `json:"$ref"`
	Description          string                     `json:"description"`
	Properties           map[string]json.RawMessage `json:"properties"`
	Items                json.RawMessage            `json:"items"`
	AdditionalProperties json.RawMessage            `json:"additionalProperties"`
	AllOf                []json.RawMessage          `json:"allOf"`
	AnyOf                []json.RawMessage          `json:"anyOf"`
	OneOf                []json.RawMessage          `json:"oneOf"`
}

// gap is one entry that ships without its sentence.
type gap struct {
	pointer string
	fix     string
}

// undescribedAtTheGate is every entry that had no sentence at the commit that
// introduced this gate, frozen so the gate could land green. Shrinking it is
// roadmap and adding to it needs a reason in the pull request. It is checked
// both ways: an entry the tree has since described fails here until it is
// deleted, so the list never describes less than the truth.
//
// The Account, AuthInfo, ListAccountsOutputBody and ListCredentialsOutputBody
// entries are in accounts.go and credentials.go, which the auth work owned
// when the gate landed. The rest are the items member of a listing body, and
// the members of the authorization vocabulary.
var undescribedAtTheGate = []string{
	"schema Account property created_at",
	"schema Account property display_name",
	"schema Account property id",
	"schema Account property subject",
	"schema Account property username",
	"schema AuthInfo property credentials",
	"schema AuthInfo property needs_setup",
	"schema AuthInfo property provider",
	"schema AuthInfo property redirect",
	"schema AuthorizationKind property verbs",
	"schema AuthorizationOutputBody property actions",
	"schema AuthorizationOutputBody property kinds",
	"schema AuthorizationOutputBody property relations",
	"schema AuthorizationOutputBody property shapes",
	"schema AuthorizationOutputBody property types",
	"schema ConnectorHealthOutputBody property items",
	"schema DeviceIssuesOutputBody property items",
	"schema DevicePage property items",
	"schema DeviceStatesOutputBody property items",
	"schema ErrorCodesOutputBody property items",
	"schema EventTypesOutputBody property items",
	"schema ExtensionsOutputBody property items",
	"schema HistoryCommandPage property items",
	"schema HistoryFactPage property items",
	"schema HistorySamplePage property items",
	"schema ListAcceptancesOutputBody property items",
	"schema ListAccountsOutputBody property items",
	"schema ListCamerasOutputBody property items",
	"schema ListConnectorEndpointsOutputBody property items",
	"schema ListCredentialsOutputBody property items",
	"schema ListGCodePresetsOutputBody property items",
	"schema ListGrantsOutputBody property items",
	"schema ListRegistrationsOutputBody property items",
	"schema ListResourcesOutputBody property items",
	"schema ListStateNamespacesOutputBody property items",
	"schema ListStorageFilesBodyBody property items",
	"schema ListStoragesBodyBody property items",
	"schema ListTransfersOutputBody property items",
	"schema ListVerbGrantsOutputBody property items",
	"schema ResourceKindsOutputBody property items",
	"schema VocabularyIndexOutputBody property items",
}

// TestEveryContractEntryIsDescribed is the gate.
func TestEveryContractEntryIsDescribed(t *testing.T) {
	t.Parallel()

	gaps := documentGaps(t)
	gaps = append(gaps, eventTypeGaps(t)...)
	gaps = append(gaps, errorCodeGaps(t)...)

	sort.Slice(gaps, func(i, j int) bool { return gaps[i].pointer < gaps[j].pointer })

	found := map[string]struct{}{}
	open := 0

	for _, g := range gaps {
		found[g.pointer] = struct{}{}

		if slices.Contains(undescribedAtTheGate, g.pointer) {
			continue
		}

		open++

		t.Errorf("%s has no description.\n  %s", g.pointer, g.fix)
	}

	if open > 0 {
		t.Logf("%d entries ship without a sentence. Write each one in the Go source, then run "+
			"make generate-openapi, make generate-api and make generate-client-sdk", open)
	}

	for _, pointer := range undescribedAtTheGate {
		if _, still := found[pointer]; !still {
			t.Errorf("%s is listed in undescribedAtTheGate and the tree describes it.\n"+
				"  Delete the entry. A list that describes nothing is one more thing to keep true.", pointer)
		}
	}
}

// documentGaps reads the committed OpenAPI document, with the floors that say
// the gate is reading the whole of it.
func documentGaps(t *testing.T) []gap {
	t.Helper()

	gaps, operations, reached := documentGapsIn(t, committedDocument, newSourceIndex(t, httpapiDir, faultDir))

	if operations < 80 {
		t.Fatalf("the document declares %d operations, so this gate is reading too little", operations)
	}

	if reached < 50 {
		t.Fatalf("the operations reach %d schemas, so this gate is reading too little", reached)
	}

	return gaps
}

// documentGapsIn reads one OpenAPI document: every operation, every parameter,
// and every property of every schema an operation reaches. It returns the gaps
// with how many operations it read and how many schemas they reach.
func documentGapsIn(t *testing.T, path string, source *sourceIndex) ([]gap, int, int) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the document: %v", err)
	}

	var doc contractDocument
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode the document: %v", err)
	}

	var (
		gaps    []gap
		reached = map[string]struct{}{}
		queue   []string
		count   int
	)

	// enqueue records every component a schema names, transitively.
	var enqueue func(raw json.RawMessage)
	enqueue = func(raw json.RawMessage) {
		if len(raw) == 0 {
			return
		}

		var s describedSchema
		if json.Unmarshal(raw, &s) != nil {
			return
		}

		if name, ok := strings.CutPrefix(s.Ref, "#/components/schemas/"); ok {
			if _, seen := reached[name]; !seen {
				reached[name] = struct{}{}
				queue = append(queue, name)
			}
		}

		for _, p := range s.Properties {
			enqueue(p)
		}

		enqueue(s.Items)
		enqueue(s.AdditionalProperties)

		for _, list := range [][]json.RawMessage{s.AllOf, s.AnyOf, s.OneOf} {
			for _, item := range list {
				enqueue(item)
			}
		}
	}

	for path, item := range doc.Paths {
		for method, raw := range item {
			if !slices.Contains(httpMethods, method) {
				continue
			}

			count++

			var op describedOperation
			if err = json.Unmarshal(raw, &op); err != nil {
				t.Fatalf("decode %s %s: %v", method, path, err)
			}

			where := source.operation(op.ID)
			pointer := fmt.Sprintf("operation %s (%s %s)", op.ID, strings.ToUpper(method), path)

			if strings.TrimSpace(op.Summary) == "" {
				gaps = append(gaps, gap{pointer, "Set Summary on the huma.Operation at " + where +
					": one line saying what the operation does."})
			}

			if strings.TrimSpace(op.Description) == "" {
				gaps = append(gaps, gap{pointer, "Set Description on the huma.Operation at " + where +
					": what it answers, what it refuses, and what a caller does next."})
			}

			for _, p := range op.Parameters {
				if strings.TrimSpace(p.Description) == "" {
					gaps = append(gaps, gap{
						fmt.Sprintf("%s parameter %s in %s", pointer, p.Name, p.In),
						"Add a doc tag to the field carrying " + p.In + ":\"" + p.Name + "\" on the input struct near " +
							where + ".",
					})
				}

				enqueue(p.Schema)
			}

			if op.RequestBody != nil {
				for _, c := range op.RequestBody.Content {
					enqueue(c.Schema)
				}
			}

			for _, r := range op.Responses {
				for _, c := range r.Content {
					enqueue(c.Schema)
				}
			}
		}
	}

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		raw, ok := doc.Components.Schemas[name]
		if !ok {
			t.Errorf("an operation references #/components/schemas/%s, which the document does not define", name)

			continue
		}

		var s describedSchema
		if err = json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("decode schema %s: %v", name, err)
		}

		for prop, propRaw := range s.Properties {
			var p describedSchema
			if err = json.Unmarshal(propRaw, &p); err != nil {
				t.Fatalf("decode %s.%s: %v", name, prop, err)
			}

			if strings.TrimSpace(p.Description) == "" {
				gaps = append(gaps, gap{
					fmt.Sprintf("schema %s property %s", name, prop),
					"Add doc:\"...\" to the field carrying json:\"" + prop + "\" at " + source.field(name, prop) +
						": one sentence saying what the value is.",
				})
			}

			enqueue(propRaw)
		}

		enqueue(s.Items)
		enqueue(s.AdditionalProperties)

		for _, list := range [][]json.RawMessage{s.AllOf, s.AnyOf, s.OneOf} {
			for _, item := range list {
				enqueue(item)
			}
		}
	}

	return gaps, count, len(reached)
}

// TestAnUndescribedEntryIsRefused runs the document walk over a fixture with
// one gap of each kind: an operation with no summary, a parameter with no
// description, a property reached through a reference with no description,
// and a schema no operation reaches, whose gap must not be reported.
func TestAnUndescribedEntryIsRefused(t *testing.T) {
	t.Parallel()

	fixture := `{
  "paths": {
    "/things/{id}": {
      "get": {
        "operationId": "get-thing",
        "description": "Reads one thing.",
        "parameters": [{"name": "id", "in": "path", "schema": {"type": "string"}}],
        "responses": {"200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Thing"}}}}}
      }
    }
  },
  "components": {
    "schemas": {
      "Thing": {"properties": {
        "name": {"type": "string", "description": "The label."},
        "parts": {"type": "array", "items": {"$ref": "#/components/schemas/Part"}}
      }},
      "Part": {"properties": {"weight": {"type": "integer"}}},
      "Orphan": {"properties": {"unused": {"type": "string"}}}
    }
  }
}`

	path := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	gaps, operations, reached := documentGapsIn(t, path, newSourceIndex(t, httpapiDir))

	if operations != 1 || reached != 2 {
		t.Fatalf("read %d operations reaching %d schemas, want 1 reaching 2", operations, reached)
	}

	var pointers []string
	for _, g := range gaps {
		pointers = append(pointers, g.pointer)
	}

	sort.Strings(pointers)

	want := []string{
		"operation get-thing (GET /things/{id})",
		"operation get-thing (GET /things/{id}) parameter id in path",
		"schema Part property weight",
		"schema Thing property parts",
	}

	if !slices.Equal(pointers, want) {
		t.Errorf("the fixture reported %q, want %q", pointers, want)
	}
}

// eventTypeGaps reads the event-type set: every entry carries a summary and a
// description.
func eventTypeGaps(t *testing.T) []gap {
	t.Helper()

	file, err := readRegistry[struct {
		Entries []struct {
			Type        string `json:"type"`
			Summary     string `json:"summary"`
			Description string `json:"description"`
		} `json:"entries"`
	}](committedEventTypes)
	if err != nil {
		t.Fatalf("read the event types: %v", err)
	}

	if len(file.Entries) < 10 {
		t.Fatalf("the event-type set holds %d entries, so this gate is reading too little", len(file.Entries))
	}

	source := newSourceIndex(t, eventDir)

	var gaps []gap

	for _, e := range file.Entries {
		where := source.literal(e.Type)

		if strings.TrimSpace(e.Summary) == "" {
			gaps = append(gaps, gap{"event type " + e.Type,
				"Set Summary on the descriptor at " + where + ": one line saying what happened."})
		}

		if strings.TrimSpace(e.Description) == "" {
			gaps = append(gaps, gap{"event type " + e.Type,
				"Set Description on the descriptor at " + where +
					": what the payload carries and what a reader that missed it has lost."})
		}
	}

	return gaps
}

// errorCodeGaps reads the error-code registry: every code carries a summary
// and an action.
func errorCodeGaps(t *testing.T) []gap {
	t.Helper()

	file, err := readRegistry[errorCodesFile](committedErrorCodes)
	if err != nil {
		t.Fatalf("read the error codes: %v", err)
	}

	if len(file.Codes) < 50 {
		t.Fatalf("the error-code registry holds %d codes, so this gate is reading too little", len(file.Codes))
	}

	source := newSourceIndex(t, faultDir)

	var gaps []gap

	for _, c := range file.Codes {
		where := source.literal(c.Code)

		if strings.TrimSpace(c.Summary) == "" {
			gaps = append(gaps, gap{"error code " + c.Code,
				"Set Summary on the registration at " + where + ": one line saying what was refused."})
		}

		if strings.TrimSpace(c.Action) == "" {
			gaps = append(gaps, gap{"error code " + c.Code,
				"Set Action on the registration at " + where + ": what the caller does about it."})
		}
	}

	return gaps
}

// sourceIndex finds the line a fix goes on, so a refusal names a place and not
// only a JSON pointer. It is a text search over the packages' non-test files:
// a schema name is a Go type name, an operation id is a string literal, and a
// property is a json tag.
type sourceIndex struct {
	dirs  []string
	files map[string][]string
}

func newSourceIndex(t *testing.T, dirs ...string) *sourceIndex {
	t.Helper()

	index := &sourceIndex{dirs: dirs, files: map[string][]string{}}

	for _, dir := range dirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}

		before := len(index.files)

		for _, path := range matches {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}

			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read %s: %v", path, readErr)
			}

			index.files[filepath.ToSlash(path)] = strings.Split(string(data), "\n")
		}

		if len(index.files) == before {
			t.Fatalf("%s holds no Go source, so no refusal can name a line", dir)
		}
	}

	return index
}

// operation is where an operation id is registered.
func (s *sourceIndex) operation(id string) string {
	return s.literal(id)
}

// literal is where a string literal first appears.
func (s *sourceIndex) literal(value string) string {
	needle := `"` + value + `"`

	for _, name := range s.sortedFiles() {
		for i, line := range s.files[name] {
			if strings.Contains(line, needle) {
				return s.at(name, i)
			}
		}
	}

	return "the packages " + strings.Join(s.dirs, ", ") + " (no line carries " + needle + ")"
}

// embedded matches a field with no name, which is a struct whose fields are
// flattened into this one: `fault.Problem` or `Payload`.
var embedded = regexp.MustCompile(`^\s+(?:\*)?(?:[A-Za-z_]\w*\.)?([A-Za-z_]\w*)\s*$`)

// field is where a json-tagged field of a schema is declared. Huma names an
// anonymous Body struct after its parent (healthOutput.Body is
// HealthOutputBody), so the search tries the schema name and the parent's name
// with the first letter in either case, and follows an embedded struct when
// the tag is not declared directly.
func (s *sourceIndex) field(schema, property string) string {
	candidates := []string{schema, lowerFirst(schema)}
	if parent, ok := strings.CutSuffix(schema, "Body"); ok {
		candidates = append(candidates, parent, lowerFirst(parent))
	}

	tag := regexp.MustCompile(`json:"` + regexp.QuoteMeta(property) + `[",]`)

	if where, ok := s.fieldIn(candidates, tag, embedDepth); ok {
		return where
	}

	return "the struct rendered as " + schema + " in " + strings.Join(s.dirs, ", ") +
		" (no type declares it by that name)"
}

func (s *sourceIndex) fieldIn(candidates []string, tag *regexp.Regexp, depth int) (string, bool) {
	if depth == 0 {
		return "", false
	}

	for _, name := range s.sortedFiles() {
		lines := s.files[name]

		for _, candidate := range candidates {
			start := slices.IndexFunc(lines, func(line string) bool {
				return strings.HasPrefix(line, "type "+candidate+" struct")
			})
			if start < 0 {
				continue
			}

			var embeds []string

			for i := start + 1; i < len(lines) && !strings.HasPrefix(lines[i], "}"); i++ {
				if tag.MatchString(lines[i]) {
					return s.at(name, i), true
				}

				if m := embedded.FindStringSubmatch(lines[i]); m != nil {
					embeds = append(embeds, m[1])
				}
			}

			if len(embeds) > 0 {
				if where, ok := s.fieldIn(embeds, tag, depth-1); ok {
					return where, true
				}
			}
		}
	}

	return "", false
}

func (s *sourceIndex) sortedFiles() []string {
	names := make([]string, 0, len(s.files))
	for name := range s.files {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

func (s *sourceIndex) at(name string, index int) string {
	return name + ":" + strconv.Itoa(index+1)
}

func lowerFirst(name string) string {
	if name == "" {
		return name
	}

	runes := []rune(name)
	runes[0] = unicode.ToLower(runes[0])

	return string(runes)
}
