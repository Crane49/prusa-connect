package httpapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestAHandshakeFailureReachesTheConfiguredLogger is what an operator at a
// bench has to be able to read.
//
// http.Server writes a failed TLS handshake through Server.ErrorLog, and a nil
// ErrorLog is log.Printf onto raw stderr: it ignores core.log.level, ignores
// core.log.format so a json deployment gets one unparseable line, and names no
// listener, which with two sockets is the first thing an operator needs.
func TestAHandshakeFailureReachesTheConfiguredLogger(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		build func(*Server) *http.Server
		names string
	}{
		"the device-facing listener": {
			build: func(s *Server) *http.Server { return s.DeviceServer("127.0.0.1:0", 5*time.Second) },
			names: "printer",
		},
		"the api listener": {
			build: func(s *Server) *http.Server { return s.HTTPServer("127.0.0.1:0") },
			names: "api",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			said := &lines{}
			server := tc.build(serverLogging(t, slog.New(said)))

			from := refuseOneHandshake(t, server)

			require.Eventually(t, func() bool { return len(said.at(slog.LevelWarn)) > 0 }, 5*time.Second,
				10*time.Millisecond, "a failed handshake reached no logger this process configured")

			reported := said.at(slog.LevelWarn)[0]

			require.Contains(t, reported, "TLS handshake error")
			require.Containsf(t, reported, "listener="+tc.names,
				"the line names no listener, and this process binds two: %s", reported)
			require.Containsf(t, reported, from,
				"the line names no remote address, so nothing says which printer it was: %s", reported)
		})
	}
}

// TestTheHandshakeBudgetIsOnEveryTimeoutGoTakesItFrom pins the mechanism the
// doc comment on DeviceServer describes. Go computes the TLS handshake budget
// as the smallest positive value among these three, so a budget written on one
// of them is a budget the other two silently cut.
func TestTheHandshakeBudgetIsOnEveryTimeoutGoTakesItFrom(t *testing.T) {
	t.Parallel()

	const budget = 37 * time.Second

	server := newTestServer(t).DeviceServer("127.0.0.1:0", budget)

	require.Equal(t, budget, server.ReadHeaderTimeout)
	require.Equal(t, budget, server.ReadTimeout)
	require.Equal(t, budget, server.WriteTimeout)
}

// refuseOneHandshake serves the given server over TLS, dials it with a client
// that trusts nothing, and returns the address that client dialled from.
func refuseOneHandshake(t *testing.T, server *http.Server) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	// The composition root wraps the listener rather than handing the server a
	// keypair, so this is the shape a handshake fails in.
	guarded := tls.NewListener(listener, &tls.Config{
		Certificates: []tls.Certificate{selfSigned(t)},
		MinVersion:   tls.VersionTLS12,
	})

	go func() { _ = server.Serve(guarded) }()

	t.Cleanup(func() { _ = server.Close() })

	var dialler net.Dialer

	// Dialled in two steps rather than through tls.Dial, so the address the
	// server is expected to name is the one this connection actually used.
	raw, err := dialler.DialContext(t.Context(), "tcp", listener.Addr().String())
	require.NoError(t, err)

	from := raw.LocalAddr().String()

	client := tls.Client(raw, &tls.Config{
		RootCAs:    x509.NewCertPool(),
		ServerName: "prusa-connect.local",
		MinVersion: tls.VersionTLS12,
	})
	require.Error(t, client.HandshakeContext(t.Context()),
		"a client trusting nothing completed the handshake")

	_ = client.Close()

	return from
}

// selfSigned is one ECDSA certificate a client that trusts nothing will refuse.
func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "prusa-connect.local"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"prusa-connect.local"},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// serverLogging is a production server whose logger is the caller's.
func serverLogging(t *testing.T, logger *slog.Logger) *Server {
	t.Helper()

	server, err := New(Config{
		Version:    "test",
		Logger:     logger,
		Principals: fixedPrincipal("operator"),
		Resources:  seededStore(),
		Presets:    seededPresets(),
		Endpoints:  seededEndpoints(),
		Grants:     &fakeGrantStore{},
		Sessions:   newFakeSessions(),
		Accounts:   &fakeAccounts{},
		Readiness:  &fakeReadiness{report: readyReport()},
		Devices:    noPrinters{},
		Stores:     noStorages{},
		Events:     noEvents{},
	})
	require.NoError(t, err)

	return server
}

// lines keeps every record a logger was handed, rendered the way a text handler
// would render it.
type lines struct {
	slog.Handler

	mu    sync.Mutex
	said  []string
	level []slog.Level
}

func (l *lines) Enabled(context.Context, slog.Level) bool { return true }

func (l *lines) Handle(_ context.Context, record slog.Record) error {
	rendered := record.Message

	record.Attrs(func(attr slog.Attr) bool {
		rendered += " " + attr.Key + "=" + attr.Value.String()

		return true
	})

	l.mu.Lock()
	defer l.mu.Unlock()

	l.said = append(l.said, rendered)
	l.level = append(l.level, record.Level)

	return nil
}

func (l *lines) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &attributed{parent: l, attrs: attrs}
}

func (l *lines) WithGroup(string) slog.Handler { return l }

func (l *lines) at(level slog.Level) []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	var out []string

	for position, said := range l.said {
		if l.level[position] == level {
			out = append(out, said)
		}
	}

	return slices.Clone(out)
}

// attributed carries what slog.Logger.With attached, so a listener name set
// once on the logger appears on every line it writes.
type attributed struct {
	parent *lines
	attrs  []slog.Attr
}

func (a *attributed) Enabled(context.Context, slog.Level) bool { return true }

func (a *attributed) Handle(ctx context.Context, record slog.Record) error {
	record.AddAttrs(a.attrs...)

	return a.parent.Handle(ctx, record)
}

func (a *attributed) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &attributed{parent: a.parent, attrs: append(slices.Clone(a.attrs), attrs...)}
}

func (a *attributed) WithGroup(string) slog.Handler { return a }
