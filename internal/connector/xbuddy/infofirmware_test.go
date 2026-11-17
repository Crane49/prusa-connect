package xbuddy

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A printer whose handshake carries no User-Agent-Version is filed under the
// firmware its registration named, and a token outlives a firmware upgrade, so
// INFO is the only thing that says what it runs now. The session ends once INFO
// names another firmware, and the printer's next handshake is filed under it.
func TestAPrinterWithNoVersionHeaderIsFiledUnderTheFirmwareItsInfoReports(t *testing.T) {
	t.Parallel()

	const fingerprint = "FINGERPRINT-INFO-FIRMWARE"

	driver := NewDriver(ModeToken, 4, 0, "", slog.New(slog.DiscardHandler))
	host := &takeOne{baseline: driver.Baseline(), sessions: make(chan *Session, 2)}

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	token := registered(t, driver, server.URL, fingerprint, "MK4")

	header := http.Header{}
	header.Set(fingerprintHeader, fingerprint)
	header.Set(tokenHeader, token)
	header.Set(modelHeader, "MK4")

	dial := func() (*websocket.Conn, *Session) {
		t.Helper()

		dialer := websocket.Dialer{Subprotocols: []string{subprotocol}}

		client, response, dialErr := dialer.Dial(
			"ws://"+strings.TrimPrefix(server.URL, "http://")+websocketPath, header)
		if dialErr != nil {
			t.Fatalf("dialling %s: %v", websocketPath, dialErr)
		}

		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}

		t.Cleanup(func() { _ = client.Close() })

		select {
		case session := <-host.sessions:
			t.Cleanup(func() { _ = session.Close() })

			return client, session
		case <-time.After(5 * time.Second):
			t.Fatal("no printer was announced")

			return nil, nil
		}
	}

	info := []byte(`{"event":"INFO","state":"IDLE","data":{"firmware":"6.5.0"}}`)

	client, first := dial()
	if got := firmwareText(first.Identity().Firmware); got != "6.4.0" {
		t.Fatalf("the first handshake was filed under firmware %q, want the registered 6.4.0", got)
	}

	if err = client.WriteMessage(websocket.TextMessage, info); err != nil {
		t.Fatalf("sending INFO: %v", err)
	}

	select {
	case <-first.done:
	case <-time.After(5 * time.Second):
		t.Fatal("INFO reported firmware 6.5.0 and the session built for 6.4.0 is still open")
	}

	client, second := dial()
	if got := firmwareText(second.Identity().Firmware); got != "6.5.0" {
		t.Fatalf("the next handshake was filed under firmware %q, want 6.5.0 from INFO", got)
	}

	// The same INFO again agrees with the session, so it stays.
	if err = client.WriteMessage(websocket.TextMessage, info); err != nil {
		t.Fatalf("sending INFO again: %v", err)
	}

	select {
	case <-second.done:
		t.Fatal("an INFO naming the firmware the session was built for ended it")
	case <-time.After(300 * time.Millisecond):
	}
}
