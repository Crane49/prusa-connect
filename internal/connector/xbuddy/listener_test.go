package xbuddy_test

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// keypair is a certificate and its key, as an operator names the two in the
// listener block.
type keypair struct {
	certificate string
	key         string
}

// TestTheDeviceCertificateIsHeldToWhatTheFirmwareCanUse is the check that turns
// an afternoon at the bench into a startup message. xBuddy firmware offers one
// cipher suite and that suite needs an ECDSA key, so an RSA certificate is a
// listener no printer will ever complete a handshake with, and the printer
// reports it as a number.
func TestTheDeviceCertificateIsHeldToWhatTheFirmwareCanUse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ecdsaPair := issuePair(t, dir, "ecdsa", ecdsaKey(t))
	rsaPair := issuePair(t, dir, "rsa", rsaKey(t))

	cases := map[string]struct {
		block string
		names string
	}{
		"an ECDSA certificate is what the firmware can use": {
			block: listenerBlock(`"addr":"127.0.0.1:8443"`, tlsBlock(ecdsaPair)),
		},
		"an RSA certificate is refused before a port is bound": {
			block: listenerBlock(`"addr":"127.0.0.1:8443"`, tlsBlock(rsaPair)),
			names: "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
		},
		"a certificate file that is not there names the key and the path": {
			block: listenerBlock(`"addr":"127.0.0.1:8443"`, tlsBlock(keypair{
				certificate: filepath.Join(dir, "absent.crt"), key: ecdsaPair.key,
			})),
			names: "listener.tls.certificate",
		},
		"a certificate with no key cannot sign a handshake": {
			block: listenerBlock(`"addr":"127.0.0.1:8443"`,
				tlsBlock(keypair{certificate: ecdsaPair.certificate})),
			names: "listener.tls.key is empty",
		},
		"a key with no certificate has nothing to present": {
			block: listenerBlock(`"addr":"127.0.0.1:8443"`, tlsBlock(keypair{key: ecdsaPair.key})),
			names: "listener.tls.certificate is empty",
		},
		"a certificate configured for a listener that does not exist is refused": {
			block: listenerBlock(tlsBlock(ecdsaPair)),
			names: "listener.addr is empty",
		},
		"a device listener with no certificate serves plain HTTP": {
			block: listenerBlock(`"addr":"127.0.0.1:8443"`),
		},
		"an address with no port is refused": {
			block: listenerBlock(`"addr":"127.0.0.1"`),
			names: "expected host:port",
		},
		"a named port is refused, because it resolves through a file nobody read": {
			block: listenerBlock(`"addr":"127.0.0.1:https"`),
			names: "is not a port number",
		},
		"no device listener at all is the default and builds": {block: `{}`},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := xbuddy.New(connectorapi.Spec{
				Ref: "printer-1", Type: xbuddy.Type, Config: json.RawMessage(testCase.block),
			})

			if testCase.names == "" {
				if err != nil {
					t.Fatalf("the block %s was refused: %v", testCase.block, err)
				}

				return
			}

			if err == nil {
				t.Fatalf("%s was accepted, and every printer would fail the handshake", testCase.block)
			}

			if !strings.Contains(err.Error(), testCase.names) {
				t.Errorf("the refusal is %q and does not name %q", err, testCase.names)
			}

			t.Logf("observed refusal: %v", err)
		})
	}
}

// TestADeviceCertificateOutsideItsValidityWindowIsRefused is the second thing
// no printer will ever complete a handshake with. The firmware verifies the
// certificate it is presented, so a listener holding an expired one serves
// every printer a refusal while the process reports itself healthy, which is
// the worst failure this listener has: nothing looks wrong anywhere.
//
// A certificate that expires next week still works for every printer today, so
// that one is a startup warning and not a refusal.
func TestADeviceCertificateOutsideItsValidityWindowIsRefused(t *testing.T) {
	t.Parallel()

	now := time.Now()

	cases := map[string]struct {
		from  time.Time
		to    time.Time
		names string
	}{
		"valid now": {
			from: now.Add(-time.Hour), to: now.Add(24 * time.Hour),
		},
		"valid now and expiring soon, which is a warning and not a refusal": {
			from: now.Add(-time.Hour), to: now.Add(48 * time.Hour),
		},
		"expired yesterday": {
			from: now.Add(-48 * time.Hour), to: now.Add(-24 * time.Hour),
			names: "expired",
		},
		"not valid until tomorrow": {
			from: now.Add(24 * time.Hour), to: now.Add(48 * time.Hour),
			names: "not valid until",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pair := issueWindow(t, t.TempDir(), "printer", ecdsaKey(t), testCase.from, testCase.to)

			_, err := xbuddy.New(connectorapi.Spec{
				Ref: "printer-1", Type: xbuddy.Type,
				Config: json.RawMessage(listenerBlock(`"addr":"127.0.0.1:8443"`, tlsBlock(pair))),
			})

			if testCase.names == "" {
				if err != nil {
					t.Fatalf("a certificate valid today was refused: %v", err)
				}

				return
			}

			if err == nil {
				t.Fatal("a certificate outside its validity window was accepted")
			}

			for _, want := range []string{testCase.names, "listener.tls.certificate"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal is %q and does not name %q", err, want)
				}
			}
		})
	}
}

// TestTheHandshakeBudgetIsReadOnlyWhereThereIsAHandshake keeps a stray key from
// reading as the default. A budget on a connector that asked for no socket
// bounds nothing, and accepting it quietly is how an operator finds out later
// that the block they wrote was never read.
func TestTheHandshakeBudgetIsReadOnlyWhereThereIsAHandshake(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		block string
		names string
	}{
		"a budget beside an address is read": {
			block: listenerBlock(`"addr":"127.0.0.1:8443"`, `"handshake_timeout":"90s"`),
		},
		"a budget with no address is refused": {
			block: listenerBlock(`"handshake_timeout":"90s"`),
			names: "listener.handshake_timeout",
		},
		"a budget of zero is refused, because no handshake would complete": {
			block: listenerBlock(`"addr":"127.0.0.1:8443"`, `"handshake_timeout":"0s"`),
			names: "expected a positive duration",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := xbuddy.New(connectorapi.Spec{
				Ref: "printer-1", Type: xbuddy.Type, Config: json.RawMessage(testCase.block),
			})

			if testCase.names == "" {
				if err != nil {
					t.Fatalf("the block %s was refused: %v", testCase.block, err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), testCase.names) {
				t.Fatalf("the refusal is %v and does not name %q", err, testCase.names)
			}
		})
	}
}

// TestTheDeviceListenerIsTheOneTheFirmwareSpeaks is what the host binds. The
// profile is the dialect's, so a host that serves it offers exactly the one
// version and the one cipher suite the firmware in the field has.
func TestTheDeviceListenerIsTheOneTheFirmwareSpeaks(t *testing.T) {
	t.Parallel()

	pair := issuePair(t, t.TempDir(), "printer", ecdsaKey(t))

	driver, err := xbuddy.New(connectorapi.Spec{
		Ref: "printer-1", Type: xbuddy.Type,
		Config: json.RawMessage(listenerBlock(
			`"addr":"127.0.0.1:8443"`, `"handshake_timeout":"90s"`, tlsBlock(pair))),
	})
	if err != nil {
		t.Fatalf("a listener an operator can write was refused: %v", err)
	}

	asks, own := driver.(connectorapi.ServesItsOwnSocket)
	if !own {
		t.Fatal("the dialect asks for no socket, so a printer VLAN reaches the whole API")
	}

	listener := asks.DeviceListener()

	if listener.Addr != "127.0.0.1:8443" {
		t.Errorf("the socket is %q, and the file asked for 127.0.0.1:8443", listener.Addr)
	}

	if listener.HandshakeTimeout != 90*time.Second {
		t.Errorf("the handshake budget is %s, and the file asked for 90s", listener.HandshakeTimeout)
	}

	if listener.TLS == nil {
		t.Fatal("a configured certificate produced no TLS profile, so the socket serves it in the clear")
	}

	if listener.TLS.MinVersion != tls.VersionTLS12 || listener.TLS.MaxVersion != tls.VersionTLS12 {
		t.Errorf("the profile runs from %x to %x, and the firmware speaks TLS 1.2 and nothing else",
			listener.TLS.MinVersion, listener.TLS.MaxVersion)
	}

	if len(listener.TLS.CipherSuites) != 1 ||
		listener.TLS.CipherSuites[0] != tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256 {
		t.Errorf("the profile offers %v, and the firmware offers one suite", listener.TLS.CipherSuites)
	}
}

// TestARotatedDeviceCertificateReachesARunningListener is what the deployment
// needs. The keypair is mounted from a secret something outside the process
// rotates, so a certificate read once at start reached the process only through
// a restart, and a restart drops every printer that was connected to pick up a
// file the process can read itself.
//
// A replacement no printer could verify is the other half. The listener keeps
// the keypair it already serves, because a bad write where the certificate
// comes from must not take down a listener that was answering a second earlier.
func TestARotatedDeviceCertificateReachesARunningListener(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		replace func(t *testing.T, dir string)
		changes bool
	}{
		"a new certificate on the same path is presented to the next handshake": {
			replace: func(t *testing.T, dir string) {
				t.Helper()
				rotate(t, dir, "printer", ecdsaKey(t), time.Now().Add(-time.Hour), time.Now().Add(240*time.Hour))
			},
			changes: true,
		},
		"an RSA replacement leaves the certificate the firmware can use in place": {
			replace: func(t *testing.T, dir string) {
				t.Helper()
				rotate(t, dir, "printer", rsaKey(t), time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
			},
		},
		"an expired replacement leaves the certificate every printer verifies in place": {
			replace: func(t *testing.T, dir string) {
				t.Helper()
				rotate(t, dir, "printer", ecdsaKey(t), time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
			},
		},
		"a replacement that is not a certificate at all leaves the working one in place": {
			replace: func(t *testing.T, dir string) {
				t.Helper()
				write(t, filepath.Join(dir, "printer.crt"), []byte("not a certificate"))
				bump(t, filepath.Join(dir, "printer.crt"))
			},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// The clock is the test's own, because a handshake looks at the
			// files at most once a second and the wait for the second look is
			// otherwise a second of somebody's afternoon.
			synctest.Test(t, func(t *testing.T) {
				dir := t.TempDir()
				profile := servedProfile(t, issuePair(t, dir, "printer", ecdsaKey(t)))
				before := presented(t, profile)

				testCase.replace(t, dir)
				time.Sleep(2 * certificateLook)

				after := presented(t, profile)

				if testCase.changes && bytes.Equal(before, after) {
					t.Error("the rotated certificate did not reach the listener, so it needs a restart")
				}

				if !testCase.changes && !bytes.Equal(before, after) {
					t.Error("a replacement no printer could verify was taken, so the listener serves a refusal")
				}
			})
		})
	}
}

// TestACertificateThatLandsBeforeItsKeyIsTakenWhenTheKeyArrives covers the
// writer that is not the kubelet. A mounted secret is swapped whole, but
// anything else replacing the two files does them one at a time, and the
// certificate that arrives first does not match the key still on disk. The
// listener has to look again when the key lands rather than wait for another
// write to the certificate that will never come.
func TestACertificateThatLandsBeforeItsKeyIsTakenWhenTheKeyArrives(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		serving := issuePair(t, dir, "printer", ecdsaKey(t))
		profile := servedProfile(t, serving)
		before := presented(t, profile)

		replacement := issueWindow(t, t.TempDir(), "printer", ecdsaKey(t),
			time.Now().Add(-time.Hour), time.Now().Add(240*time.Hour))

		land(t, replacement.certificate, serving.certificate)
		time.Sleep(2 * certificateLook)

		if half := presented(t, profile); !bytes.Equal(before, half) {
			t.Error("a certificate whose key the listener does not hold was presented, so the handshake fails")
		}

		land(t, replacement.key, serving.key)
		time.Sleep(2 * certificateLook)

		if after := presented(t, profile); bytes.Equal(before, after) {
			t.Error("the key arrived and the listener still serves the certificate it replaced, so a half-written " +
				"rotation needs a restart")
		}
	})
}

// certificateLook is how often the listener looks at the files behind its
// keypair. It is written out here because the listener's own constant is not
// this package's to read.
const certificateLook = time.Second

// servedProfile is the TLS profile a driver serves printers on its own socket.
func servedProfile(t *testing.T, pair keypair) *tls.Config {
	t.Helper()

	driver, err := xbuddy.New(connectorapi.Spec{
		Ref: "printer-1", Type: xbuddy.Type,
		Config: json.RawMessage(listenerBlock(`"addr":"127.0.0.1:8443"`, tlsBlock(pair))),
	})
	if err != nil {
		t.Fatalf("a listener an operator can write was refused: %v", err)
	}

	return driver.(connectorapi.ServesItsOwnSocket).DeviceListener().TLS
}

// presented is the certificate the listener answers a ClientHello with now.
func presented(t *testing.T, profile *tls.Config) []byte {
	t.Helper()

	if profile == nil || profile.GetCertificate == nil {
		t.Fatal("the listener has no certificate to look up, so it cannot read a rotated one")
	}

	pair, err := profile.GetCertificate(&tls.ClientHelloInfo{
		ServerName:        "prusa-connect.local",
		CipherSuites:      []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		SupportedVersions: []uint16{tls.VersionTLS12},
	})
	if err != nil {
		t.Fatalf("the listener had no certificate to present: %v", err)
	}

	if pair == nil || len(pair.Certificate) == 0 {
		t.Fatal("the listener presented no certificate, so every printer fails the handshake")
	}

	return pair.Certificate[0]
}

// rotate writes a keypair over the paths the listener was given, the way the
// kubelet replaces a mounted secret: both files at once.
func rotate(t *testing.T, dir, name string, key crypto.Signer, from, to time.Time) {
	t.Helper()

	bump(t, issueWindow(t, dir, name, key, from, to).certificate)
}

// land copies one half of a replacement keypair over the file the listener
// reads, for a writer that replaces the two one at a time.
func land(t *testing.T, from, to string) {
	t.Helper()

	content, err := os.ReadFile(from)
	if err != nil {
		t.Fatalf("read %s: %v", from, err)
	}

	write(t, to, content)
	bump(t, to)
}

// bump moves a file's modification time a minute past the newest file beside
// it, so a write reads as a change whatever the filesystem's timestamp
// resolution is, and the write after it reads as another one.
func bump(t *testing.T, path string) {
	t.Helper()

	entries, readErr := os.ReadDir(filepath.Dir(path))
	if readErr != nil {
		t.Fatalf("read %s: %v", filepath.Dir(path), readErr)
	}

	var newest time.Time

	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}

	ahead := newest.Add(time.Minute)
	if err := os.Chtimes(path, ahead, ahead); err != nil {
		t.Fatalf("move %s's modification time: %v", path, err)
	}
}

// TestADialectThatAsksForNoSocketSaysSo is the shipping default: a deployment
// that declares no connector mounts this dialect with its own defaults, and the
// printers then dial the server's own listener.
func TestADialectThatAsksForNoSocketSaysSo(t *testing.T) {
	t.Parallel()

	driver, err := xbuddy.New(connectorapi.Spec{Ref: "printer-1", Type: xbuddy.Type})
	if err != nil {
		t.Fatalf("the shipping defaults were refused: %v", err)
	}

	asks, own := driver.(connectorapi.ServesItsOwnSocket)
	if !own {
		t.Fatal("the dialect no longer answers the host's question about a socket of its own")
	}

	if addr := asks.DeviceListener().Addr; addr != "" {
		t.Errorf("the shipping default binds %q, and it binds nothing", addr)
	}
}

// listenerBlock is a settings block carrying a listener made of the entries
// given, so a case reads as the keys it sets and nothing else.
func listenerBlock(entries ...string) string {
	return `{"listener":{` + strings.Join(entries, ",") + `}}`
}

// tlsBlock is the keypair as an operator writes it under the listener.
func tlsBlock(pair keypair) string {
	return fmt.Sprintf(`"tls":{"certificate":%q,"key":%q}`, pair.certificate, pair.key)
}

// ecdsaKey and rsaKey are the two key types an operator can plausibly hand this
// listener. Only one of them can complete the firmware's handshake.
func ecdsaKey(t *testing.T) crypto.Signer {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate an ECDSA key: %v", err)
	}

	return key
}

func rsaKey(t *testing.T) crypto.Signer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate an RSA key: %v", err)
	}

	return key
}

// issuePair writes a self-signed certificate and its key, and returns the pair
// as an operator would name it.
func issuePair(t *testing.T, dir, name string, key crypto.Signer) keypair {
	t.Helper()

	return issueWindow(t, dir, name, key, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
}

// issueWindow is issuePair with the validity window written out, so a test can
// produce the certificate that has expired as well as the one that has not.
func issueWindow(t *testing.T, dir, name string, key crypto.Signer, from, to time.Time) keypair {
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
	if err != nil {
		t.Fatalf("issue a certificate: %v", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("encode the key: %v", err)
	}

	certPath := filepath.Join(dir, name+".crt")
	keyPath := filepath.Join(dir, name+".key")

	write(t, certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	write(t, keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))

	return keypair{certificate: certPath, key: keyPath}
}

// write puts one file where a case named it.
func write(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
