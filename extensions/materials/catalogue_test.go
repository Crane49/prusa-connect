package main

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// A mirror behind a certificate of the operator's own is still reached
// through the proxy the environment names.
func TestACatalogueCAKeepsTheProxy(t *testing.T) {
	t.Parallel()

	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, []byte(testCA), 0o600); err != nil {
		t.Fatal(err)
	}

	client, err := catalogueClient(ca)
	if err != nil {
		t.Fatal(err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatalf("the client with catalogue_ca set reads no proxy: %#v", client.Transport)
	}
}

// testCA is a self-signed certificate; only its parse is read here.
const testCA = `-----BEGIN CERTIFICATE-----
MIIBhzCCAS2gAwIBAgIUDkz4SU3FiHHXSoO24F572q5QYX4wCgYIKoZIzj0EAwIw
GTEXMBUGA1UEAwwObWF0ZXJpYWxzLXRlc3QwHhcNMjYxMTAyMTEyNDE5WhcNMzYx
MDMwMTEyNDE5WjAZMRcwFQYDVQQDDA5tYXRlcmlhbHMtdGVzdDBZMBMGByqGSM49
AgEGCCqGSM49AwEHA0IABAHiwJ1RICWzpSPxycY4V7JPp75G2+V6y2Bwv/wB3wTI
JZry8PLBraUWy7/b9hxlxc3EeXJvFi9BlSQv2GZZVVCjUzBRMB0GA1UdDgQWBBRM
z3QNPDoI35xSVZFLZ4qg6ijQcjAfBgNVHSMEGDAWgBRMz3QNPDoI35xSVZFLZ4qg
6ijQcjAPBgNVHRMBAf8EBTADAQH/MAoGCCqGSM49BAMCA0gAMEUCIQDdn6OFygky
NRNZp/BI6UfNw3R+OyeDWRYIIGUymyFkvAIgDsGaY1HWuYun2VgMcoycFU5fVRNa
wzJZPsZgWvumnSM=
-----END CERTIFICATE-----
`
