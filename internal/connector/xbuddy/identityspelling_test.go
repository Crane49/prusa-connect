package xbuddy

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/prusa3d/prusa-connect/internal/kernel/admission"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
)

// The device type code reaching the capability model, over the real handshake.
//
// A printer that sends no User-Agent-Printer is filed under the printer_type
// its registration carried, which is a code and not a model name. That is the
// string the old backlog recorded as matching nothing on a real Core One, so this drives
// it from the websocket upgrade through to a resolved capability set.
func TestAPrinterThatSendsNoModelHeaderIsResolvedFromItsDeviceType(t *testing.T) {
	t.Parallel()

	const (
		fingerprint = "FINGERPRINT-SPELLING"
		deviceType  = "7.1.0"
	)

	driver := NewDriver(ModeToken, 4, 0, "", slog.New(slog.DiscardHandler))

	host := &takeOne{baseline: driver.Baseline(), sessions: make(chan *Session, 1)}

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	address := strings.TrimPrefix(server.URL, "http://")
	token := registered(t, driver, server.URL, fingerprint, deviceType)

	header := http.Header{}
	header.Set(fingerprintHeader, fingerprint)
	header.Set(tokenHeader, token)
	header.Set(firmwareHeader, "6.4.0")

	dialer := websocket.Dialer{Subprotocols: []string{subprotocol}}

	client, response, err := dialer.Dial("ws://"+address+websocketPath, header)
	if err != nil {
		t.Fatalf("dialling %s: %v", websocketPath, err)
	}

	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}

	t.Cleanup(func() { _ = client.Close() })

	var session *Session

	select {
	case session = <-host.sessions:
		t.Cleanup(func() { _ = session.Close() })
	case <-time.After(5 * time.Second):
		t.Fatal("no printer was announced, so this test is watching nothing")
	}

	if got := session.Identity().Model; got != deviceType {
		t.Fatalf("the driver filed the printer as model %q, want %q", got, deviceType)
	}

	profiles, err := profile.Builtin()
	if err != nil {
		t.Fatalf("loading the built-in profiles: %v", err)
	}

	resolver := capability.NewResolver(profiles, slog.New(slog.DiscardHandler))

	resolved := resolver.Arrive(t.Context(), driver.Baseline(), session.Identity(), nil)
	if !resolved.ProfileMatched {
		t.Fatalf("a printer filed as %q matched no profile and was held to %d coverage keys",
			deviceType, len(resolved.Capabilities))
	}

	if len(resolved.Capabilities) >= len(driver.Baseline()) {
		t.Errorf("the profile resolved %d of the driver's %d keys, so it narrowed nothing",
			len(resolved.Capabilities), len(driver.Baseline()))
	}
}

// The two spellings of one machine's fingerprint, over the real handshake.
//
// Buddy computes 50 characters and sends all of them in the body of
// POST /p/register, then puts the first 16 in the Fingerprint header of every
// request including the websocket upgrade (connect.cpp UpgradeRequest with
// FINGERPRINT_HDR_SIZE, httpc.cpp writing "%s: %.*s"). So the string this
// driver registered a printer under is never the string that printer dials
// with, and a table keyed on either one alone loses the machine at one of the
// two routes.
//
// The retired server met this and left a duplicate row behind
// (legacy/connector/xbuddy/auth.go). This drives the real asymmetry through
// the frozen wire and asserts the printer comes back as the printer it
// registered as, serial and all.
func TestAPrinterThatRegistersInFullAndDialsWithAPrefixIsOneMachine(t *testing.T) {
	t.Parallel()

	// Fifty base32 characters, which is what printerHash emits: the alphabet is
	// 0-9 then A-V and the length is FINGERPRINT_SIZE.
	const (
		full       = "3K7QA9VB2M0PDTUE5C1LGHJ4RS6N8FIO7Q2XA0KB9M3PD5"
		deviceType = "7.1.0"
	)

	prefix := full[:fingerprintHeaderLength]

	driver := NewDriver(ModeToken, 4, 0, "", slog.New(slog.DiscardHandler))

	// naming, so GET /p/auth has a host to answer with. Its id carries the
	// unit id and the model, which is what lets this case read which spelling
	// the driver resolved.
	host := &naming{takeOne{baseline: driver.Baseline(), sessions: make(chan *Session, 1)}}

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	token := registered(t, driver, server.URL, full, deviceType)

	// GET /p/auth carries the prefix too, and it must answer for the printer
	// that registered rather than refuse a credential it issued.
	status, body := askAuth(t, server, prefix, token)
	if status != http.StatusOK {
		t.Fatalf("GET %s answered %d for the prefix of a registered printer: %v", authPath, status, body)
	}

	// The answer names the printer that registered, which is what proves the
	// prefix resolved to the registered spelling rather than to a second
	// machine.
	if got, want := body[authIDMember], "dev-"+full+"-"+deviceType; got != want {
		t.Errorf("GET %s answered %q, want %q", authPath, got, want)
	}

	header := http.Header{}
	header.Set(fingerprintHeader, prefix)
	header.Set(tokenHeader, token)
	header.Set(firmwareHeader, "6.4.0")

	dialer := websocket.Dialer{Subprotocols: []string{subprotocol}}

	client, response, err := dialer.Dial("ws://"+strings.TrimPrefix(server.URL, "http://")+websocketPath, header)
	if err != nil {
		t.Fatalf("dialling %s with the 16 character prefix: %v", websocketPath, err)
	}

	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}

	t.Cleanup(func() { _ = client.Close() })

	var session *Session

	select {
	case session = <-host.sessions:
		t.Cleanup(func() { _ = session.Close() })
	case <-time.After(5 * time.Second):
		t.Fatal("no printer was announced, so the prefix reached no registration")
	}

	identity := session.Identity()

	if identity.UnitUID != full {
		t.Errorf("the printer is filed under unit uid %q, want the registered spelling %q",
			identity.UnitUID, full)
	}

	if identity.Serial != "SN-1" {
		t.Errorf("the printer carries serial %q, so its registration was not found", identity.Serial)
	}

	// And the spelling it actually dialled with rides along, so the host can
	// put the two together for a machine that got through this handshake. It is
	// reported here and not on the registration: the prefix is in the clear on
	// every request, and only the credential says which machine sent it.
	if identity.UnitUIDAlias != prefix {
		t.Errorf("the admission reports the second spelling %q, want the header spelling %q",
			identity.UnitUIDAlias, prefix)
	}
}

// Sixteen characters of a printer's fingerprint must not buy that printer's
// registration.
//
// A printer puts the leading sixteen characters of its fingerprint in the clear
// in the Fingerprint header of every request it makes, so those sixteen are not
// a secret and cannot be treated as one. What the printer never puts in a
// header is the other thirty-four, and that is what a registration is issued
// against.
//
// So a caller that registers a fingerprint sharing only the header prefix is a
// different machine: it gets a code and a token of its own, and neither of them
// is the first printer's. This drives that over POST /p/register and then
// checks the credential each side ended up with.
func TestAFingerprintSharingOnlyTheHeaderPrefixIsADifferentPrinter(t *testing.T) {
	t.Parallel()

	const (
		victim     = "3K7QA9VB2M0PDTUE5C1LGHJ4RS6N8FIO7Q2XA0KB9M3PD5"
		deviceType = "7.1.0"
	)

	// The same first sixteen characters, which is all a listener on one request
	// ever sees, and a different tail.
	shadow := victim[:fingerprintHeaderLength] + strings.Repeat("Z", len(victim)-fingerprintHeaderLength)

	driver := NewDriver(ModeToken, 8, 0, "", slog.New(slog.DiscardHandler))

	host := &naming{takeOne{baseline: driver.Baseline(), sessions: make(chan *Session, 1)}}

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	held := registered(t, driver, server.URL, victim, deviceType)
	taken := registered(t, driver, server.URL, shadow, deviceType)

	t.Logf("observed: the printer holds token %q and the shadow registration holds %q", held, taken)

	if held == taken {
		t.Fatalf("a fingerprint sharing sixteen characters was handed the printer's own token %q", held)
	}

	// The printer's own header spelling still resolves to the printer, because
	// it registered first, and it still needs the printer's token.
	prefix := victim[:fingerprintHeaderLength]

	if status, body := askAuth(t, server, prefix, held); status != http.StatusOK {
		t.Errorf("the printer's own credentials were refused with %d: %v", status, body)
	}

	if status, _ := askAuth(t, server, prefix, taken); status == http.StatusOK {
		t.Error("the shadow registration's token was accepted for the printer's header spelling")
	}

	// And the shadow is its own machine rather than the printer, which is what
	// keeps the printer's device id out of its reach.
	_, body := askAuth(t, server, shadow, taken)
	if got, want := body[authIDMember], "dev-"+shadow+"-"+deviceType; got != want {
		t.Errorf("the shadow registration resolves to %q, want %q", got, want)
	}
}

// The approval mode none makes for itself reaches the host, with both spellings
// of the machine's fingerprint on it.
//
// The host binds a device id at an approval, and mode none approves inside this
// table, so before the notice existed that binding never happened: the printer
// was given an id at its first handshake instead, under whichever spelling that
// handshake carried. On the bench that left one Core One as two devices
// (BUG-011).
func TestTheApprovalModeNoneMakesForItselfIsReportedToTheHost(t *testing.T) {
	t.Parallel()

	const full = "3K7QA9VB2M0PDTUE5C1LGHJ4RS6N8FIO7Q2XA0KB9M3PD5"

	driver := NewDriver(ModeNone, 4, 0, "", slog.New(slog.DiscardHandler))

	reported := make(chan admission.Registration, 2)
	deciders := make(chan string, 2)

	driver.UseApprovals(func(_ context.Context, item admission.Registration, by string) error {
		reported <- item
		deciders <- by

		return nil
	})

	host := &naming{takeOne{baseline: driver.Baseline(), sessions: make(chan *Session, 1)}}

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	body := strings.NewReader(`{"sn":"SN-1","fingerprint":"` + full + `","printer_type":"7.1.0","firmware":"6.4.0"}`)

	answered, err := http.Post(server.URL+registerPath, "application/json", body)
	if err != nil {
		t.Fatalf("POST %s: %v", registerPath, err)
	}

	_ = answered.Body.Close()

	if answered.StatusCode != http.StatusOK {
		t.Fatalf("POST %s answered %d", registerPath, answered.StatusCode)
	}

	var item admission.Registration

	select {
	case item = <-reported:
	case <-time.After(5 * time.Second):
		t.Fatal("the host was never told about the approval this driver made, so nothing bound an identity")
	}

	t.Logf("observed: the host was told about %q", item.Fingerprint)

	// One registration names one spelling, and it is the registered one. A
	// notice that carried the header spelling as well let the host bind a
	// device id from characters that travel in the clear, so a registration
	// sharing them with a working printer reached that printer's device id
	// through one operator approval. The header spelling reaches the host on
	// the handshake instead (connectorapi.Identity.UnitUIDAlias).
	if item.Fingerprint != full {
		t.Errorf("the notice names %q, want the registered spelling %q", item.Fingerprint, full)
	}

	if !item.Approved {
		t.Error("the notice reports a registration nobody approved")
	}

	// Nobody decided it, so the notice names nobody. An empty principal is what
	// keeps a driver's own approval from reading as an ownership transfer on
	// the host (internal/app.approvalKeepsTheIdentity).
	if by := <-deciders; by != "" {
		t.Errorf("the driver's own approval was reported as decided by %q", by)
	}
}
