package diagnostics

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
)

func TestTextScrubsEveryShape(t *testing.T) {
	s := NewScrubber([]byte("salt"), nil)
	host := s.Hash("hooks.example.com")

	cases := []struct{ name, in, want string }{
		{"url", "post https://u:p@hooks.example.com/T0/B1/xyz?token=abc failed", "post https://<" + host + "> failed"},
		{"email", "mail jan.novak@example.cz now", "mail <email> now"},
		{"ipv4", "dial 192.168.1.20:8080 refused", "dial <ip> refused"},
		{"ipv6", "peer [fe80::1]:443 gone", "peer <ip> gone"},
		{"clock time", "at 12:34:56 it stopped", "at 12:34:56 it stopped"},
		{"bearer", "Authorization: Bearer abc.def-ghi", "Authorization: <bearer>"},
		{"jwt", "jwt eyJhbGciOi.eyJzdWIiOi.c2ln here", "jwt <jwt> here"},
		{"pair", "host=db password=hunter22 sslmode=off", "host=db password=<redacted> sslmode=off"},
		{
			"path", "open /home/jan/prints/benchy.gcode: denied",
			"open <path:" + s.Hash("/home/jan/prints/benchy.gcode") + ">: denied",
		},
		{"go frame", "main.go frame /build/src/internal/app/app.go:120", "main.go frame …/app.go:120"},
		{"api route", "route /api/v1/devices/{device_id} missing", "route /api/v1/devices/{device_id} missing"},
		{"long hex", "key 0123456789abcdef0123456789abcdef01 leaked", "key <redacted> leaked"},

		{"json password", `{"msg":"login","password":"hunter22"}`, `{"msg":"login","password":<redacted>}`},
		{"access_token", "access_token=abc123def", "access_token=<redacted>"},
		{"client_secret", "client_secret=s3cr3tval", "client_secret=<redacted>"},
		{"auth_token", "auth_token=x1y2z3w4", "auth_token=<redacted>"},
		{"api key header", "X-Api-Key: Hd7sKm2pQx9a", "X-Api-Key: <redacted>"},
		{"basic auth", "Authorization: Basic dXNlcjpwYXNz", "Authorization: <credentials>"},
		{"token auth", "Authorization: Token abc123", "Authorization: <credentials>"},
		{"cookie", "Cookie: session=abc123def456", "Cookie: <redacted>"},

		{
			"go function frame",
			"github.com/prusa3d/prusa-connect/internal/app.(*App).Run(0xc000010000, {0x1, 0x2})",
			"github.com/prusa3d/prusa-connect/internal/app.(*App).Run(0xc000010000, {0x1, 0x2})",
		},
		{"json basic auth", `{"Authorization":"Basic dXNlcjpwYXNz"}`, `{"Authorization":"<credentials>"}`},
		{"json cookie", `{"Cookie":"session=abc123def456"}`, `{"Cookie":"<redacted>"}`},
		{
			"http.Header as JSON", `{"Authorization":["Basic dXNlcjpwYXNz"],"Set-Cookie":["a=b"]}`,
			`{"Authorization":["<credentials>"],"Set-Cookie":["<redacted>"]}`,
		},
		{
			"schemeless webhook", "post hooks.slack.com/services/T0/B1/abcdEFGH1234 failed",
			"post <path:" + s.Hash("hooks.slack.com/services/T0/B1/abcdEFGH1234") + "> failed",
		},
		{
			"http.Header via %v", "map[Authorization:[Basic dXNlcjpwYXNz] Cookie:[a=b]]",
			"map[Authorization:[<credentials>] Cookie:[<redacted>]]",
		},
		{
			"unknown go.* host", "go.mycorp.com/hooks/abcdEFGH1234",
			"<path:" + s.Hash("go.mycorp.com/hooks/abcdEFGH1234") + ">",
		},
		{"go.uber.org module", "go.uber.org/zap/zapcore", "go.uber.org/zap/zapcore"},
		{"module path", "github.com/danielgtaylor/huma/v2", "github.com/danielgtaylor/huma/v2"},

		{
			"trace id",
			"trace_id=4bf92f3577b34da6a3ce929d0e0e4736",
			"trace_id=" + s.Hash("4bf92f3577b34da6a3ce929d0e0e4736"),
		},
		{"uuid", "id 123e4567-e89b-12d3-a456-426614174000 ok", "id 123e4567-e89b-12d3-a456-426614174000 ok"},
		{
			"commit revision", "vcs.revision=0123456789abcdef0123456789abcdef01234567",
			"vcs.revision=0123456789abcdef0123456789abcdef01234567",
		},
		{
			"long api route", "GET /api/v1/printers/telemetry/history 500",
			"GET /api/v1/printers/telemetry/history 500",
		},
		{
			"module name", "module prusa-connect-fleet-statistics failed",
			"module prusa-connect-fleet-statistics failed",
		},
		{"random token", "tok Zx9Qm3Lk8Vt2Rb7Nw4Pc6Hy1Jd5Fs0GaUe3TiKo end", "tok <redacted> end"},

		{
			"resolver error", "dial tcp: lookup mqtt.home.lan on <ip>: no such host",
			"dial tcp: lookup <" + s.Hash("mqtt.home.lan") + "> on <ip>: no such host",
		},
		{
			"host and port", "dial tcp broker.example.net:8883: i/o timeout",
			"dial tcp <" + s.Hash("broker.example.net") + ">:8883: i/o timeout",
		},
		{"local name", "answering for workshop.local now", "answering for <" + s.Hash("workshop.local") + "> now"},
		{"go position", "panic at main.go:42", "panic at main.go:42"},
		{
			"quoted user", `password authentication failed for user "lukas"`,
			"password authentication failed for user <" + s.Hash("lukas") + ">",
		},
		{"mac", "printer 00:1A:2b:3C:4d:5E joined", "printer <mac> joined"},
		{
			"home dir with space",
			"open /Users/Jan Novak/Documents/x.gcode",
			"open <path:" + s.Hash("/Users/Jan Novak/Documents/x.gcode") + ">",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, s.Text(tc.in))
		})
	}
}

func TestSweepRemovesHTMLEscapedJSONSecret(t *testing.T) {
	s := NewScrubber([]byte("salt"), []string{`pa"ss&wd`})

	require.NotContains(t, string(s.Sweep([]byte(`{"a":"pa\"ss&wd"}`))), "ss&wd")
	require.NotContains(t, string(s.Sweep([]byte(`{"a":"pa\"ss\u0026wd"}`))), "u0026wd")
}

func TestSweepRemovesRawAndJSONEscapedSecrets(t *testing.T) {
	s := NewScrubber([]byte("salt"), []string{`pa"ss\word`, "tiny", "longsecretvalue"})

	out := string(s.Sweep([]byte(`{"a":"pa\"ss\\word","b":"x longsecretvalue y","c":"tiny"}`)))

	require.NotContains(t, out, "longsecretvalue")
	require.NotContains(t, out, `pa\"ss\\word`)
	require.Contains(t, out, "tiny", "secrets shorter than 6 bytes are not swept")
}

// TestTextSweepsAKnownSecretBeforeAPatternCutsIt: the pair pattern stops at
// an ampersand, and a secret swept only afterwards would leave its tail.
func TestTextSweepsAKnownSecretBeforeAPatternCutsIt(t *testing.T) {
	s := NewScrubber([]byte("salt"), []string{"Tr0ub4dor&3xyz"})

	require.Equal(t, "password=<redacted>", s.Text("password=Tr0ub4dor&3xyz"))
}

func TestClipCutsALongLineAfterTheSweep(t *testing.T) {
	secret := "planted-secret-value"
	s := NewScrubber([]byte("salt"), []string{secret})
	line := strings.Repeat("a", maxLineBytes-5) + secret + strings.Repeat("b", 100)

	clipped := s.clip(line)

	require.NotContains(t, clipped, "plant")
	require.LessOrEqual(t, len(clipped), maxLineBytes+len(machinetext.Marker))
	require.True(t, strings.HasSuffix(clipped, machinetext.Marker))
}

func TestHashIsStableWithinASaltAndDiffersAcross(t *testing.T) {
	a, b := NewScrubber([]byte("one"), nil), NewScrubber([]byte("two"), nil)

	first := a.Hash("SN123")

	require.Equal(t, first, a.Hash("SN123"))
	require.NotEqual(t, a.Hash("SN123"), b.Hash("SN123"))
	require.Len(t, a.Hash("SN123"), 14)
	require.Empty(t, a.Hash(""))
}
