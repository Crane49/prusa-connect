package natsbind_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind"
)

// writeCertificate writes a self-signed certificate for hq.example and its
// key into dir, and returns the material that names them.
func writeCertificate(t *testing.T, dir string) natsbind.TLSMaterial {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("create a key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "hq.example"},
		DNSNames:              []string{"hq.example"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create the certificate: %v", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("encode the key: %v", err)
	}

	material := natsbind.TLSMaterial{
		CertFile: filepath.Join(dir, "hq.pem"),
		KeyFile:  filepath.Join(dir, "hq.key"),
		CAFiles:  []string{filepath.Join(dir, "hq.pem")},
	}

	for file, block := range map[string]*pem.Block{
		material.CertFile: {Type: "CERTIFICATE", Bytes: der},
		material.KeyFile:  {Type: "PRIVATE KEY", Bytes: keyDER},
	} {
		if err = os.WriteFile(file, pem.EncodeToMemory(block), 0o600); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}

	return material
}

// TestServerTLS: the exported listener configuration presents the
// certificate of the material over TLS 1.3, and refuses material without one.
func TestServerTLS(t *testing.T) {
	t.Parallel()

	if _, err := natsbind.ServerTLS(natsbind.TLSMaterial{}); !errors.Is(err, natsbind.ErrNoCertificate) {
		t.Errorf("ServerTLS of empty material returned %v, want ErrNoCertificate", err)
	}

	material := writeCertificate(t, t.TempDir())

	listener, err := natsbind.ServerTLS(material)
	if err != nil {
		t.Fatalf("ServerTLS: %v", err)
	}

	presented, err := listener.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("read the certificate at a handshake: %v", err)
	}

	type shape struct {
		MinVersion   uint16
		ClientAuth   tls.ClientAuthType
		Certificates int
	}

	got := shape{listener.MinVersion, listener.ClientAuth, len(presented.Certificate)}
	if want := (shape{tls.VersionTLS13, tls.NoClientCert, 1}); got != want {
		t.Errorf("the listener is %+v, want %+v", got, want)
	}
}

// TestClientTLSTrustsAnchors: a client built from the anchors of the material
// trusts the certificate the listener presents for its host.
func TestClientTLSTrustsAnchors(t *testing.T) {
	t.Parallel()

	material := writeCertificate(t, t.TempDir())

	anchors, err := natsbind.LoadAnchors(material.CAFiles)
	if err != nil {
		t.Fatalf("LoadAnchors: %v", err)
	}

	client := natsbind.ClientTLS(anchors, "hq.example")

	text, err := os.ReadFile(material.CertFile)
	if err != nil {
		t.Fatalf("read the certificate: %v", err)
	}

	block, _ := decodePEM(text)

	certificate, err := x509.ParseCertificate(block)
	if err != nil {
		t.Fatalf("parse the certificate: %v", err)
	}

	if _, err = certificate.Verify(x509.VerifyOptions{Roots: client.RootCAs, DNSName: client.ServerName}); err != nil {
		t.Errorf("the client does not trust the certificate of hq.example: %v", err)
	}

	if client.MinVersion != tls.VersionTLS13 {
		t.Errorf("the client's MinVersion is %x, want TLS 1.3", client.MinVersion)
	}
}

// decodePEM returns the bytes of the first PEM block.
func decodePEM(data []byte) ([]byte, bool) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, false
	}

	return block.Bytes, true
}
