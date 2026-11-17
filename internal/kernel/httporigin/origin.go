// Package httporigin compares HTTP origins for credential scoping.
package httporigin

import (
	"net/url"
	"strings"
)

// Same reports whether two HTTP URLs have the same scheme, host and effective
// port.
func Same(left, right *url.URL) bool {
	return left != nil && right != nil &&
		strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(host(left), host(right))
}

func host(address *url.URL) string {
	port := address.Port()
	if port == "" {
		switch strings.ToLower(address.Scheme) {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}

	return strings.ToLower(address.Hostname()) + ":" + port
}
