package mjpeg

import (
	"crypto/tls"
	"net/http"
	"time"
)

const (
	// idleConnsPerCamera is two: one snapshot poll and one live stream.
	idleConnsPerCamera = 2
	// responseHeaderTimeout bounds how long a camera may take to send its
	// headers. It applies to the stream too.
	responseHeaderTimeout = 10 * time.Second
)

// newClient builds one camera's HTTP client. It sets no Client.Timeout, because
// that would also end a stream body. A snapshot uses a context deadline.
func newClient(cfg Config) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:          idleConnsPerCamera,
		MaxIdleConnsPerHost:   idleConnsPerCamera,
		IdleConnTimeout:       streamIdleTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		DisableCompression:    true,
	}

	if cfg.SkipVerify {
		//nolint:gosec // the operator asked for it: a camera on a LAN often has no verifiable name.
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &http.Client{
		Transport: transport,
		// Redirects are not followed. A redirect could point this server at a
		// host the operator did not name.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
