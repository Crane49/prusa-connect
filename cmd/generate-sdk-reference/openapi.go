package main

import (
	"sort"

	"github.com/prusa3d/prusa-connect/internal/codegen/openapidoc"
)

// operation is one route as a page prints it. openapidoc is shared with
// cmd/generate-client-sdk, so both commands derive the same method names.
type operation struct {
	openapidoc.Operation

	// Name is the client method name.
	Name string
	// Params is path first, then query, then header, each in document order.
	Params    []parameter
	Body      *body
	Responses []response
}

// parameter is one path, query or header parameter.
type parameter struct {
	Name        string
	In          string
	Type        string
	Description string
	Required    bool
}

// body is the request body: every media type the document offers and its
// schema.
type body struct {
	Required bool
	Media    []mediaSchema
}

// response is one status the operation answers with.
type response struct {
	Status      string
	Description string
	Media       []mediaSchema
}

// mediaSchema is one media type and the name of its schema.
type mediaSchema struct {
	Media  string
	Schema string
}

func operationsOf(decoded []openapidoc.Operation) []operation {
	ops := make([]operation, 0, len(decoded))
	for _, d := range decoded {
		ops = append(ops, operationOf(d))
	}

	return ops
}

func operationOf(decoded openapidoc.Operation) operation {
	op := operation{Operation: decoded, Name: decoded.Name()}

	for _, in := range []string{"path", "query", "header"} {
		for _, p := range decoded.ParametersIn(in) {
			op.Params = append(op.Params, parameter{
				Name:        p.Name,
				In:          p.In,
				Type:        p.Schema.Name(),
				Description: p.Description,
				Required:    p.Required,
			})
		}
	}

	if decoded.RequestBody != nil {
		op.Body = &body{
			Required: decoded.RequestBody.Required,
			Media:    mediaOf(decoded.RequestBody.Content),
		}
	}

	for status, r := range decoded.Responses {
		op.Responses = append(op.Responses, response{
			Status:      status,
			Description: r.Description,
			Media:       mediaOf(r.Content),
		})
	}

	sort.Slice(op.Responses, func(i, j int) bool { return op.Responses[i].Status < op.Responses[j].Status })

	return op
}

// mediaOf lists the media types of a content map with their schema names.
// JSON comes first because the generated method uses it. The rest are sorted.
func mediaOf(content map[string]openapidoc.Content) []mediaSchema {
	media := make([]mediaSchema, 0, len(content))
	for m, c := range content {
		media = append(media, mediaSchema{Media: m, Schema: c.Schema.Name()})
	}

	sort.Slice(media, func(i, j int) bool {
		if (media[i].Media == openapidoc.MediaJSON) != (media[j].Media == openapidoc.MediaJSON) {
			return media[i].Media == openapidoc.MediaJSON
		}

		return media[i].Media < media[j].Media
	})

	return media
}
