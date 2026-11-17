package main

import (
	"slices"

	"github.com/prusa3d/prusa-connect/internal/codegen/openapidoc"
)

// The document types come from internal/codegen/openapidoc, shared with
// cmd/generate-sdk-reference.
type (
	document          = openapidoc.Document
	documentParameter = openapidoc.Parameter
	responseKind      = openapidoc.ResponseKind
)

const (
	responseJSON = openapidoc.ResponseJSON
	responseText = openapidoc.ResponseText
	responseRaw  = openapidoc.ResponseRaw
	responseNone = openapidoc.ResponseNone
)

const (
	mediaJSON    = openapidoc.MediaJSON
	mediaProblem = "application/problem+json"
	mediaBinary  = "application/octet-stream"
)

// httpMethods is the set of path-item keys that are operations. A path item
// may also have `parameters` or `summary`, which are not.
var httpMethods = openapidoc.HTTPMethods

// operation is one route as the emitter sees it.
type operation struct {
	ID      string
	Name    string
	Method  string
	Path    string
	Summary string

	PathParams   []string
	Query        []documentParameter
	Header       []documentParameter
	Body         string
	BodyRequired bool

	Response responseKind
	// JSONStatuses are the success statuses that answer JSON. The response
	// type is the union of their bodies.
	JSONStatuses []string
	Anonymous    bool
	// LongRunning is an operation whose answer waits on a machine.
	LongRunning bool
}

// RequestOptional reports whether a caller may omit the request object: no
// path parameter, no required body, no required query or header.
func (op operation) RequestOptional() bool {
	if len(op.PathParams) > 0 || (op.Body != "" && op.BodyRequired) {
		return false
	}

	return !anyRequired(op.Query) && !anyRequired(op.Header)
}

func anyRequired(params []documentParameter) bool {
	return slices.ContainsFunc(params, func(p documentParameter) bool { return p.Required })
}

func readDocument(path string) (*document, error) {
	return openapidoc.Read(path)
}

// operationsOf lists every operation in the document, sorted by id.
func operationsOf(doc *document) ([]operation, error) {
	decoded, err := doc.Operations()
	if err != nil {
		return nil, err
	}

	ops := make([]operation, 0, len(decoded))
	for _, d := range decoded {
		ops = append(ops, operationOf(d))
	}

	return ops, nil
}

func operationOf(decoded openapidoc.Operation) operation {
	op := operation{
		ID:          decoded.ID,
		Name:        decoded.Name(),
		Method:      decoded.Method,
		Path:        decoded.Path,
		Summary:     decoded.Summary,
		Anonymous:   decoded.Anonymous(),
		LongRunning: decoded.LongRunning,
	}

	for _, p := range decoded.Parameters {
		switch p.In {
		case "path":
			op.PathParams = append(op.PathParams, p.Name)
		case "query":
			op.Query = append(op.Query, p)
		case "header":
			op.Header = append(op.Header, p)
		}
	}

	if decoded.RequestBody != nil {
		op.Body = decoded.RequestBody.SentMedia()
		op.BodyRequired = decoded.RequestBody.Required
	}

	kind, successes := decoded.Success()
	op.Response = kind

	for _, s := range successes {
		op.JSONStatuses = append(op.JSONStatuses, s.Status)
	}

	return op
}
