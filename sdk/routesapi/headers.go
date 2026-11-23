package routesapi

import (
	"net/http"
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The headers that pass through the host between a person and a module. Both
// lists are allow-lists: a module gets the defaults and the headers the
// operator granted it, and nothing else.
//
// Into a module, the person's Cookie and Authorization do not pass, so a
// module cannot call the core as that person. PrincipalOf says who is calling.
//
// Out of a module, cookies, the security headers of the core's origin and
// Location do not pass.

// The request headers that pass into every module are
// manifest.DefaultRequestHeaders. A module asks for more in routes.headers of
// its manifest, and the host passes those the operator granted
// (ADR-20261118-nnm).

// responseHeaders are the headers of a module's answer that pass to the
// person, in lower case.
//
//nolint:gochecknoglobals // a closed table read on every answer, never written.
var responseHeaders = []string{
	"accept-ranges", "cache-control", "content-disposition", "content-language", "content-range",
	"content-type", "etag", "expires", "last-modified", "retry-after", "vary", "x-content-type-options",
}

// RequestHeaders is the part of headers a module receives. The host applies
// it to a person's request and to a call from another module.
func RequestHeaders(headers map[string][]string) map[string][]string {
	return passing(headers, manifest.DefaultRequestHeaders)
}

// GrantedRequestHeaders is the part of headers a module receives when the
// operator granted it the headers named in granted as well. A name the
// manifest never lets a module ask for is left out even when granted names it.
func GrantedRequestHeaders(headers map[string][]string, granted []string) map[string][]string {
	allowed := slices.Clone(manifest.DefaultRequestHeaders)

	for _, name := range granted {
		if manifest.CheckRequestHeader(name) == nil {
			allowed = append(allowed, strings.ToLower(name))
		}
	}

	return passing(headers, allowed)
}

// ResponseHeaders is the part of a module's answer the host relays.
func ResponseHeaders(headers map[string][]string) map[string][]string {
	return passing(headers, responseHeaders)
}

// passing copies the headers whose names are in allowed, under their
// canonical names, and answers nil when none is.
func passing(headers map[string][]string, allowed []string) map[string][]string {
	var out map[string][]string

	for name, values := range headers {
		if !slices.Contains(allowed, strings.ToLower(name)) {
			continue
		}

		if out == nil {
			out = map[string][]string{}
		}

		out[http.CanonicalHeaderKey(name)] = slices.Clone(values)
	}

	return out
}
