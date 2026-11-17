// Package openapidoc is the subset of an OpenAPI 3.1 document the generator
// commands read, and the one reading of it they share.
//
// cmd/generate-client-sdk types a method from it and cmd/generate-sdk-reference
// writes a page from it. Both need the same answers: which path-item keys are
// operations, which parameters an operation takes once the path item's are
// merged in, which body the runtime sends, and how a successful answer is
// read. Two readers of one document can give two answers, so the reading
// lives here and each command keeps only the shape it renders.
//
// No schema is resolved. A method is typed by indexed access into the
// openapi-typescript output, and a page names a schema by the component a
// $ref points at, so the id, the method, the path, the parameters, the media
// types, the summary, the description and the examples are the whole input.
// An example is kept as the JSON the document holds, because a page prints it
// and a client never decodes it.
package openapidoc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
)

// Document is the paths object, with each operation left raw until
// Operations decodes it, and the tags the document declares in the order it
// lists them.
type Document struct {
	Tags  []Tag                                 `json:"tags"`
	Paths map[string]map[string]json.RawMessage `json:"paths"`
	// Components holds the schemas a $ref points at. A generator that types a
	// method never resolves one; the compatibility gate does, to ask whether
	// a field the mapping names is one the document declares.
	Components Components `json:"components"`
}

// Components is the components object, of which only the schemas are read.
type Components struct {
	Schemas map[string]Schema `json:"schemas"`
}

// Tag is one tag the document declares. The order of Document.Tags is the
// order a reference lists the resources in.
type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Operation is one route as the document declares it. Method and Path come
// from the path item the operation sits under.
type Operation struct {
	Method string `json:"-"`
	Path   string `json:"-"`

	ID          string              `json:"operationId"`
	Summary     string              `json:"summary"`
	Description string              `json:"description"`
	Tags        []string            `json:"tags"`
	Parameters  []Parameter         `json:"parameters"`
	RequestBody *Body               `json:"requestBody"`
	Responses   map[string]Response `json:"responses"`
	// Security is kept raw because absent and empty mean different things:
	// absent inherits the document's requirement, `[]` takes no credential.
	Security json.RawMessage `json:"security"`
	// SampleExcuse is the sentence the server wrote for an operation that
	// carries no sample, under the x-sample-excuse extension. Empty means the
	// operation carries one.
	SampleExcuse string `json:"x-sample-excuse"`
	// SampleQuery is the query string the sample sends, under the
	// x-sample-query extension, without the leading question mark.
	SampleQuery string `json:"x-sample-query"`
	// SampleHeaders are the request headers the sample sends beside the
	// credential, under the x-sample-headers extension.
	SampleHeaders map[string]string `json:"x-sample-headers"`
	// LongRunning is set under x-long-running for an operation whose answer
	// waits on a machine. A generated client leaves its own deadline off one,
	// because a deadline there cuts off work that is still running.
	LongRunning bool `json:"x-long-running"`
}

// Parameter is one path, query or header parameter.
type Parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
	Schema      Schema `json:"schema"`
	// Example is the value the parameter object carries itself. The server
	// writes a field's example tag into the schema instead, so ExampleValue
	// reads both places.
	Example json.RawMessage `json:"example"`
}

// Schema is what a page can name of a schema: the component a $ref points
// at, or the primitive type.
type Schema struct {
	Ref string `json:"$ref"`
	// Type is kept raw: OpenAPI 3.1 lets it be one name or a list of them,
	// `["string", "null"]` for a nullable value.
	Type   json.RawMessage `json:"type"`
	Format string          `json:"format"`
	Items  *Schema         `json:"items"`
	// Enum is kept raw: a member may be a string or a number.
	Enum []json.RawMessage `json:"enum"`
	// Examples is where the server writes a parameter field's example tag.
	Examples []json.RawMessage `json:"examples"`
	// Properties and Required are what an object schema declares, read by the
	// compatibility gate and by nothing that renders.
	Properties map[string]Schema `json:"properties"`
	Required   []string          `json:"required"`
	// AllOf, OneOf and AnyOf are the composed forms. A property reached through
	// one of them is still a property the document declares.
	AllOf []Schema `json:"allOf"`
	OneOf []Schema `json:"oneOf"`
	AnyOf []Schema `json:"anyOf"`
}

// Body is a request body: whether it is required, and what it may carry.
type Body struct {
	Required bool               `json:"required"`
	Content  map[string]Content `json:"content"`
}

// Response is one status an operation answers with.
type Response struct {
	Description string             `json:"description"`
	Content     map[string]Content `json:"content"`
}

// Content is one media type's schema, and the example the server attached to
// it. The example is kept raw: a JSON body is rendered as it was published,
// and a text body, an SDP offer or a gcode file, is a JSON string.
type Content struct {
	Schema  Schema          `json:"schema"`
	Example json.RawMessage `json:"example"`
}

// ResponseKind is how the runtime reads a successful answer.
type ResponseKind string

// The four ways a successful answer is read.
const (
	ResponseJSON ResponseKind = "json"
	ResponseText ResponseKind = "text"
	ResponseRaw  ResponseKind = "raw"
	ResponseNone ResponseKind = "none"
)

// MediaJSON is the media type the runtime sends and reads as JSON.
const MediaJSON = "application/json"

const (
	mediaOctetStream = "application/octet-stream"
	schemaPrefix     = "#/components/schemas/"
)

// TextMedia is what the runtime answers as a string. Everything else that is
// not JSON is answered as the Response itself, so a stream or an image is
// consumed by the caller.
var TextMedia = []string{"text/plain", "text/html", "application/sdp"}

// HTTPMethods is the set of path-item keys that are operations. A path item
// may also carry `parameters` or `summary`, which are not.
var HTTPMethods = []string{"get", "put", "post", "delete", "patch", "head", "options", "trace"}

// The two ways a document is not a document this package can read.
var (
	ErrNoOperations = errors.New("the document declares no operation")
	ErrDuplicateID  = errors.New("two operations share an operationId")
)

// JSONSuccess is one success status answering JSON, with the component it
// carries.
type JSONSuccess struct {
	Status string
	Schema string
}

// Read decodes the document at path.
func Read(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	doc, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}

	return doc, nil
}

// Decode decodes a document held in memory.
func Decode(data []byte) (*Document, error) {
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	return &doc, nil
}

// Component resolves a $ref of the `#/components/schemas/Name` form to the
// schema it names. The second result is false for any other reference and for
// a name the document does not declare.
func (d *Document) Component(ref string) (Schema, bool) {
	name, ok := strings.CutPrefix(ref, schemaPrefix)
	if !ok {
		return Schema{}, false
	}

	schema, ok := d.Components.Schemas[name]

	return schema, ok
}

// Operations lists every operation in the document, sorted by id so the
// output is stable whatever order the document holds them in. A path item's
// own parameters reach every operation under it.
func (d *Document) Operations() ([]Operation, error) {
	var ops []Operation

	seen := map[string]struct{}{}

	for path, item := range d.Paths {
		shared, err := itemParameters(item)
		if err != nil {
			return nil, fmt.Errorf("decode the parameters of %s: %w", path, err)
		}

		for method, raw := range item {
			if !slices.Contains(HTTPMethods, method) {
				continue
			}

			var op Operation
			if err = json.Unmarshal(raw, &op); err != nil {
				return nil, fmt.Errorf("decode %s %s: %w", strings.ToUpper(method), path, err)
			}

			if _, dup := seen[op.ID]; dup {
				return nil, fmt.Errorf("%w: %s", ErrDuplicateID, op.ID)
			}

			seen[op.ID] = struct{}{}

			op.Method = strings.ToUpper(method)
			op.Path = path
			op.Parameters = mergeParameters(shared, op.Parameters)

			ops = append(ops, op)
		}
	}

	if len(ops) == 0 {
		return nil, ErrNoOperations
	}

	sort.Slice(ops, func(i, j int) bool { return ops[i].ID < ops[j].ID })

	return ops, nil
}

// itemParameters reads the parameters a path item declares for every
// operation under it. The core's document declares each one on the operation,
// and a module's fragment may declare a path parameter once on the item, as
// OpenAPI allows.
func itemParameters(item map[string]json.RawMessage) ([]Parameter, error) {
	raw, ok := item["parameters"]
	if !ok {
		return nil, nil
	}

	var params []Parameter
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}

	return params, nil
}

// mergeParameters is the item's parameters under the operation's own. An
// operation that restates one by name and location wins, which is the
// override OpenAPI gives it.
func mergeParameters(shared, own []Parameter) []Parameter {
	if len(shared) == 0 {
		return own
	}

	merged := make([]Parameter, 0, len(shared)+len(own))

	for _, p := range shared {
		restated := slices.ContainsFunc(own, func(o Parameter) bool {
			return o.Name == p.Name && o.In == p.In
		})
		if !restated {
			merged = append(merged, p)
		}
	}

	return append(merged, own...)
}

// Name is the method name a caller types: the operationId in camel case.
func (op Operation) Name() string {
	return CamelCase(op.ID)
}

// Anonymous reports whether the document publishes the operation with an
// empty security requirement, so no credential is sent.
func (op Operation) Anonymous() bool {
	return string(bytes.TrimSpace(op.Security)) == "[]"
}

// ParametersIn lists the operation's parameters of one location, in the
// order the document declares them.
func (op Operation) ParametersIn(in string) []Parameter {
	var params []Parameter

	for _, p := range op.Parameters {
		if p.In == in {
			params = append(params, p)
		}
	}

	return params
}

// Sample is one operation's worked example as the document publishes it: the
// body the caller sends, and the body and status the server answers with.
type Sample struct {
	// RequestMedia and Request are the body sent, or empty for an operation
	// that takes none.
	RequestMedia string
	Request      json.RawMessage
	// Status and ResponseMedia say which answer the example is attached to.
	Status        string
	ResponseMedia string
	Response      json.RawMessage
}

// Sample reads the example off the operation: the request example under the
// media type the runtime sends, and the response example under the lowest
// success status that carries one, JSON before any other media type. The
// second result is false for an operation that publishes no response example.
func (op Operation) Sample() (Sample, bool) {
	sample := Sample{}

	if op.RequestBody != nil {
		sample.RequestMedia = op.RequestBody.SentMedia()
		sample.Request = op.RequestBody.Content[sample.RequestMedia].Example
	}

	statuses := make([]string, 0, len(op.Responses))
	for status := range op.Responses {
		if strings.HasPrefix(status, "2") {
			statuses = append(statuses, status)
		}
	}

	sort.Strings(statuses)

	for _, status := range statuses {
		media, found := exampleMedia(op.Responses[status].Content)
		if !found {
			continue
		}

		sample.Status = status
		sample.ResponseMedia = media
		sample.Response = op.Responses[status].Content[media].Example

		return sample, true
	}

	return Sample{}, false
}

// exampleMedia is the media type of a content map that carries an example,
// JSON first because it is what the generated method reads, then the rest in
// name order so the answer is the same on every run.
func exampleMedia(content map[string]Content) (string, bool) {
	if len(content[MediaJSON].Example) > 0 {
		return MediaJSON, true
	}

	names := make([]string, 0, len(content))
	for media := range content {
		names = append(names, media)
	}

	sort.Strings(names)

	for _, media := range names {
		if len(content[media].Example) > 0 {
			return media, true
		}
	}

	return "", false
}

// ExampleValue is the value a rendered sample fills the parameter with: the
// parameter object's own example when it carries one, otherwise the first
// example on its schema, which is where the server writes a field's example
// tag. The second result is false for a parameter with neither.
func (p Parameter) ExampleValue() (json.RawMessage, bool) {
	if len(p.Example) > 0 {
		return p.Example, true
	}

	if len(p.Schema.Examples) > 0 {
		return p.Schema.Examples[0], true
	}

	return nil, false
}

// Success classifies the success responses. JSON wins, because it is what
// the types file can name; a stream or an image is handed over as the
// Response; a text type is read as a string; no content is void. The JSON
// statuses come back sorted, each with the component it carries.
func (op Operation) Success() (ResponseKind, []JSONSuccess) {
	var (
		jsonStatuses []JSONSuccess
		raw, text    bool
	)

	for status, response := range op.Responses {
		if !strings.HasPrefix(status, "2") {
			continue
		}

		for media, content := range response.Content {
			switch {
			case media == MediaJSON:
				jsonStatuses = append(jsonStatuses, JSONSuccess{Status: status, Schema: content.Schema.Name()})
			case slices.Contains(TextMedia, media):
				text = true
			default:
				raw = true
			}
		}
	}

	sort.Slice(jsonStatuses, func(i, j int) bool { return jsonStatuses[i].Status < jsonStatuses[j].Status })

	switch {
	case len(jsonStatuses) > 0:
		return ResponseJSON, jsonStatuses
	case raw:
		return ResponseRaw, nil
	case text:
		return ResponseText, nil
	default:
		return ResponseNone, nil
	}
}

// SentMedia picks the request body the runtime sends: JSON when the document
// offers it, otherwise the one raw type the caller supplies. An offer of both
// octet-stream and sdp (the WebRTC offer) is one raw body either way.
func (b *Body) SentMedia() string {
	if b == nil {
		return ""
	}

	if _, ok := b.Content[MediaJSON]; ok {
		return MediaJSON
	}

	media := b.MediaTypes()
	if len(media) == 0 {
		return ""
	}

	// The most specific type first, so `application/sdp` wins over the
	// `application/octet-stream` alias the document lists beside it.
	if len(media) > 1 && media[0] == mediaOctetStream {
		return media[1]
	}

	return media[0]
}

// MediaTypes lists every media type the body may carry, in name order.
func (b *Body) MediaTypes() []string {
	if b == nil {
		return nil
	}

	media := make([]string, 0, len(b.Content))
	for m := range b.Content {
		media = append(media, m)
	}

	sort.Strings(media)

	return media
}

// Name is the short name a reader looks up: the component a $ref names, or
// the primitive type the schema declares.
func (s Schema) Name() string {
	if name, ok := strings.CutPrefix(s.Ref, schemaPrefix); ok {
		return name
	}

	typ := s.typeNames()

	switch {
	case typ == "array" && s.Items != nil:
		return "array of " + s.Items.Name()
	case s.Format == "binary":
		return "binary"
	case len(s.Enum) > 0:
		return "one of " + rawList(s.Enum)
	case typ != "":
		return typ
	default:
		return "object"
	}
}

// typeNames spells the schema's type: the one name, or the names joined
// with `or` when the document lists several.
func (s Schema) typeNames() string {
	if len(s.Type) == 0 {
		return ""
	}

	if s.Type[0] == '[' {
		var names []string
		if err := json.Unmarshal(s.Type, &names); err != nil {
			return strings.Trim(string(s.Type), "[]")
		}

		return strings.Join(names, " or ")
	}

	return strings.Trim(string(s.Type), `"`)
}

// rawList spells the members of an enum as the document does, without the
// quotes JSON puts around a string.
func rawList(members []json.RawMessage) string {
	spelled := make([]string, 0, len(members))
	for _, m := range members {
		spelled = append(spelled, strings.Trim(string(m), `"`))
	}

	return strings.Join(spelled, ", ")
}

// CamelCase turns an operationId like `list-device-files` into
// `listDeviceFiles`, the method name a caller types.
func CamelCase(id string) string {
	parts := strings.Split(id, "-")

	var b strings.Builder

	for i, part := range parts {
		if part == "" {
			continue
		}

		if i == 0 {
			b.WriteString(part)

			continue
		}

		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}

	return b.String()
}
