package app

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
)

// The demo serves HTTPS when it is given a certificate and a key, the files
// core.api.tls takes. Everything the demo runs against the core then trusts
// the certificates in that file, and nothing else: the simulated printers,
// the echo machine and the demo's own reads.

// errDemoTLSHalf refuses a certificate without its key, or a key without its
// certificate.
var errDemoTLSHalf = errors.New("give -tls-cert and -tls-key together, or neither")

// errDemoTLSNoCertificate refuses a certificate file that holds no
// certificate the demo's clients could trust.
var errDemoTLSNoCertificate = errors.New("the -tls-cert file holds no PEM certificate")

// servesTLS reports whether this run serves HTTPS.
func (o DemoOptions) servesTLS() bool { return o.TLSCert != "" }

// demoClientTLS is what the demo's clients trust: every certificate in the
// file the core presents, and no other root. It is nil for a plain run.
func demoClientTLS(opts DemoOptions) (*tls.Config, error) {
	if (opts.TLSCert == "") != (opts.TLSKey == "") {
		return nil, errDemoTLSHalf
	}

	if !opts.servesTLS() {
		return nil, nil //nolint:nilnil // a plain run trusts nothing of its own.
	}

	held, err := os.ReadFile(opts.TLSCert)
	if err != nil {
		return nil, fmt.Errorf("read the demo certificate: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(held) {
		return nil, fmt.Errorf("%w: %s", errDemoTLSNoCertificate, opts.TLSCert)
	}

	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13}, nil
}

// demoHTTPClient is the client the demo's own reads go through.
func demoHTTPClient(clientTLS *tls.Config) *http.Client {
	client := &http.Client{Timeout: demoRequest}
	if clientTLS != nil {
		client.Transport = &http.Transport{TLSClientConfig: clientTLS}
	}

	return client
}

// demoDialAddr is the API address as a client dials it over TLS. An address
// that binds every interface is dialled on loopback, which is also the name
// the client checks the certificate against.
func demoDialAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}

	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		return net.JoinHostPort("127.0.0.1", port)
	}

	return addr
}

// demoTLSConfig is the core.api.tls block of a run that serves HTTPS, and
// nothing for a plain run.
func demoTLSConfig(opts DemoOptions) string {
	if !opts.servesTLS() {
		return ""
	}

	return "\n    tls:\n      certificate: " + demoPath(opts.TLSCert) + "\n      key: " + demoPath(opts.TLSKey)
}
