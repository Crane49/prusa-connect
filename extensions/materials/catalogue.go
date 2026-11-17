package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
)

// catalogueClient is the client the catalogue is downloaded with. caFile, the
// catalogue_ca setting, names a PEM file trusted beside the system roots, for
// a mirror behind a certificate of the operator's own; empty trusts the system
// roots alone. The address stays https whatever this trusts: importer.Download
// refuses anything else.
func catalogueClient(caFile string) (*http.Client, error) {
	client := &http.Client{Timeout: downloadTimeout}
	if caFile == "" {
		return client, nil
	}

	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read catalogue_ca: %w", err)
	}

	roots, _ := x509.SystemCertPool()
	if roots == nil {
		roots = x509.NewCertPool()
	}

	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("catalogue_ca %s holds no PEM certificate", caFile)
	}

	// A clone keeps the proxy from the environment and the default timeouts.
	transport, _ := http.DefaultTransport.(*http.Transport)
	transport = transport.Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	client.Transport = transport

	return client, nil
}
