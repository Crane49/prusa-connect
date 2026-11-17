package app

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// TestAMadeAuthoritySignsBothCertificates holds ADR-20261015-ejz rule 36: the files a
// printer and a browser need, each in the form the printer's firmware takes.
func TestAMadeAuthoritySignsBothCertificates(t *testing.T) {
	t.Parallel()

	folder := filepath.Join(t.TempDir(), "certs")

	made, err := writeCertificates(folder, []string{"printserver", "printserver.local", "192.168.1.50"}, time.Now())
	require.NoError(t, err)

	authority, err := x509.ParseCertificate(made.PrinterAuthority)
	require.NoError(t, err, "connect.der is not one DER certificate")
	require.True(t, authority.IsCA)
	require.True(t, authority.MaxPathLenZero)

	roots := x509.NewCertPool()
	roots.AddCert(authority)

	for _, pair := range [][2]string{
		{made.WebCertificate, made.WebKey},
		{made.PrinterCertificate, made.PrinterKey},
	} {
		_, err = tls.LoadX509KeyPair(pair[0], pair[1])
		require.NoError(t, err)

		raw, readErr := os.ReadFile(pair[0])
		require.NoError(t, readErr)

		block, _ := pem.Decode(raw)
		leaf, parseErr := x509.ParseCertificate(block.Bytes)
		require.NoError(t, parseErr)

		_, isECDSA := leaf.PublicKey.(*ecdsa.PublicKey)
		require.True(t, isECDSA, "the printer takes ECDSA only")

		for _, name := range []string{"printserver.local", "192.168.1.50"} {
			_, verifyErr := leaf.Verify(x509.VerifyOptions{DNSName: name, Roots: roots})
			require.NoError(t, verifyErr, "%s is not valid for %s", pair[0], name)
		}

		require.Contains(t, leaf.DNSNames, "192.168.1.50",
			"an address needs a DNS twin, which is how the firmware matches it")

		if runtime.GOOS != "windows" {
			info, statErr := os.Stat(pair[1])
			require.NoError(t, statErr)
			require.Equal(t, os.FileMode(keyFileMode), info.Mode().Perm(), "a key is readable by others")
		}
	}

	der, err := os.ReadFile(filepath.Join(folder, printerCAFile))
	require.NoError(t, err)
	require.Equal(t, made.PrinterAuthority, der)
}

// TestThePrintersListenerIsCheckedBeforeTheRestart refuses what would stop the
// start: the API's own socket, half a pair, and an address with no port.
func TestThePrintersListenerIsCheckedBeforeTheRestart(t *testing.T) {
	t.Parallel()

	made, err := writeCertificates(t.TempDir(), []string{"printserver"}, time.Now())
	require.NoError(t, err)

	block, err := printersBlock(&httpapi.SetupPrinters{
		Addr: ":8443", Certificate: made.PrinterCertificate, Key: made.PrinterKey,
	}, ":8080")
	require.NoError(t, err)
	require.Contains(t, block, printerConnector())

	for name, printers := range map[string]httpapi.SetupPrinters{
		"the API's socket": {Addr: ":8080"},
		"no port":          {Addr: "printserver"},
		"half a pair":      {Addr: ":8443", Certificate: made.PrinterCertificate},
	} {
		_, err = printersBlock(&printers, ":8080")
		require.Error(t, err, name)
	}
}

// TestCertificateNamesAreHostsOrAddresses refuses a name no certificate can
// carry before anything is signed.
func TestCertificateNamesAreHostsOrAddresses(t *testing.T) {
	t.Parallel()

	names, err := certificateNames([]string{" PrintServer ", "printserver", "10.0.0.2"})
	require.NoError(t, err)
	require.Equal(t, []string{"printserver", "10.0.0.2"}, names)

	_, err = certificateNames([]string{"not a name"})
	require.Error(t, err)

	_, err = certificateNames(nil)
	require.Error(t, err)
}
