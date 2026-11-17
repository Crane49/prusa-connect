package natsbind

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

// The errors of the TLS helpers.
var (
	// ErrNoAnchors means a file in TLSMaterial.CAFiles has no certificate the
	// node can parse.
	ErrNoAnchors = errors.New("the trust anchors hold no certificate")
	// ErrNoCertificate means ServerTLS was given material with no certificate
	// or no key, so a listener has nothing to present.
	ErrNoCertificate = errors.New("the tls material names no certificate and key")
)

// ServerTLS builds the TLS configuration of a listener from m, as the node's
// own listeners use it: TLS 1.3, no client certificate, and the certificate
// read again at every handshake. The module serves its other listeners with
// it, so every listener of this server presents the same certificate.
func ServerTLS(m TLSMaterial) (*tls.Config, error) {
	if !m.Serves() {
		return nil, ErrNoCertificate
	}
	return serverTLS(&m), nil
}

// serverTLS builds the TLS configuration of a listener. It requires TLS 1.3
// and asks the client for no certificate. It reads the certificate files again
// at every handshake, so the listener serves a renewed certificate without a
// restart.
//
// The server authenticates a node that connects by its user JWT, a signed
// token, and by the node's signature over the server's nonce. A client
// certificate would add nothing to that.
func serverTLS(m *TLSMaterial) *tls.Config {
	if !m.Serves() {
		return nil
	}
	certFile, keyFile := m.CertFile, m.KeyFile
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		ClientAuth: tls.NoClientCert,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			pair, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				return nil, fmt.Errorf("read the certificate: %w", err)
			}
			return &pair, nil
		},
	}
}

// ClientTLS builds the TLS configuration of an outgoing connection. It
// requires TLS 1.3, verifies the server's certificate with the CA certificates
// in anchors, and sends no client certificate. With an empty serverName the
// NATS library uses the host of the address it connects to.
func ClientTLS(anchors *x509.CertPool, serverName string) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    anchors,
		ServerName: serverName,
	}
}

// LoadAnchors reads the CA certificates from the files in
// TLSMaterial.CAFiles. With no files it returns the CA certificates of the
// operating system.
func LoadAnchors(files []string) (*x509.CertPool, error) {
	if len(files) == 0 {
		pool, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("read the system trust anchors: %w", err)
		}
		return pool, nil
	}
	pool := x509.NewCertPool()
	for _, file := range files {
		pem, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%s: %w", file, ErrNoAnchors)
		}
	}
	return pool, nil
}
