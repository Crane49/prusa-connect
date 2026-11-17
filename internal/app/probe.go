package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// ProbePath is what the probe asks for. It is readiness and not liveness,
// because a caller may be sent to a container that reports healthy, and the
// readiness endpoint says whether that is safe. Liveness has a path of its
// own. A Kubernetes deployment reads both over HTTP without this code.
const ProbePath = "/api/v1/readiness"

// probeTimeout bounds the whole probe. It is short because a probe is run every
// few seconds by something that will call it hung if it does not answer.
const probeTimeout = 3 * time.Second

// probeHost is the host the probe dials. The listener's address says which
// interface it accepts on. It does not say how a caller on the same host
// reaches it, and a wildcard address names no host.
const probeHost = "127.0.0.1"

// Probe reports whether the core this configuration describes is ready to
// serve. It is what the container's health check runs, so the image needs no
// curl and no wget.
//
// It reads the same configuration file the server reads, so a moved address or
// a configured certificate needs no second place to update.
func Probe(ctx context.Context, options Options) error {
	core, err := config.Load(options.ConfigPath)
	if err != nil {
		return err
	}

	core = options.applyTo(core)

	target, client := probeTarget(core)

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("build the probe request for %s: %w", target, err)
	}

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("probe %s: %w", target, err)
	}

	defer func() { _ = response.Body.Close() }()

	// Read the body to the end so the connection can be reused, and discard it:
	// the status is the verdict and the body is for a person reading the log.
	_, _ = io.Copy(io.Discard, response.Body)

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", target, response.Status)
	}

	return nil
}

// probeTarget is the URL to ask and the client to ask it with.
//
// A configured certificate is not verified. The peer is this same process over
// the loopback interface, and no network lies between the two. The certificate
// is often one this deployment signed itself, so verifying it would fail the
// probe on those deployments.
func probeTarget(core config.Core) (string, *http.Client) {
	client := &http.Client{Timeout: probeTimeout}
	scheme := "http://"

	if core.API.TLS.Enabled() {
		scheme = "https://"
		client.Transport = &http.Transport{
			//nolint:gosec // a loopback probe of this process's own listener; see the comment above.
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13},
		}
	}

	return scheme + net.JoinHostPort(probeHost, probePort(core.API.Addr)) + ProbePath, client
}

// probePort is the port half of a listener address. An address that will not
// split is returned whole, so the probe fails with the address in the
// message.
func probePort(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}

	return port
}
