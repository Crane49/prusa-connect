package routesapi

import (
	"net/http"
	"slices"
	"strings"
)

// The headers that pass through the host between a person and a module. Both
// lists are allow-lists.
//
// Into a module, the person's Cookie and Authorization do not pass, so a
// module cannot call the core as that person. PrincipalOf says who is calling.
//
// Out of a module, cookies, the security headers of the core's origin and
// Location do not pass.

// requestHeaders are the request headers that pass into a module, in lower
// case.
//
//nolint:gochecknoglobals // a closed table read on every request, never written.
var requestHeaders = []string{
	"accept", "accept-language",
	"content-language", "content-type",
	"if-match", "if-modified-since", "if-none-match", "if-range", "if-unmodified-since",
	"range", "user-agent",
}

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
	return passing(headers, requestHeaders)
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
