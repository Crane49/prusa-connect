package httpapi

import (
	"net/http"
	"strings"
)

// acceptsGzip reports whether the client named gzip in Accept-Encoding. RFC
// 9110 section 12.5.3 makes a weight of zero a refusal, so "gzip;q=0" is not
// an acceptance, and x-gzip is another name for gzip.
func acceptsGzip(r *http.Request) bool {
	for encoding := range strings.SplitSeq(strings.Join(r.Header.Values("Accept-Encoding"), ","), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(encoding), ";")
		name = strings.TrimSpace(name)

		if !strings.EqualFold(name, "gzip") && !strings.EqualFold(name, "x-gzip") {
			continue
		}

		return !weightIsZero(params)
	}

	return false
}

// weightIsZero reads the q parameter of one Accept-Encoding member. A weight
// is at most three decimals, so zero is "0" followed by an optional point and
// zeros.
func weightIsZero(params string) bool {
	for param := range strings.SplitSeq(params, ";") {
		key, value, _ := strings.Cut(strings.TrimSpace(param), "=")
		if !strings.EqualFold(strings.TrimSpace(key), "q") {
			continue
		}

		value = strings.TrimSpace(value)

		return strings.HasPrefix(value, "0") && strings.Trim(strings.TrimPrefix(value, "0"), ".0") == ""
	}

	return false
}
