package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// TestAClientThatConstrainsNothingGetsTheProfileTheFirmwareSpeaks is the
// measurement every other test in this package prevents itself from taking. A
// client pinned to TLS 1.2 and one suite proves that the listener will accept
// that profile; it cannot see the listener accepting anything else, and the
// listener accepting anything else is exactly what a pinned profile promises it
// does not do.
//
// Go ignores tls.Config.CipherSuites under TLS 1.3, so a printer listener with
// no ceiling negotiates 1.3 with a suite the firmware has never heard of, while
// the startup line and the documentation both name one 1.2 suite.
func TestAClientThatConstrainsNothingGetsTheProfileTheFirmwareSpeaks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pki := issue(t, dir)
	addr := freeAddr(t)

	start(t, dir, printerConfig(dir, addr, pki))

	var dialler net.Dialer

	conn, err := tls.DialWithDialer(&dialler, "tcp", addr, &tls.Config{
		RootCAs:    pki.pool,
		ServerName: printerName,
	})
	require.NoError(t, err)

	t.Cleanup(func() { _ = conn.Close() })

	state := conn.ConnectionState()

	require.Equal(t, uint16(tls.VersionTLS12), state.Version,
		"a client that offered everything negotiated %s, so the printer listener has no ceiling and "+
			"the profile it advertises is not the profile it serves", versionName(state.Version))
	require.Equal(t, firmwareSuite, state.CipherSuite,
		"a client that offered everything negotiated %s, not the one suite the firmware offers",
		tls.CipherSuiteName(state.CipherSuite))
}

// TestTheHandshakeBudgetIsTheOneThatWasConfigured measures the budget rather
// than reading it back off the struct that set it.
//
// Go takes the TLS handshake budget from the smallest positive value among
// ReadHeaderTimeout, ReadTimeout and WriteTimeout, so the budget an operator
// configured survives only while all three carry it. Leaving the API listener's
// ten seconds on any one of them, or reverting one to a second, changes what a
// printer is given and changes nothing a struct comparison would notice.
func TestTheHandshakeBudgetIsTheOneThatWasConfigured(t *testing.T) {
	t.Parallel()

	const budget = 2 * time.Second

	dir := t.TempDir()
	pki := issue(t, dir)
	addr := freeAddr(t)

	start(t, dir, printerConfigWithin(dir, addr, pki, budget))

	conn, err := net.DialTimeout("tcp", addr, settle)
	require.NoError(t, err)

	t.Cleanup(func() { _ = conn.Close() })

	// A printer that has opened the socket and not yet sent a ClientHello. The
	// listener holds it for the handshake budget and then gives up.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(4*budget)))

	began := time.Now()
	_, err = conn.Read(make([]byte, 1))
	waited := time.Since(began)

	require.Error(t, err, "the listener kept a connection that never began a handshake")
	require.Greaterf(t, waited, budget*4/5,
		"a printer was given %s to complete its handshake and listener.handshake_timeout is %s",
		waited, budget)
	require.Lessf(t, waited, budget*2,
		"a printer was given %s to complete its handshake and listener.handshake_timeout is %s",
		waited, budget)
}

// TestTheServerNamesWhichVerificationTheClientRefused is the bench's question:
// three things go wrong at a printer and an operator has to tell them apart.
//
// A client that fails to verify what it was presented sends a fatal alert
// naming why, and mbedTLS picks a different alert per reason. Measured against
// mbedtls 2.28.8 (alpine:3.16, ssl_client2, the 2.x branch xBuddy firmware
// runs): a chain it does not trust sends unknown_ca, a name that does not match
// sends bad_certificate, and an expired certificate sends certificate_expired.
// openssl s_client agrees on all three, which is what this pins, because it
// needs no container and no network.
func TestTheServerNamesWhichVerificationTheClientRefused(t *testing.T) {
	t.Parallel()

	binary, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("openssl is not on PATH, so the independent client cannot run")
	}

	dir := t.TempDir()
	pki := issue(t, dir)
	stranger := issue(t, t.TempDir())

	cases := map[string]struct {
		anchor string
		name   string
		says   string
	}{
		"a chain the printer's connect.der does not anchor": {
			anchor: stranger.ca, name: printerName,
			says: "unknown certificate authority",
		},
		"a name the certificate does not carry": {
			anchor: pki.ca, name: "not-the-name.local",
			says: "bad certificate",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			addr, refusals := refusingListener(t, pki)

			run(t, binary, "s_client", "-connect", addr,
				"-servername", tc.name, "-verify_hostname", tc.name,
				"-CAfile", tc.anchor, "-tls1_2", "-verify_return_error", "-verify", "2")

			select {
			case refusal := <-refusals:
				require.Containsf(t, refusal, tc.says,
					"the printer listener reported %q, which does not tell an operator which of the "+
						"three certificate failures happened", refusal)
			case <-time.After(settle):
				require.Fail(t, "the listener saw no failed handshake")
			}
		})
	}
}

// TestACertificateNearingExpiryIsReportedAtStartup is the other half of the
// expiry rule. One that has expired is refused by config.Core.Validate, because
// no printer can complete a handshake against it; one that expires soon serves
// every printer today and is a warning, because refusing it would take a
// working deployment down for a problem it does not yet have.
func TestACertificateNearingExpiryIsReportedAtStartup(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		lasts  time.Duration
		warned bool
	}{
		"a certificate with two months left, which is a quiet start": {
			lasts: 2 * config.ExpiryWarning,
		},
		"a certificate with three days left": {
			lasts: 3 * 24 * time.Hour, warned: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			pki := issueUntil(t, dir, []string{printerName}, nil, time.Now().Add(tc.lasts))

			said := &recorder{}

			require.NotNil(t, deviceProfile(t, slog.New(said), pki))

			if !tc.warned {
				require.Empty(t, said.warnings(), "a certificate with months left was reported as a problem")

				return
			}

			require.Len(t, said.warnings(), 1)
			require.Contains(t, said.warnings()[0], "expires")
			require.Contains(t, said.warnings()[0], "scripts/generate_certificates.sh")
		})
	}
}

// TestTheNameAPrinterAskedForIsReportedWhenItIsNotOnTheCertificate is the half
// of a verification failure the server sees before the printer refuses.
//
// The alert says a name did not match. It cannot say which name, because by the
// time it arrives the printer has stopped talking. The hostname a printer is
// configured with arrives in the ClientHello, so the listener reads it there and
// the log line names the string that has to appear as a DNS entry.
//
// The name here is DNS-shaped because Go's client drops an IP literal from SNI
// (RFC 6066) and cannot produce that hello. mbedtls 2.28.8 sends it: measured
// with ssl_client2 at hostname 192.168.1.50, which reached a Go server as
// ServerName="192.168.1.50". So an IP-configured printer lands here too, which
// is the case this line exists for.
func TestTheNameAPrinterAskedForIsReportedWhenItIsNotOnTheCertificate(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		asked  string
		warned bool
	}{
		"a name the certificate carries":  {asked: printerName},
		"a name the certificate does not": {asked: "printer.lan", warned: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			pki := issue(t, dir)
			said := &recorder{}

			addr := servePrinterProfile(t, slog.New(said), pki)

			var dialler net.Dialer

			conn, err := tls.DialWithDialer(&dialler, "tcp", addr, &tls.Config{
				RootCAs:            pki.pool,
				ServerName:         tc.asked,
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: true, // the client's own verdict is not what this measures
			})
			require.NoError(t, err)

			_ = conn.Close()

			if !tc.warned {
				require.Empty(t, said.warnings(), "a printer that asked for a name on the certificate")

				return
			}

			require.Len(t, said.warnings(), 1)
			require.Contains(t, said.warnings()[0], tc.asked)
			require.Contains(t, said.warnings()[0], printerName)
		})
	}
}

// servePrinterProfile serves the device listener's own TLS configuration on a
// loopback port and returns the address.
func servePrinterProfile(t *testing.T, logger *slog.Logger, pki keypair) string {
	t.Helper()

	certificate := deviceProfile(t, logger, pki)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			raw, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			go func() {
				conn := tls.Server(raw, certificate)
				_ = conn.HandshakeContext(t.Context())
				_ = conn.Close()
			}()
		}
	}()

	return listener.Addr().String()
}

// refusingListener serves the device listener's own TLS configuration and
// reports every handshake it did not complete. It is the composition root's
// open() reduced to the one step this measures: the certificate comes from the
// dialect, so the profile under test is the shipped one.
func refusingListener(t *testing.T, pki keypair) (string, <-chan string) {
	t.Helper()

	certificate := deviceProfile(t, discard(), pki)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = listener.Close() })

	refusals := make(chan string, 4)

	go func() {
		for {
			raw, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			go func() {
				conn := tls.Server(raw, certificate)
				if handshakeErr := conn.HandshakeContext(t.Context()); handshakeErr != nil {
					refusals <- handshakeErr.Error()
				}

				_ = conn.Close()
			}()
		}
	}()

	return listener.Addr().String(), refusals
}

// run drives one command and reports what it printed when it fails in a way the
// case did not expect. A refused handshake is the point here, so a non-zero
// exit is not a failure.
func run(t *testing.T, binary string, args ...string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), settle)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = strings.NewReader("")

	out, _ := cmd.CombinedOutput()
	t.Logf("%s said:\n%s", binary, out)
}

// deviceProfile is the TLS profile the xBuddy dialect asks this host to serve,
// built the way the composition root builds it: out of the connector's own
// settings block. A measurement of the printer-facing socket then measures what
// the dialect produced and never a second copy of it.
func deviceProfile(t *testing.T, logger *slog.Logger, pki keypair) *tls.Config {
	t.Helper()

	block := fmt.Sprintf(`{"listener":{"addr":"127.0.0.1:0","tls":{"certificate":%q,"key":%q}}}`,
		slashed(pki.certificate), slashed(pki.key))

	profile := deviceListenerIn(t, logger, block).TLS
	require.NotNil(t, profile, "a configured certificate produced no profile")

	return profile
}

// deviceListenerIn is the socket one settings block asks for.
func deviceListenerIn(t *testing.T, logger *slog.Logger, block string) connectorapi.DeviceListener {
	t.Helper()

	driver, err := xbuddy.New(connectorapi.Spec{
		Ref: "printer-1", Type: xbuddy.Type, Config: json.RawMessage(block), Logger: logger,
	})
	require.NoError(t, err)

	asks, own := driver.(connectorapi.ServesItsOwnSocket)
	require.True(t, own, "the shipping dialect asks the host for no socket of its own")

	return asks.DeviceListener()
}

// versionName renders a negotiated TLS version for a failure message.
func versionName(version uint16) string {
	switch version {
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return "an unrecognised version"
	}
}
