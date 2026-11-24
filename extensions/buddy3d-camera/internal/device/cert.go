package device

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// The file names in the data directory. The camera reads the certificate
// from its SD card under the name self-signed.pem, so the page offers it for
// download under that name.
const (
	certFile = "self-signed.pem"
	keyFile  = "self-signed.key"
)

// Certificate is the pair the camera's server presents, and the PEM a person
// copies to the camera.
type Certificate struct {
	TLS tls.Certificate
	PEM []byte
}

// LoadOrMake reads the self-signed certificate in dir, and makes a new one
// when there is none or it does not name address.
//
// The camera trusts exactly this certificate, so it is made once and kept: a
// new one means a trip to the SD card.
func LoadOrMake(dir, address string) (Certificate, error) {
	certPath, keyPath := filepath.Join(dir, certFile), filepath.Join(dir, keyFile)

	if held, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		leaf, parsed := x509.ParseCertificate(held.Certificate[0])
		if parsed == nil && names(leaf, address) {
			pemBytes, readErr := os.ReadFile(certPath)
			if readErr == nil {
				return Certificate{TLS: held, PEM: pemBytes}, nil
			}
		}
	}

	certPEM, keyPEM, err := makePair(address)
	if err != nil {
		return Certificate{}, err
	}

	if err = os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return Certificate{}, fmt.Errorf("device: write the key: %w", err)
	}

	if err = os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return Certificate{}, fmt.Errorf("device: write the certificate: %w", err)
	}

	held, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return Certificate{}, fmt.Errorf("device: read the new pair: %w", err)
	}

	return Certificate{TLS: held, PEM: certPEM}, nil
}

// names reports whether the certificate was made for address.
func names(leaf *x509.Certificate, address string) bool {
	if ip := net.ParseIP(address); ip != nil {
		return slices.ContainsFunc(leaf.IPAddresses, ip.Equal)
	}

	return slices.Contains(leaf.DNSNames, address)
}

// makePair makes a self-signed RSA certificate for address.
//
// The address is the common name and a DNS name as well as an IP name: an
// older mbedTLS matches the host it dialled against those strings and ignores
// IP names. It is valid from 1970, because the camera's firmware may have no
// working clock when it first connects.
// The key's size, which the camera's mbedTLS takes, and the bits of a serial.
const (
	keyBits    = 2048
	serialBits = 62
)

func makePair(address string) (certPEM, keyPEM []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		return nil, nil, fmt.Errorf("device: make a key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), serialBits))
	if err != nil {
		return nil, nil, fmt.Errorf("device: make a serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: address, Organization: []string{"buddy3d-camera"}},
		DNSNames:              []string{address},
		NotBefore:             time.Date(1970, 1, 2, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2049, 12, 31, 0, 0, 0, 0, time.UTC),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	if ip := net.ParseIP(address); ip != nil {
		template.IPAddresses = []net.IP{ip}
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("device: sign the certificate: %w", err)
	}

	var certOut, keyOut bytes.Buffer

	_ = pem.Encode(&certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	_ = pem.Encode(&keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	return certOut.Bytes(), keyOut.Bytes(), nil
}
