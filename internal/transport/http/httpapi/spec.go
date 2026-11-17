package httpapi

import (
	"bytes"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/yaml"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// The paths the document is served at.
const (
	specJSONPath = "/openapi.json"
	specYAMLPath = "/openapi.yaml"
)

// The paths the API reference page is served at. The printer build answers
// 404 on both, because it leaves the page out.
const (
	docsPath       = "/docs"
	docsScriptPath = "/docs/scalar.js"
)

// specCache holds the OpenAPI document as bytes. The document is rendered as
// JSON on the first request for either form, and the YAML is made from those
// JSON bytes. Later requests get the same bytes.
//
// Rendering the document takes a lot of short-lived memory. Doing it while the
// server starts is enough to get the server killed on a small device, and most
// servers are never asked for the document.
//
// Huma serves its own document from a closure that caches on the first
// request without a lock, so two first requests race. Here the render runs
// behind a sync.Once instead, so it runs once whatever the number of first
// requests. The bytes never go stale, because the route set is fixed by the
// time New returns.
type specCache struct {
	json func() ([]byte, error)
	yaml func() ([]byte, error)

	// renders counts the renders of the document, so a test can see when they
	// happen.
	renders atomic.Int32
}

func newSpecCache(api huma.API) *specCache {
	cache := &specCache{}

	cache.json = sync.OnceValues(func() ([]byte, error) {
		cache.renders.Add(1)

		body, err := api.OpenAPI().MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("render the OpenAPI document as JSON: %w", err)
		}

		return body, nil
	})

	cache.yaml = sync.OnceValues(func() ([]byte, error) {
		body, err := cache.json()
		if err != nil {
			return nil, err
		}

		var out bytes.Buffer
		if err = yaml.Convert(&out, bytes.NewReader(body)); err != nil {
			return nil, fmt.Errorf("convert the OpenAPI document to YAML: %w", err)
		}

		return out.Bytes(), nil
	})

	return cache
}

func (c *specCache) serveJSON(w http.ResponseWriter, r *http.Request) {
	c.serve(w, r, "application/openapi+json", c.json)
}

func (c *specCache) serveYAML(w http.ResponseWriter, r *http.Request) {
	c.serve(w, r, "application/openapi+yaml", c.yaml)
}

// document returns the JSON rendering, for a caller that wants the bytes and
// not a response. The gate that compares the committed document uses it.
func (c *specCache) document() ([]byte, error) { return c.json() }

func (c *specCache) serve(w http.ResponseWriter, r *http.Request, mediaType string, render func() ([]byte, error)) {
	body, err := render()
	if err != nil {
		writeProblem(w, r, httpfault.Unexpected("the OpenAPI document could not be rendered").Wrap(err))

		return
	}

	w.Header().Set("Content-Type", mediaType)
	// The document changes with the binary. A browser that kept an old copy
	// would call an operation this build does not serve.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}
