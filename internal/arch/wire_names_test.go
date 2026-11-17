package arch_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ADR-20260826-c77 says one name for one thing on the wire, and rule 21 says a
// published name is renamed only on a surface that has a gate. This file is
// that gate for the HTTP surface. It reads the committed OpenAPI document,
// which `make generate-openapi` writes from the registered operations, so it
// measures what the build serves and not what a person wrote down.
//
// Four rules are checkable from the document alone.
//
//   - rule 1: a bare entity id is `device_id`, and `printer_id` names nothing
//   - rule 9: an instant is an RFC 3339 string
//   - rule 10: a duration on the wire is an integer of milliseconds under a key
//     ending `_ms`
//   - rules 15 and 16: a list that pages by offset answers `items`, `total`,
//     `limit` and `offset`
//
// Rules 12 and 13 are not here. A gate cannot read a unit out of a key name it
// has never met, and two spellings of one product word are a judgement over two
// descriptions; ADR-20260826-c77's own enforcement table says so.
const openAPIDocument = "internal/transport/http/httpapi/openapi.json"

// bareIDSpellingsRefused are the member and parameter names rule 1 retired. The
// object is the device and the printer is its type (ADR-20260826-5yj), so a bare id is
// `device_id` on every surface and one of these on none.
//
//nolint:gochecknoglobals // a table two tests read; there is nowhere cheaper.
var bareIDSpellingsRefused = []string{"printer_id", "printerId", "printerID"}

// instantSuffixes are the endings that name an instant. A member named some
// other way is invisible to this gate, which ADR-20260826-c77's enforcement table
// states as the cost of reading a name rather than a type.
//
//nolint:gochecknoglobals // a table two tests read; there is nowhere cheaper.
var instantSuffixes = []string{"_at", "_time", "modified", "_since", "_timestamp"}

// durationWords are the words that name a span of time. A key carrying one is
// an integer of milliseconds and ends `_ms`.
//
// `ttl` is absent on purpose. Rule 11 keeps the CloudEvents envelope's `ttl` in
// seconds, because CloudEvents 1.0 allows no underscore in an attribute name.
//
//nolint:gochecknoglobals // a table two tests read; there is nowhere cheaper.
var durationWords = []string{
	"timeout", "interval", "duration", "elapsed", "remaining",
	"backoff", "cooldown", "latency", "uptime",
}

// durationsBelowAMillisecond are the keys that end `_ms` and hold a fraction
// rather than a count. Rule 10 asks for an integer, and an integer here would
// report zero for every reading the value exists to carry.
//
//nolint:gochecknoglobals // a table one test reads; there is nowhere cheaper.
var durationsBelowAMillisecond = map[string]string{
	"latency_ms": "a round trip between two servers on one network is under a " +
		"millisecond, so a whole number of milliseconds is zero for a healthy link",
}

// durationsAStandardNames are the durations whose name and unit another
// standard fixes, keyed where the gate reports them. A client written to that
// standard reads the member as it is, so renaming it would break the client
// the operation exists for.
//
//nolint:gochecknoglobals // a table one test reads; there is nowhere cheaper.
var durationsAStandardNames = map[string]string{
	"IssuedAppPairingBody.interval": "RFC 8628 section 3.2 names the polling interval interval, in " +
		"seconds, and a device authorization client reads it under that name (ADR-20261025-tpp rule 16)",
}

// cursorPagers are the operations that page and cannot answer `total` and
// `offset`. Rule 17 asks for this list: a list that does not page the way rule
// 15 sets out is named here with its reason, so the exception is read next to
// the rule rather than discovered by a client.
//
//nolint:gochecknoglobals // a table one test reads; there is nowhere cheaper.
var cursorPagers = map[string]string{
	"list-device-jobs": "a device that has printed every day for a year holds a row per " +
		"print, and counting them to render a pager is a scan of the whole table per page",
	"list-jobs": "the same reason as list-device-jobs, over every device the caller may " +
		"observe",
	"list-device-commands": "the rows come from a history provider, whose contract " +
		"hands back a cursor and never a count (ADR-20260901-rf2)",
	"list-device-history-facts":   "the same provider contract",
	"list-device-history-samples": "the same provider contract",
	"list-history-facts":          "the same provider contract",
	"list-history-samples":        "the same provider contract",
}

// TestNoWireNameSpellsABareIDPrinterID holds ADR-20260826-c77 rule 1 with ADR-20260826-5yj. The
// object is the device, so the bare id is `device_id` and nothing on the served
// surface is called `printer_id`.
func TestNoWireNameSpellsABareIDPrinterID(t *testing.T) {
	t.Parallel()

	document := servedDocument(t)

	var violations []string

	for _, property := range documentProperties(document) {
		for _, refused := range bareIDSpellingsRefused {
			if property.name == refused {
				violations = append(violations,
					fmt.Sprintf("%s is named %s, and rule 1 spells a bare id device_id",
						property.where, refused))
			}
		}
	}

	for _, parameter := range documentParameters(document) {
		for _, refused := range bareIDSpellingsRefused {
			if parameter.name == refused {
				violations = append(violations,
					fmt.Sprintf("%s is named %s, and rule 1 spells a bare id device_id",
						parameter.where, refused))
			}
		}
	}

	reportWireViolations(t, violations)
}

// TestAnInstantOnTheWireIsAnRFC3339String holds ADR-20260826-c77 rule 9. A member whose
// name says it carries an instant is a string with format date-time, whatever
// precision the device that supplied it had.
func TestAnInstantOnTheWireIsAnRFC3339String(t *testing.T) {
	t.Parallel()

	document := servedDocument(t)

	var violations []string

	for _, property := range documentProperties(document) {
		if !namesAnInstant(property.name) {
			continue
		}

		if schemaType(property.schema) == "string" && property.schema["format"] == "date-time" {
			continue
		}

		violations = append(violations, fmt.Sprintf(
			"%s names an instant and is %s/%v, and rule 9 wants string/date-time",
			property.where, schemaType(property.schema), property.schema["format"]))
	}

	reportWireViolations(t, violations)
}

// TestAWireDurationIsAnIntegerOfMilliseconds holds ADR-20260826-c77 rule 10. A key that
// carries a span of time ends `_ms` and holds an integer, so a client never has
// to read a description to learn the unit.
func TestAWireDurationIsAnIntegerOfMilliseconds(t *testing.T) {
	t.Parallel()

	document := servedDocument(t)

	check := func(name string, schema map[string]any, where string) string {
		if !namesADuration(name) {
			return ""
		}

		if _, standard := durationsAStandardNames[where]; standard {
			return ""
		}

		if !strings.HasSuffix(name, "_ms") {
			return fmt.Sprintf("%s names a duration and does not end _ms", where)
		}

		if _, fraction := durationsBelowAMillisecond[name]; fraction {
			return ""
		}

		if schemaType(schema) != "integer" {
			return fmt.Sprintf("%s ends _ms and is %s, and rule 10 wants an integer",
				where, schemaType(schema))
		}

		return ""
	}

	var violations []string

	for _, property := range documentProperties(document) {
		if bad := check(property.name, property.schema, property.where); bad != "" {
			violations = append(violations, bad)
		}
	}

	for _, parameter := range documentParameters(document) {
		if bad := check(parameter.name, parameter.schema, parameter.where); bad != "" {
			violations = append(violations, bad)
		}
	}

	reportWireViolations(t, violations)
}

// TestAListThatPagesAnswersTheFourMembers holds ADR-20260826-c77 rules 15 to 17. An
// operation taking `limit` or `offset` answers `items`, `total`, `limit` and
// `offset`, or it is named in cursorPagers with the reason it cannot.
func TestAListThatPagesAnswersTheFourMembers(t *testing.T) {
	t.Parallel()

	document := servedDocument(t)
	wanted := []string{"items", "total", "limit", "offset"}

	var (
		violations []string
		checked    int
	)

	for _, operation := range listOperations(document) {
		if !operation.query["limit"] && !operation.query["offset"] {
			continue
		}

		if _, named := cursorPagers[operation.id]; named {
			continue
		}

		checked++

		for _, member := range wanted {
			if _, ok := operation.body[member]; !ok {
				violations = append(violations,
					fmt.Sprintf("%s pages and does not answer %s", operation.id, member))
			}
		}

		if !operation.query["offset"] {
			violations = append(violations,
				fmt.Sprintf("%s pages and takes no offset parameter", operation.id))
		}
	}

	if checked == 0 {
		t.Fatal("no operation pages by offset, so this gate passed over nothing")
	}

	reportWireViolations(t, violations)
}

// TestEveryCursorPagerNamedHereStillPages stops the exception list outliving
// the exceptions. An operation named in cursorPagers that no longer takes a
// cursor is a row nobody removed.
func TestEveryCursorPagerNamedHereStillPages(t *testing.T) {
	t.Parallel()

	document := servedDocument(t)
	found := map[string]bool{}

	for _, operation := range listOperations(document) {
		found[operation.id] = operation.query["cursor"]
	}

	var violations []string

	for id, reason := range cursorPagers {
		switch {
		case reason == "":
			violations = append(violations, id+" is named with no reason")
		case !found[id]:
			violations = append(violations, id+" is named here and takes no cursor parameter")
		}
	}

	reportWireViolations(t, violations)
}

// namedSchema is one property or parameter of the served document, with enough
// of its address for a failure to say where to look.
type namedSchema struct {
	name   string
	where  string
	schema map[string]any
}

// listOperation is one operation whose success body carries a list.
type listOperation struct {
	id    string
	query map[string]bool
	body  map[string]any
}

func namesAnInstant(name string) bool {
	if name == "time" {
		return true
	}

	for _, suffix := range instantSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}

	return false
}

func namesADuration(name string) bool {
	for _, word := range durationWords {
		if strings.Contains(name, word) {
			return true
		}
	}

	return false
}

// schemaType reads a schema's type, taking the first entry when the type is a
// list. Huma writes ["array", "null"] for a nullable array.
func schemaType(schema map[string]any) string {
	switch declared := schema["type"].(type) {
	case string:
		return declared
	case []any:
		for _, entry := range declared {
			if text, ok := entry.(string); ok && text != "null" {
				return text
			}
		}
	}

	return ""
}

// servedDocument reads the committed OpenAPI document. It is the document the
// build serves: httpapi's own spec test refuses a build whose surface differs.
func servedDocument(t *testing.T) map[string]any {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(repoRoot(t), openAPIDocument))
	if err != nil {
		t.Fatalf("read %s: %v", openAPIDocument, err)
	}

	var document map[string]any
	if unmarshalErr := json.Unmarshal(body, &document); unmarshalErr != nil {
		t.Fatalf("parse %s: %v", openAPIDocument, unmarshalErr)
	}

	return document
}

// documentProperties walks every schema under components and returns each
// property once per place it is declared.
func documentProperties(document map[string]any) []namedSchema {
	schemas, _ := mapAt(document, "components", "schemas")

	var found []namedSchema

	for _, name := range sortedMapKeys(schemas) {
		schema, ok := schemas[name].(map[string]any)
		if !ok {
			continue
		}

		found = append(found, walkProperties(schema, name)...)
	}

	return found
}

// walkProperties descends one schema, following properties, array items and
// the three composition keywords.
func walkProperties(schema map[string]any, where string) []namedSchema {
	var found []namedSchema

	if properties, ok := schema["properties"].(map[string]any); ok {
		for _, name := range sortedMapKeys(properties) {
			child, isObject := properties[name].(map[string]any)
			if !isObject {
				continue
			}

			found = append(found, namedSchema{name: name, where: where + "." + name, schema: child})
			found = append(found, walkProperties(child, where+"."+name)...)
		}
	}

	for _, keyword := range []string{"items", "additionalProperties"} {
		if child, ok := schema[keyword].(map[string]any); ok {
			found = append(found, walkProperties(child, where+"[]")...)
		}
	}

	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		branches, ok := schema[keyword].([]any)
		if !ok {
			continue
		}

		for _, branch := range branches {
			if child, isObject := branch.(map[string]any); isObject {
				found = append(found, walkProperties(child, where)...)
			}
		}
	}

	return found
}

// documentParameters returns every path, query and header parameter of every
// operation.
func documentParameters(document map[string]any) []namedSchema {
	var found []namedSchema

	forEachOperation(document, func(path, method string, operation map[string]any) {
		parameters, ok := operation["parameters"].([]any)
		if !ok {
			return
		}

		for _, entry := range parameters {
			parameter, isObject := entry.(map[string]any)
			if !isObject {
				continue
			}

			name, _ := parameter["name"].(string)
			schema, _ := parameter["schema"].(map[string]any)

			found = append(found, namedSchema{
				name:   name,
				where:  fmt.Sprintf("%s %s parameter %s", strings.ToUpper(method), path, name),
				schema: schema,
			})
		}
	})

	return found
}

// listOperations returns every operation whose success body carries exactly one
// array member, or an array named items, with the query parameters it takes and
// the members it answers.
func listOperations(document map[string]any) []listOperation {
	schemas, _ := mapAt(document, "components", "schemas")

	var found []listOperation

	forEachOperation(document, func(_, _ string, operation map[string]any) {
		id, _ := operation["operationId"].(string)
		if id == "" {
			return
		}

		body := successBody(operation, schemas)

		arrays := 0

		for _, member := range body {
			child, ok := member.(map[string]any)
			if ok && schemaType(resolve(child, schemas)) == "array" {
				arrays++
			}
		}

		// A list may carry arrays beside its rows, such as the filters and
		// the sort fields the device listing offers. Its rows are items.
		if arrays != 1 && !isArrayMember(body, "items", schemas) {
			return
		}

		query := map[string]bool{}

		if parameters, ok := operation["parameters"].([]any); ok {
			for _, entry := range parameters {
				parameter, isObject := entry.(map[string]any)
				if !isObject {
					continue
				}

				if in, _ := parameter["in"].(string); in == "query" {
					name, _ := parameter["name"].(string)
					query[name] = true
				}
			}
		}

		found = append(found, listOperation{id: id, query: query, body: body})
	})

	return found
}

// isArrayMember reports whether a body's member of that name is an array.
func isArrayMember(body map[string]any, name string, schemas map[string]any) bool {
	child, ok := body[name].(map[string]any)

	return ok && schemaType(resolve(child, schemas)) == "array"
}

// successBody returns the properties of an operation's 200 or 201 JSON body.
func successBody(operation map[string]any, schemas map[string]any) map[string]any {
	responses, ok := operation["responses"].(map[string]any)
	if !ok {
		return nil
	}

	for _, status := range []string{"200", "201"} {
		response, isObject := responses[status].(map[string]any)
		if !isObject {
			continue
		}

		schema, found := mapAt(response, "content", "application/json", "schema")
		if !found {
			continue
		}

		properties, _ := resolve(schema, schemas)["properties"].(map[string]any)

		return properties
	}

	return nil
}

// resolve follows a local $ref into the components section. The document holds
// no reference cycle, and a chain of references ends at a schema of its own.
func resolve(schema map[string]any, schemas map[string]any) map[string]any {
	for range 8 {
		reference, ok := schema["$ref"].(string)
		if !ok {
			return schema
		}

		target, found := schemas[reference[strings.LastIndex(reference, "/")+1:]].(map[string]any)
		if !found {
			return schema
		}

		schema = target
	}

	return schema
}

// forEachOperation calls visit once per operation of the document.
func forEachOperation(document map[string]any, visit func(path, method string, op map[string]any)) {
	paths, ok := document["paths"].(map[string]any)
	if !ok {
		return
	}

	methods := []string{"get", "put", "post", "delete", "patch", "head", "options"}

	for _, path := range sortedMapKeys(paths) {
		item, isObject := paths[path].(map[string]any)
		if !isObject {
			continue
		}

		for _, method := range methods {
			operation, found := item[method].(map[string]any)
			if found {
				visit(path, method, operation)
			}
		}
	}
}

// mapAt walks a chain of object keys and reports whether the whole chain
// resolved to an object.
func mapAt(node map[string]any, keys ...string) (map[string]any, bool) {
	for _, key := range keys {
		child, ok := node[key].(map[string]any)
		if !ok {
			return nil, false
		}

		node = child
	}

	return node, true
}

func sortedMapKeys(node map[string]any) []string {
	keys := make([]string, 0, len(node))
	for key := range node {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

// reportWireViolations fails once with every violation, so one run says
// everything that is wrong rather than the first thing.
func reportWireViolations(t *testing.T, violations []string) {
	t.Helper()

	if len(violations) == 0 {
		return
	}

	sort.Strings(violations)
	t.Fatalf("ADR-20260826-c77 is broken on %d names:\n  %s",
		len(violations), strings.Join(violations, "\n  "))
}
