// Package routesapi is the HTTP surface a module serves.
//
// A module does not listen on a port. It answers `routes.serve` over the wire,
// and the host mounts it under /api/v1/modules/<namespace>, or at a root prefix
// such as /graphql when the operator names one. The host resolves the caller
// and decides the permission before it makes the call.
package routesapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// MethodServe is the call a host makes to reach a module's HTTP surface. It
// takes one request and answers one response. The host authorizes the caller
// before it makes this call.
const MethodServe wirerpc.Method = "routes.serve"

// MaxModuleBody bounds one request body and one response body on this method.
// A module that moves a file uses a stream.
const MaxModuleBody = 1 << 20

// Request is one HTTP request as a module receives it.
//
// Path is relative to the mount: a module writes /spools and is reached at
// /api/v1/modules/<namespace>/spools. It never includes the mount.
//
// Principal is who the host authorized. The host sets it, not the caller.
type Request struct {
	Method    string              `json:"method"`
	Path      string              `json:"path"`
	Query     string              `json:"query,omitempty"`
	Headers   map[string][]string `json:"headers,omitempty"`
	Body      []byte              `json:"body,omitempty"`
	Principal string              `json:"principal"`
}

// Response is what a module answers with. A zero Status is read as 200.
type Response struct {
	Status  int                 `json:"status,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"`
}

// hopByHop are the headers that describe one connection and are never
// forwarded (RFC 9110 §7.6.1).
//
//nolint:gochecknoglobals // a fixed set both directions read; a const cannot hold a slice.
var hopByHop = []string{
	"connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
	"te", "trailer", "transfer-encoding", "upgrade",
}

// Serve adapts an ordinary http.Handler onto MethodServe, so a module author
// writes net/http. Register it in the module's method set:
//
//	wirerpc.Methods{routesapi.MethodServe: routesapi.Serve(mux)}
func Serve(handler http.Handler) wirerpc.Handler {
	return func(ctx context.Context, call wirerpc.Call) (any, error) {
		var request Request
		if err := json.Unmarshal(call.Params, &request); err != nil {
			return nil, fmt.Errorf("routesapi: read the request: %w", err)
		}

		inner, err := request.HTTP(ctx)
		if err != nil {
			return nil, err
		}

		recorder := &recorder{header: http.Header{}}
		handler.ServeHTTP(recorder, inner)

		return recorder.response(), nil
	}
}

// HTTP renders the request as an *http.Request, for a module that does not
// use Serve.
func (r Request) HTTP(ctx context.Context) (*http.Request, error) {
	if len(r.Body) > MaxModuleBody {
		return nil, fmt.Errorf("routesapi: the body is %d bytes and the limit is %d",
			len(r.Body), MaxModuleBody)
	}

	target := r.Path
	if r.Query != "" {
		target += "?" + r.Query
	}

	inner, err := http.NewRequestWithContext(ctx, r.Method, target, strings.NewReader(string(r.Body)))
	if err != nil {
		return nil, fmt.Errorf("routesapi: build the request: %w", err)
	}

	for name, values := range r.Headers {
		if forwardable(name) {
			inner.Header[http.CanonicalHeaderKey(name)] = values
		}
	}

	inner.ContentLength = int64(len(r.Body))

	return inner.WithContext(context.WithValue(inner.Context(), principalKey{}, r.Principal)), nil
}

// principalKey is the context key of the principal the host authorized.
type principalKey struct{}

// PrincipalOf returns who the host authorized for this request, and empty for a
// request that did not come through the host. Use it, never an identity
// header, which the caller could have written.
func PrincipalOf(r *http.Request) string {
	if r == nil {
		return ""
	}

	principal, _ := r.Context().Value(principalKey{}).(string)

	return principal
}

// Write renders the response onto a ResponseWriter. Only the headers
// ResponseHeaders passes are written.
func (r Response) Write(w http.ResponseWriter) error {
	for name, values := range ResponseHeaders(r.Headers) {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}

	status := r.Status
	if status == 0 {
		status = http.StatusOK
	}

	w.WriteHeader(status)

	if _, err := w.Write(r.Body); err != nil {
		return fmt.Errorf("routesapi: write the module's answer: %w", err)
	}

	return nil
}

// Validate refuses an answer this host will not relay: a status outside the
// range HTTP defines, and a body over the bound. A status of 0 means 200.
func (r Response) Validate() error {
	if r.Status != 0 && (r.Status < 100 || r.Status > 599) {
		return fmt.Errorf("routesapi: the module answered with status %d", r.Status)
	}

	if len(r.Body) > MaxModuleBody {
		return fmt.Errorf("routesapi: the module answered %d bytes and the limit is %d",
			len(r.Body), MaxModuleBody)
	}

	return nil
}

// recorder is the ResponseWriter Serve gives the module's handler. A write
// past MaxModuleBody is dropped, and the host refuses the whole answer.
type recorder struct {
	header  http.Header
	status  int
	body    []byte
	over    bool
	written bool
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) {
	if r.written {
		return
	}

	r.status = status
	r.written = true
}

func (r *recorder) Write(body []byte) (int, error) {
	if !r.written {
		r.WriteHeader(http.StatusOK)
	}

	if len(r.body)+len(body) > MaxModuleBody {
		r.over = true

		return len(body), nil
	}

	r.body = append(r.body, body...)

	return len(body), nil
}

// response is what the handler wrote, as a Response.
func (r *recorder) response() Response {
	if r.over {
		return Response{Status: http.StatusInternalServerError}
	}

	status := r.status
	if status == 0 {
		status = http.StatusOK
	}

	out := Response{Status: status, Body: r.body}

	if len(r.header) > 0 {
		out.Headers = map[string][]string{}
		for name, values := range r.header {
			if forwardable(name) {
				out.Headers[name] = values
			}
		}
	}

	return out
}

// forwardable reports whether a header is not hop-by-hop.
func forwardable(name string) bool {
	return !slices.Contains(hopByHop, strings.ToLower(name))
}
