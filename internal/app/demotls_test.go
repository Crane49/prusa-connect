package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// demoCertificate writes a self-signed certificate for 127.0.0.1 and its key,
// as an operator hands them to -tls-cert and -tls-key.
func demoCertificate(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "prusa-connect-demo"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:     []string{"localhost"},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	require.NoError(t, err)

	encodedKey, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	dir := t.TempDir()
	certFile = filepath.Join(dir, "api.crt")
	keyFile = filepath.Join(dir, "api.key")

	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(
		t,
		os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey}), 0o600),
	)

	return certFile, keyFile
}

// With a certificate and a key the demo writes them into core.api.tls, which
// the core loads, and its clients dial https on loopback. Without them the
// file carries no tls block.
func TestTheDemoConfigurationCarriesTheAPICertificate(t *testing.T) {
	t.Parallel()

	certFile, keyFile := demoCertificate(t)
	dir := t.TempDir()

	opts := DemoOptions{APIAddr: "0.0.0.0:18443", TLSCert: certFile, TLSKey: keyFile}.withDefaults()

	merged, err := demoDocument([]byte("# nothing here\n"), demoConfig(opts, dir, nil, true))
	require.NoError(t, err)

	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, merged, 0o600))

	core, err := config.Load(path)
	require.NoError(t, err, string(merged))
	require.NoError(t, core.Validate(), string(merged))
	require.Equal(t, config.TLS{Certificate: filepath.ToSlash(certFile), Key: filepath.ToSlash(keyFile)}, core.API.TLS)
	require.Equal(t, "https://127.0.0.1:18443", opts.baseURL())

	plain := DemoOptions{APIAddr: "0.0.0.0:18080"}.withDefaults()
	require.NotContains(t, demoConfig(plain, dir, nil, true), "tls:")
	require.Equal(t, "http://0.0.0.0:18080", plain.baseURL(), "a plain run dials the address as it always did")
}

// The demo's clients trust the certificates in the file and nothing else, and
// a certificate given without its key is refused before anything starts.
func TestTheDemoClientsTrustOnlyTheGivenCertificate(t *testing.T) {
	t.Parallel()

	certFile, keyFile := demoCertificate(t)

	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)

	server := &http.Server{
		Handler: http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
		),
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13},
		ReadHeaderTimeout: time.Second,
	}

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = server.ServeTLS(listener, "", "") }()

	t.Cleanup(func() { _ = server.Close() })

	opts := DemoOptions{APIAddr: listener.Addr().String(), TLSCert: certFile, TLSKey: keyFile}

	trusted, err := demoClientTLS(opts)
	require.NoError(t, err)
	require.False(t, trusted.InsecureSkipVerify)

	get := func(client *http.Client) error {
		request, reqErr := http.NewRequestWithContext(t.Context(), http.MethodGet, opts.baseURL()+"/", nil)
		require.NoError(t, reqErr)

		response, doErr := client.Do(request)
		if doErr == nil {
			_ = response.Body.Close()
		}

		return doErr
	}

	require.NoError(t, get(demoHTTPClient(trusted)), "the demo client refused the certificate it was given")

	other, _ := demoCertificate(t)
	untrusting, err := demoClientTLS(DemoOptions{TLSCert: other, TLSKey: keyFile})
	require.NoError(t, err)
	require.Error(t, get(demoHTTPClient(untrusting)), "the demo client trusted a certificate it was not given")

	_, err = demoClientTLS(DemoOptions{TLSCert: certFile})
	require.ErrorIs(t, err, errDemoTLSHalf)

	_, err = demoClientTLS(DemoOptions{TLSCert: keyFile, TLSKey: keyFile})
	require.ErrorIs(t, err, errDemoTLSNoCertificate)
}

// The whole demo over HTTPS: the printers register and stay connected through
// the certificate, the demo signs in and reads over it, and the banner says
// the core serves HTTPS. Plain HTTP is not served on the port.
func TestTheDemoRunsItsFleetOverHTTPS(t *testing.T) {
	t.Parallel()

	certFile, keyFile := demoCertificate(t)
	addr := freeAddr(t)
	said := &demoSaid{}

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	opts := DemoOptions{
		APIAddr: addr, Flap: 0, Password: demoTestPassword, Out: said, Cameras: false,
		TLSCert: certFile, TLSKey: keyFile,
	}

	// The reader's trust is set before the demo goroutine reads opts: the
	// closure holds the variable, and a later write to it would race.
	trusted, err := demoClientTLS(opts)
	require.NoError(t, err)

	opts.clientTLS = trusted
	reader := &demoFleet{client: demoHTTPClient(trusted)}

	go func() { done <- Demo(ctx, opts) }()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done, "a demo the person stopped must stop cleanly")
	})

	require.Eventually(t, func() bool {
		token, signErr := reader.signIn(
			t.Context(),
			opts,
			demoLogin{username: demoUsername, password: demoTestPassword},
		)
		reader.token = token

		return signErr == nil
	}, demoSettle, demoPoll, "the demo account never signed in over HTTPS")

	require.Eventually(t, func() bool {
		return connectedIn(demoRead[demoListing](t, reader, opts.baseURL()+"/api/v1/devices")) >= demoFleetSize
	}, demoSettle, demoPoll, "the fleet never connected over HTTPS")

	require.Eventually(t, func() bool {
		return strings.Contains(said.text(), "The core serves HTTPS with the certificate "+certFile)
	}, demoSettle, demoPoll, "the banner never said the core serves HTTPS: %q", said.text())

	require.Contains(t, said.text(), "https://"+addr+"/api/v1/devices")

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/api/v1/health", nil)
	require.NoError(t, err)

	response, err := (&http.Client{Timeout: demoRequest}).Do(request)
	if err == nil {
		_ = response.Body.Close()
		require.Equal(t, http.StatusBadRequest, response.StatusCode, "the port served plain HTTP")
	}
}
