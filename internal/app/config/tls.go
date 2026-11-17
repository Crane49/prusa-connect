package config

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"
)

// ExpiryWarning is how long before a certificate's NotAfter a listener starts
// warning. A leaf from scripts/generate_certificates.sh is good for 825 days.
// Replacing a leaf signed by the same CA needs no visit to any printer, so a
// month of notice is enough.
const ExpiryWarning = 30 * 24 * time.Hour

// TLS is the certificate a listener presents and the key that signs the
// handshake with it. Both empty is a listener that serves plain HTTP.
//
// There is no minimum-version key. Each listener's floor follows from who
// dials it: a browser reaches the API listener and a printer reaches the
// printer listener, and each has one correct value.
type TLS struct {
	// Certificate is a PEM file holding the leaf and every certificate between
	// it and the trust anchor. mbedTLS validates the whole presented chain, so
	// a chain missing a link fails on the printer and not here.
	Certificate string `yaml:"certificate"`
	// Key is the PEM private key for that leaf.
	Key string `yaml:"key"`
}

// Enabled reports whether this listener was given a certificate.
func (t TLS) Enabled() bool { return t.Certificate != "" || t.Key != "" }

// Load reads the keypair. It is called once during validation, so a certificate
// that will not parse refuses the process before a port is bound, and once
// again when the listener is built.
func (t TLS) Load() (tls.Certificate, error) {
	pair, err := tls.LoadX509KeyPair(t.Certificate, t.Key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("%w: read the certificate at %s and the key at %s: %w%s",
			ErrInvalid, t.Certificate, t.Key, err, t.chainOrder())
	}

	return pair, nil
}

// chainOrder explains the one load failure whose message points at the wrong
// cause, and is empty for every other. tls.LoadX509KeyPair takes the first
// certificate in the file as the leaf, so a chain written root-first is
// refused as a key that does not match its certificate. The real mistake is
// the order of the chain.
func (t TLS) chainOrder() string {
	chain, err := certificatesIn(t.Certificate)
	if err != nil || len(chain) < 2 {
		return ""
	}

	signer, err := signerIn(t.Key)
	if err != nil {
		return ""
	}

	matcher, ok := signer.Public().(interface{ Equal(crypto.PublicKey) bool })
	if !ok {
		return ""
	}

	for position, certificate := range chain {
		if position == 0 || !matcher.Equal(certificate.PublicKey) {
			continue
		}

		return fmt.Sprintf(". Certificate %d of %d in that file is the one this key signs for, so the "+
			"chain is in the wrong order: the leaf comes first and every certificate between it and "+
			"the trust anchor follows", position+1, len(chain))
	}

	return ""
}

// certificatesIn reads every certificate a PEM file holds, in file order.
func certificatesIn(path string) ([]*x509.Certificate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var chain []*x509.Certificate

	for rest := raw; len(rest) > 0; {
		var block *pem.Block

		if block, rest = pem.Decode(rest); block == nil {
			break
		}

		if block.Type != "CERTIFICATE" {
			continue
		}

		certificate, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil {
			return nil, fmt.Errorf("parse a certificate in %s: %w", path, parseErr)
		}

		chain = append(chain, certificate)
	}

	return chain, nil
}

// signerIn reads a PEM private key in whichever of the three encodings it was
// written in.
func signerIn(path string) (crypto.Signer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("%w: %s holds no PEM block", ErrInvalid, path)
	}

	if key, pkcs8 := x509.ParsePKCS8PrivateKey(block.Bytes); pkcs8 == nil {
		signer, ok := key.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("%w: the key in %s cannot sign", ErrInvalid, path)
		}

		return signer, nil
	}

	if key, ec := x509.ParseECPrivateKey(block.Bytes); ec == nil {
		return key, nil
	}

	key, pkcs1 := x509.ParsePKCS1PrivateKey(block.Bytes)
	if pkcs1 != nil {
		return nil, fmt.Errorf("%w: the key in %s is in no encoding this reads: %w", ErrInvalid, path, pkcs1)
	}

	return key, nil
}

// Leaf is the certificate a client sees, parsed. It is parsed here because not
// every release of the standard library fills tls.Certificate.Leaf.
func Leaf(pair tls.Certificate) (*x509.Certificate, error) {
	if len(pair.Certificate) == 0 {
		return nil, fmt.Errorf("%w: the certificate file holds no certificate", ErrInvalid)
	}

	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("%w: parse the leaf certificate: %w", ErrInvalid, err)
	}

	return leaf, nil
}

// validate holds one listener's certificate to what can be checked before a
// socket is opened: that both halves are named, that both files are there, and
// that the pair loads.
func (t TLS) validate(key string) error {
	_, err := t.named(key)

	return err
}

// named reports the first thing wrong with the pair and, when nothing is,
// returns it loaded, so a caller that needs the certificate does not read the
// files a second time.
func (t TLS) named(key string) (tls.Certificate, error) {
	if !t.Enabled() {
		return tls.Certificate{}, nil
	}

	if err := t.present(key); err != nil {
		return tls.Certificate{}, err
	}

	return t.Load()
}

// present reports the first of the two paths that is missing or unreadable.
func (t TLS) present(key string) error {
	if t.Certificate == "" {
		return fmt.Errorf("%w: %s.key is set and %s.certificate is empty, so there is nothing to present",
			ErrInvalid, key, key)
	}

	if t.Key == "" {
		return fmt.Errorf("%w: %s.certificate is set and %s.key is empty, so nothing can sign the handshake",
			ErrInvalid, key, key)
	}

	for name, path := range map[string]string{key + ".certificate": t.Certificate, key + ".key": t.Key} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("%w: %s is %q: %w", ErrInvalid, name, path, err)
		}
	}

	return nil
}
