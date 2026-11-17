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
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// firmwareSuite is the one cipher suite xBuddy firmware offers, quoted from the
// protocol contract at docs/src/content/docs/prusa-xbuddy-websocket-protocol.md.
const firmwareSuite uint16 = tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256

// printerName is the DNS name the fixture certificate is issued for, and what a
// printer would carry as hostname in prusa_printer_settings.ini.
const printerName = "prusa-connect.local"

// TestThePrinterListenerMeetsWhatTheFirmwareEnforces dials the printer listener
// with a client held to exactly what the firmware enforces and to nothing more:
// TLS 1.2 as both floor and ceiling, that one cipher suite, verification on
// against the CA the printer would carry on its USB stick.
//
// A client that negotiates freely proves nothing here. Go and Go agree on far
// more than mbedTLS 2.x will offer, so a handshake that succeeds because both
// ends walked up to TLS 1.3 says nothing about a printer that can only walk to
// 1.2.
func TestThePrinterListenerMeetsWhatTheFirmwareEnforces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pki := issue(t, dir)
	addr := freeAddr(t)

	start(t, dir, printerConfig(dir, addr, pki))

	state := handshake(t, addr, pki, printerName)

	require.Equal(t, uint16(tls.VersionTLS12), state.Version,
		"the printer listener did not negotiate TLS 1.2, which is the only version the firmware speaks")
	require.Equal(t, firmwareSuite, state.CipherSuite,
		"the printer listener negotiated %s, not the suite the firmware offers",
		tls.CipherSuiteName(state.CipherSuite))
	require.True(t, state.HandshakeComplete)

	leaf := state.PeerCertificates[0]

	require.IsType(t, &ecdsa.PublicKey{}, leaf.PublicKey,
		"the firmware needs an ECDSA certificate, and this listener presented %T", leaf.PublicKey)

	// Earlier research, frozen: mbedTLS 2.x compares the string handed to
	// mbedtls_ssl_set_hostname() against DNS SAN entries only, so a printer
	// configured by IP needs the address in both forms or the handshake fails
	// with 0x04. This asserts on the certificate that came off the wire.
	for _, ip := range leaf.IPAddresses {
		require.Containsf(t, leaf.DNSNames, ip.String(),
			"the presented certificate carries IP:%s with no DNS:%s beside it, so every "+
				"printer configured by that address fails the handshake with 0x04", ip, ip)
	}
}

// TestOpenSSLCompletesTheFirmwareHandshake is the second opinion. A Go client
// verifying a Go server shares a library, a default and an assumption with it;
// the printer runs a C stack that shares none of those. openssl s_client is the
// nearest independently implemented client available here, and -verify_hostname
// checks the name against DNS SAN entries only, which is the mbedTLS 2.x
// behaviour earlier research records.
func TestOpenSSLCompletesTheFirmwareHandshake(t *testing.T) {
	t.Parallel()

	binary, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("openssl is not on PATH, so the independent handshake check cannot run")
	}

	dir := t.TempDir()
	pki := issue(t, dir)
	addr := freeAddr(t)

	start(t, dir, printerConfig(dir, addr, pki))

	// A printer configured by IP hands mbedTLS the address as a hostname, so
	// the DNS name and the IP literal are both checked as names.
	for _, name := range []string{printerName, "127.0.0.1"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			transcript := sClient(t, binary, addr, pki, name)

			require.Contains(t, transcript, "Protocol  : TLSv1.2")
			require.Contains(t, transcript, "Cipher    : ECDHE-ECDSA-AES128-GCM-SHA256")
			require.Contains(t, transcript, "Verify return code: 0 (ok)")
		})
	}
}

// TestASimulatedPrinterRegistersOverTLS is the whole path: a printer that only
// speaks the firmware's dialect reaches POST /p/register, polls GET /p/register,
// upgrades on GET /p/ws and is telemetering, all of it over TLS.
func TestASimulatedPrinterRegistersOverTLS(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pki := issue(t, dir)
	addr := freeAddr(t)

	running := start(t, dir, printerConfig(dir, addr, pki))

	dialWithTLS := func(ctx context.Context, network, address string) (net.Conn, error) {
		return tlsDial(ctx, network, address, firmwareClient(t, pki, printerName))
	}

	runOver(t, running, simulator.Config{
		Model:   "COREONE",
		Serial:  "SIM-TLS",
		BaseURL: "http://" + addr,
		Dial:    dialWithTLS,
	})

	listed := printers(t, running)
	require.Len(t, listed, 1)
	require.True(t, listed[0].Connected)
	require.Equal(t, "SIM-TLS", listed[0].Serial)
}

// TestThePrinterListenerServesTheDeviceWiresAndNothingElse is why there are two
// listeners rather than one socket with a certificate on it. The printer
// listener faces whatever network the printers are on, and the API is not on
// it.
func TestThePrinterListenerServesTheDeviceWiresAndNothingElse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pki := issue(t, dir)
	addr := freeAddr(t)

	start(t, dir, printerConfig(dir, addr, pki))

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return tlsDial(ctx, network, address, firmwareClient(t, pki, printerName))
		},
	}}

	refused := []string{"/api/v1/health", "/api/v1/devices", "/openapi.json", "/docs", "/"}
	for _, path := range refused {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			status, kind := probe(t, client, "http://"+addr+path)

			require.Equalf(t, http.StatusNotFound, status,
				"the printer listener answered %s, which belongs to the API listener", path)
			require.Equal(t, "application/problem+json", kind)
		})
	}

	// The wire a printer dials is there, and answers as itself rather than as a
	// route that does not exist.
	status, _ := probe(t, client, "http://"+addr+"/p/register")
	require.NotEqual(t, http.StatusNotFound, status,
		"GET /p/register is a path the firmware has compiled in and the printer listener must serve it")
}

// TestPlainHTTPIsUnchangedBesideAPrinterListener is the compatibility promise:
// the API listener keeps answering plain HTTP on every path it answered before,
// including the device wires, whether or not a printer listener exists. The
// simulator, the emulator and a developer's curl all use that.
func TestPlainHTTPIsUnchangedBesideAPrinterListener(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pki := issue(t, dir)
	addr := freeAddr(t)

	running := start(t, dir, printerConfig(dir, addr, pki))

	require.Equal(t, http.StatusOK, running.get(t, "/api/v1/health").status)
	require.Equal(t, http.StatusOK, running.get(t, "/openapi.json").status)

	// A second listener does not close the first one to printers: a printer
	// dialling the API listener over plain HTTP is still admitted.
	runOver(t, running, simulator.Config{
		Model:   "MK4",
		Serial:  "SIM-PLAIN",
		BaseURL: "http://" + running.addr,
	})

	listed := printers(t, running)
	require.Len(t, listed, 1)
	require.Equal(t, "SIM-PLAIN", listed[0].Serial)
}

// TestTheAPIListenerServesTLSAtItsOwnFloor is the other half of the topology.
// The API listener answers a browser, so it is held to TLS 1.3 and refuses the
// version the printer listener exists to speak. One socket could not do both.
func TestTheAPIListenerServesTLSAtItsOwnFloor(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pki := issue(t, dir)
	addr := freeAddr(t)

	serveAPIOverTLS(t, dir, addr, pki)

	browser := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: pki.pool, ServerName: printerName, MinVersion: tls.VersionTLS13,
	}}}

	require.Equal(t, http.StatusOK, healthOver(t, browser, addr).StatusCode,
		"the API listener never answered over TLS on %s", addr)

	// The printer's dialect is refused here, which is the whole reason the
	// printer has a socket of its own.
	var dialler net.Dialer

	conn, err := tls.DialWithDialer(&dialler, "tcp", addr, firmwareClient(t, pki, printerName))
	require.Error(t, err, "the API listener accepted TLS 1.2, so its floor is the printer's floor")
	require.Nil(t, conn)
}

// TestTheAPIListenerOffersHTTP2 is why that listener names ALPN protocols.
//
// A browser holds six connections per origin over HTTP/1.1 and the event stream
// holds one of them for as long as its reader is there, so a sixth of a
// browser's budget goes to a connection carrying no requests. net/http
// configures its HTTP/2 half from what the listener negotiated, and a
// tls.Config offering no protocols negotiates none.
func TestTheAPIListenerOffersHTTP2(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pki := issue(t, dir)
	addr := freeAddr(t)

	serveAPIOverTLS(t, dir, addr, pki)

	browser := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    pki.pool,
			ServerName: printerName,
			MinVersion: tls.VersionTLS13,
			NextProtos: []string{"h2", "http/1.1"},
		},
		ForceAttemptHTTP2: true,
	}}

	response := healthOver(t, browser, addr)

	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "h2", response.TLS.NegotiatedProtocol,
		"a client offering h2 negotiated %q, so the API listener advertises no protocol",
		response.TLS.NegotiatedProtocol)
	require.Equal(t, "HTTP/2.0", response.Proto)
}

// TestBothListenersGoDownTogether is the second socket's half of the shutdown
// contract. A drain that stops the API listener and leaves the printer one
// accepting is a process that has stopped answering anybody and is still
// holding a port.
func TestBothListenersGoDownTogether(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pki := issue(t, dir)
	addr := freeAddr(t)

	running := start(t, dir, printerConfig(dir, addr, pki))

	require.Equal(t, http.StatusOK, running.get(t, "/api/v1/health").status)

	printer := connect(t, addr, pki, printerName)
	require.True(t, printer.ConnectionState().HandshakeComplete)

	// Hung up before the stop. A connection that has completed its handshake
	// and sent nothing is one the drain waits its whole budget for, and that
	// wait is the client's and not the listener's.
	require.NoError(t, printer.Close())

	running.stopAndWait(t)

	_, err := net.DialTimeout("tcp", addr, time.Second)
	require.Error(t, err, "the printer listener must be closed once the core has stopped")
}

// TestTheListenerProfilesAreTheOnesTheirClientsSpeak reads the two
// configurations side by side. It is the assertion a handshake cannot make:
// a client only proves what it happened to negotiate, and the floor a listener
// refuses below is invisible to a client that never asked for it.
func TestTheListenerProfilesAreTheOnesTheirClientsSpeak(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pki := issue(t, dir)
	certificate := config.TLS{Certificate: pki.certificate, Key: pki.key}

	printer := deviceProfile(t, discard(), pki)
	require.Equal(t, uint16(tls.VersionTLS12), printer.MinVersion,
		"the firmware speaks TLS 1.2 and nothing above it")
	require.Equal(t, uint16(tls.VersionTLS12), printer.MaxVersion,
		"without a ceiling Go ignores CipherSuites, so the suite below is advertised and not served")
	require.Equal(t, []uint16{firmwareSuite}, printer.CipherSuites,
		"the printer listener offers a suite the firmware does not")

	api, err := apiTLS(t.Context(), discard(), certificate)
	require.NoError(t, err)
	require.Equal(t, uint16(tls.VersionTLS13), api.MinVersion,
		"a browser reaches the API listener, and nothing there has to negotiate down to a printer")

	// No certificate is plain HTTP, not a failure. This is what every
	// deployment that has configured nothing runs on.
	require.Nil(t, deviceListenerIn(t, discard(), `{"listener":{"addr":"127.0.0.1:0"}}`).TLS)
}

// TestAnAddressWithNoNameBesideItIsReportedAtStartup is earlier research,
// which is frozen: mbedTLS 2.x matches the hostname a printer was configured
// with against DNS entries only, so a certificate carrying IP:x with no DNS:x
// fails every IP-configured printer at once, with a number and no name.
//
// The certificate still loads and the listener still serves, because a
// deployment whose printers are configured by DNS name needs no IP entry. What
// it must not do is start in silence.
func TestAnAddressWithNoNameBesideItIsReportedAtStartup(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		names  []string
		warned bool
	}{
		"both forms, which is what the generator emits": {
			names: []string{printerName, "127.0.0.1"},
		},
		"the IP alone, which no IP-configured printer can verify": {
			names:  []string{printerName},
			warned: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			pki := issueNamed(t, dir, tc.names, []net.IP{net.ParseIP("127.0.0.1")})

			said := &recorder{}

			require.NotNil(t, deviceProfile(t, slog.New(said), pki))

			if !tc.warned {
				require.Empty(t, said.warnings(), "a usable certificate was reported as a problem")

				return
			}

			require.Len(t, said.warnings(), 1)
			require.Contains(t, said.warnings()[0], "DNS:127.0.0.1")
			require.Contains(t, said.warnings()[0], "0x04")
		})
	}
}

// recorder keeps the warnings a logger was given, and nothing else.
type recorder struct {
	slog.Handler

	mu   sync.Mutex
	said []string
}

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *recorder) Handle(_ context.Context, record slog.Record) error {
	if record.Level < slog.LevelWarn {
		return nil
	}

	line := record.Message

	record.Attrs(func(attr slog.Attr) bool {
		line += " " + attr.Key + "=" + attr.Value.String()

		return true
	})

	r.mu.Lock()
	defer r.mu.Unlock()

	r.said = append(r.said, line)

	return nil
}

func (r *recorder) warnings() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.said)
}

// keypair is a certificate, its key and the CA that signed it, as file paths.
type keypair struct {
	ca          string
	certificate string
	key         string
	pool        *x509.CertPool
}

// printerConfig is a core with a printer listener on addr, holding the
// certificate a printer would verify against the CA on its USB stick.
func printerConfig(dir, addr string, pki keypair) string {
	return printerConfigWithin(dir, addr, pki, 0)
}

// printerConfigWithin is printerConfig with the handshake budget written out,
// so a test can measure what a printer is actually given rather than what the
// key says.
func printerConfigWithin(dir, addr string, pki keypair, handshake time.Duration) string {
	block := []string{
		"core:",
		"  log:",
		"    level: error",
		"  connectors:",
		"    xbuddy:",
		"      type: xbuddy",
		"      config:",
		"        listener:",
		`          addr: "` + addr + `"`,
	}

	if handshake > 0 {
		block = append(block, "          handshake_timeout: "+handshake.String())
	}

	return lines(append(block,
		"          tls:",
		"            certificate: "+slashed(pki.certificate),
		"            key: "+slashed(pki.key),
		"resources:",
		"  storage:",
		"    local:",
		"      type: filesystem",
		"      enabled: true",
		"      config:",
		"        root: "+slashed(filepath.Join(dir, "storage")))...)
}

// serveAPIOverTLS starts a core whose API listener carries pki on addr, and
// stops it when the test ends.
func serveAPIOverTLS(t *testing.T, dir, addr string, pki keypair) {
	t.Helper()

	write(t, filepath.Join(dir, "config.yaml"), lines(
		"core:",
		"  log:",
		"    level: error",
		"  api:",
		`    addr: "`+addr+`"`,
		"    tls:",
		"      certificate: "+slashed(pki.certificate),
		"      key: "+slashed(pki.key)))

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() {
		done <- Run(ctx, Options{ConfigPath: filepath.Join(dir, "config.yaml"), Version: "test", LoopbackOnly: true})
	}()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
	})
}

// healthOver waits for the API listener to answer and returns the response, so
// a caller can read what the connection under it negotiated.
func healthOver(t *testing.T, client *http.Client, addr string) *http.Response {
	t.Helper()

	var answered *http.Response

	require.Eventually(t, func() bool {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
			"https://"+addr+"/api/v1/health", nil)
		require.NoError(t, err)

		response, err := client.Do(request)
		if err != nil {
			return false
		}

		_ = response.Body.Close()
		answered = response

		return true
	}, settle, 20*time.Millisecond, "the API listener never answered on %s", addr)

	return answered
}

// firmwareClient is a TLS client held to exactly what the firmware enforces and
// to nothing more. Every field here is a constraint the protocol contract names,
// and relaxing any one of them is what makes a Go-to-Go handshake prove nothing.
func firmwareClient(t *testing.T, pki keypair, name string) *tls.Config {
	t.Helper()

	return &tls.Config{
		RootCAs:      pki.pool,
		ServerName:   name,
		MinVersion:   tls.VersionTLS12,
		MaxVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{firmwareSuite},
	}
}

// handshake completes one connection and returns what was negotiated.
func handshake(t *testing.T, addr string, pki keypair, name string) tls.ConnectionState {
	t.Helper()

	return connect(t, addr, pki, name).ConnectionState()
}

// connect is handshake with the connection kept, for a case that has to hang
// up before the process stops.
func connect(t *testing.T, addr string, pki keypair, name string) *tls.Conn {
	t.Helper()

	var dialler net.Dialer

	conn, err := tls.DialWithDialer(&dialler, "tcp", addr, firmwareClient(t, pki, name))
	require.NoError(t, err, "a client constrained to what the firmware enforces could not connect")

	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

// tlsDial opens a TCP connection and completes the TLS handshake on it, so a
// caller that speaks plain HTTP over the returned connection is speaking HTTPS.
// It is how the simulator reaches a TLS listener without knowing it did.
func tlsDial(ctx context.Context, network, address string, cfg *tls.Config) (net.Conn, error) {
	var dialler tls.Dialer

	dialler.Config = cfg

	return dialler.DialContext(ctx, network, address)
}

// probe reads a status and a content type, and nothing else.
func probe(t *testing.T, client *http.Client, url string) (int, string) {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)

	response, err := client.Do(request)
	require.NoError(t, err)

	defer func() { _ = response.Body.Close() }()

	return response.StatusCode, response.Header.Get("Content-Type")
}

// sClient drives openssl s_client with the firmware's constraints and returns
// what it printed. -verify_hostname is the mbedTLS 2.x check: it compares the
// name against DNS SAN entries and not against IP ones.
func sClient(t *testing.T, binary, addr string, pki keypair, name string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "s_client",
		"-connect", addr,
		"-servername", name,
		"-verify_hostname", name,
		"-CAfile", pki.ca,
		"-tls1_2",
		"-cipher", "ECDHE-ECDSA-AES128-GCM-SHA256",
		"-verify_return_error",
		"-verify", "2")
	cmd.Stdin = strings.NewReader("")

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "openssl s_client refused the handshake:\n%s", out)

	return string(out)
}

// runOver starts a simulated printer against an already-running core and waits
// until the core lists it as connected. It is runSimulator without the base URL
// override, because the point here is which listener the printer dialled.
func runOver(t *testing.T, running *process, cfg simulator.Config) {
	t.Helper()

	cfg.ReconnectDelay = 50 * time.Millisecond

	printer, err := simulator.New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() { done <- printer.RegisterAndRun(ctx) }()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	require.Eventually(t, func() bool {
		listed := printers(t, running)

		return len(listed) == 1 && listed[0].Connected && telemetered(listed[0].State)
	}, settle, 25*time.Millisecond, "the simulated printer never appeared in the listing")
}

// issue writes a private CA and one ECDSA leaf signed by it, shaped the way
// scripts/generate_certificates.sh shapes the printer certificate: an ECDSA key,
// serverAuth, and every IP carried as both an IP and a DNS SAN.
func issue(t *testing.T, dir string) keypair {
	t.Helper()

	return issueNamed(t, dir, []string{printerName, "127.0.0.1"}, []net.IP{net.ParseIP("127.0.0.1")})
}

// issueNamed is issue with the subject alternative names written out, so a test
// can produce the certificate that fails on a printer as well as the one that
// works.
//
// The leaf outlives config.ExpiryWarning, because a certificate inside that
// window is one the listener is meant to say something about.
func issueNamed(t *testing.T, dir string, names []string, ips []net.IP) keypair {
	t.Helper()

	return issueUntil(t, dir, names, ips, time.Now().Add(2*config.ExpiryWarning))
}

// issueUntil is issueNamed with the leaf's NotAfter written out.
func issueUntil(t *testing.T, dir string, names []string, ips []net.IP, notAfter time.Time) keypair {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Prusa Connect Test Root CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	leafTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: printerName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              names,
		IPAddresses:           ips,
		BasicConstraintsValid: true,
	}

	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCert, &leafKey.PublicKey, caKey)
	require.NoError(t, err)

	leafKeyDER, err := x509.MarshalECPrivateKey(leafKey)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	return keypair{
		ca:          writePEM(t, filepath.Join(dir, "ca.crt"), "CERTIFICATE", caDER),
		certificate: writePEM(t, filepath.Join(dir, "printer.crt"), "CERTIFICATE", leafDER),
		key:         writePEM(t, filepath.Join(dir, "printer.key"), "EC PRIVATE KEY", leafKeyDER),
		pool:        pool,
	}
}

// writePEM writes one PEM block and returns the path it went to.
func writePEM(t *testing.T, path, kind string, der []byte) string {
	t.Helper()

	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600))

	return path
}
