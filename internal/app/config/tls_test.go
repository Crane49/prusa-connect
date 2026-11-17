package config_test

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// TestAChainInTheWrongOrderSaysSo is the refusal that named the wrong cause.
// A PEM file written root-first is refused as a key that does not match its
// certificate, which is true of the certificate at the top of the file and is
// not the mistake the operator made.
func TestAChainInTheWrongOrderSaysSo(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	key := ecdsaKey(t)
	leaf := issuePair(t, dir, "leaf", key)
	root := issuePair(t, dir, "root", ecdsaKey(t))

	leafPEM, err := os.ReadFile(leaf.Certificate)
	require.NoError(t, err)

	rootPEM, err := os.ReadFile(root.Certificate)
	require.NoError(t, err)

	reversed := filepath.Join(dir, "reversed.crt")
	require.NoError(t, os.WriteFile(reversed, append(rootPEM, leafPEM...), 0o600))

	ordered := filepath.Join(dir, "ordered.crt")
	require.NoError(t, os.WriteFile(ordered, append(leafPEM, rootPEM...), 0o600))

	_, err = config.TLS{Certificate: ordered, Key: leaf.Key}.Load()
	require.NoError(t, err, "a chain with the leaf first is the one a TLS server presents")

	_, err = config.TLS{Certificate: reversed, Key: leaf.Key}.Load()
	require.ErrorIs(t, err, config.ErrInvalid)
	require.ErrorContains(t, err, "wrong order")
	require.ErrorContains(t, err, "2 of 2")
}

// ecdsaKey is the key behind every certificate these cases issue.
func ecdsaKey(t *testing.T) crypto.Signer {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	return key
}

// issuePair writes a self-signed certificate and its key, and returns the pair
// as an operator would name it.
func issuePair(t *testing.T, dir, name string, key crypto.Signer) config.TLS {
	t.Helper()

	return issueWindow(t, dir, name, key, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
}

// issueWindow is issuePair with the validity window written out, so a test can
// produce the certificate that has expired as well as the one that has not.
func issueWindow(t *testing.T, dir, name string, key crypto.Signer, from, to time.Time) config.TLS {
	t.Helper()

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "prusa-connect.local"},
		NotBefore:             from,
		NotAfter:              to,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"prusa-connect.local"},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	require.NoError(t, err)

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	certPath := filepath.Join(dir, name+".crt")
	keyPath := filepath.Join(dir, name+".key")

	require.NoError(t, os.WriteFile(certPath,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyPath,
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600))

	return config.TLS{Certificate: certPath, Key: keyPath}
}
