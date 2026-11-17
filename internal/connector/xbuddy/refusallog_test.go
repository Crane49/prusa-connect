package xbuddy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// lines is a log the test reads back, one JSON object per record.
type lines struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.Write(p)
}

func (l *lines) records(t *testing.T) []map[string]any {
	t.Helper()

	l.mu.Lock()
	defer l.mu.Unlock()

	var out []map[string]any

	for raw := range strings.SplitSeq(strings.TrimSpace(l.buf.String()), "\n") {
		if raw == "" {
			continue
		}

		record := map[string]any{}
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			t.Fatalf("a log line is not JSON: %v: %s", err, raw)
		}

		out = append(out, record)
	}

	return out
}

// refusingHost is an Arrivals that admits nobody.
type refusingHost struct{}

func (refusingHost) Admit(context.Context, connectorapi.Identity) (connectorapi.Set, error) {
	return nil, errors.New("the printer is blocked")
}

func (refusingHost) Arrived(context.Context, connectorapi.Session) error { return nil }

// dial sends one websocket handshake the way firmware does and returns the
// status the driver answered.
func dial(t *testing.T, handler http.Handler, fingerprint, token string) int {
	t.Helper()

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, websocketPath, nil)
	request.Header.Set(fingerprintHeader, fingerprint)
	request.Header.Set(tokenHeader, token)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	return recorder.Code
}

// A printer knocking with a token this server never issued is told 401 every
// few seconds. The operator learns it once a minute per printer, with what to
// do about it, and never once per attempt.
func TestARefusedWebsocketIsLoggedOnceAMinutePerPrinter(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		out := &lines{}
		driver := NewDriver(ModeToken, 4, 0, "", slog.New(slog.NewJSONHandler(out, nil)))

		handler, err := driver.Handler(&takeOne{baseline: driver.Baseline()})
		if err != nil {
			t.Fatalf("handler: %v", err)
		}

		for range 5 {
			if got := dial(t, handler, "FP-UNKNOWN-0001", "made-up"); got != http.StatusUnauthorized {
				t.Fatalf("a made-up token was answered %d, want 401", got)
			}
		}

		if got := dial(t, handler, "FP-UNKNOWN-0002", "made-up"); got != http.StatusUnauthorized {
			t.Fatalf("a second printer was answered %d, want 401", got)
		}

		records := out.records(t)
		if len(records) != 2 {
			t.Fatalf("six refusals from two printers wrote %d lines, want 2: %v", len(records), records)
		}

		first := records[0]
		for key, want := range map[string]any{
			"level": "WARN", "status": float64(http.StatusUnauthorized),
			"fingerprint": "FP-UNKNOWN-0001", "mode": string(ModeToken), "remote": "192.0.2.1",
		} {
			if first[key] != want {
				t.Errorf("the line carries %s=%v, want %v", key, first[key], want)
			}
		}

		if reason, _ := first["reason"].(string); !strings.Contains(reason, "not one this driver issued") {
			t.Errorf("the line gives the reason %q, which does not say the token is unknown", reason)
		}

		if message, _ := first["msg"].(string); !strings.Contains(message, "Add Printer to Connect") {
			t.Errorf("the line %q does not tell the operator how a printer gets a token", message)
		}

		time.Sleep(refusalLogEvery)
		dial(t, handler, "FP-UNKNOWN-0001", "made-up")

		if got := len(out.records(t)); got != 3 {
			t.Errorf("a refusal a minute later wrote %d lines in all, want 3", got)
		}
	})
}

// The host may refuse a printer whose credentials were good. That is a 403,
// and it is logged with the host's reason.
func TestAWebsocketTheHostRefusesIsLogged(t *testing.T) {
	t.Parallel()

	out := &lines{}
	driver := NewDriver(ModeNone, 4, 0, "", slog.New(slog.NewJSONHandler(out, nil)))

	handler, err := driver.Handler(refusingHost{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if got := dial(t, handler, "FP-BLOCKED-0001", "any"); got != http.StatusForbidden {
		t.Fatalf("a printer the host refuses was answered %d, want 403", got)
	}

	records := out.records(t)
	if len(records) != 1 {
		t.Fatalf("one refusal wrote %d lines, want 1", len(records))
	}

	if records[0]["status"] != float64(http.StatusForbidden) || records[0]["reason"] != "the printer is blocked" {
		t.Errorf("the line is %v, want status 403 and the host's reason", records[0])
	}
}

// The fingerprint header is the caller's to choose, so the table of printers
// the log remembers does not grow past its limit.
func TestTheRefusalLogStaysBounded(t *testing.T) {
	t.Parallel()

	var log refusalLog

	now := time.Unix(0, 0)

	for index := range refusalLogLimit {
		if !log.due("fp-"+strconv.Itoa(index), now) {
			t.Fatalf("key %d was refused before the table was full", index)
		}
	}

	if log.due("one-more", now) {
		t.Error("a full table took one more key")
	}

	if got := len(log.last); got != refusalLogLimit {
		t.Errorf("the table holds %d keys, want %d", got, refusalLogLimit)
	}

	if !log.due("one-more", now.Add(refusalLogEvery)) {
		t.Error("a full table did not make room once its keys were a window old")
	}
}

// notices collects what the driver tells the host about refused printers.
type notices struct {
	mu   sync.Mutex
	seen []connectorapi.DeviceRefusal
}

func (n *notices) take(_ context.Context, refusal connectorapi.DeviceRefusal) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.seen = append(n.seen, refusal)
}

func (n *notices) all() []connectorapi.DeviceRefusal {
	n.mu.Lock()
	defer n.mu.Unlock()

	return append([]connectorapi.DeviceRefusal(nil), n.seen...)
}

// The host hears about a refused printer on the clock the log uses, with the
// reason and what the printer said it is, and never with its fingerprint.
func TestARefusedWebsocketIsReportedToTheHostOnceAMinute(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		heard := &notices{}
		driver := NewDriver(ModeToken, 4, 0, "", nil)
		driver.UseRefusals(heard.take)

		handler, err := driver.Handler(&takeOne{baseline: driver.Baseline()})
		if err != nil {
			t.Fatalf("handler: %v", err)
		}

		knock := func() {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, websocketPath, nil)
			request.Header.Set(fingerprintHeader, "FP-UNKNOWN-0001")
			request.Header.Set(tokenHeader, "made-up")
			request.Header.Set(modelHeader, "MK4")
			request.Header.Set(firmwareHeader, "6.4.0")
			handler.ServeHTTP(httptest.NewRecorder(), request)
		}

		for range 4 {
			knock()
		}

		got := heard.all()
		if len(got) != 1 {
			t.Fatalf("four knocks inside a minute were reported %d times, want 1", len(got))
		}

		want := connectorapi.DeviceRefusal{
			Reason:            connectorapi.RefusalUnknownCredentials,
			FingerprintDigest: "b759c24eb2f3626b1fc8b9cf0cfad8e01e09f2fb51b6a701b9e5361b89974fae",
			Model:             "MK4",
			Firmware:          "6.4.0",
		}
		if got[0] != want {
			t.Errorf("the host was told %+v, want %+v", got[0], want)
		}

		time.Sleep(refusalLogEvery)
		knock()

		if got := len(heard.all()); got != 2 {
			t.Errorf("a knock a minute later made %d reports in all, want 2", got)
		}
	})
}

// A printer the host would not take is reported as not admitted.
func TestAWebsocketTheHostRefusesIsReportedAsNotAdmitted(t *testing.T) {
	t.Parallel()

	heard := &notices{}
	driver := NewDriver(ModeNone, 4, 0, "", nil)
	driver.UseRefusals(heard.take)

	handler, err := driver.Handler(refusingHost{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	dial(t, handler, "FP-BLOCKED-0001", "any")

	got := heard.all()
	if len(got) != 1 || got[0].Reason != connectorapi.RefusalNotAdmitted {
		t.Fatalf("the host was told %+v, want one refusal with reason %s", got, connectorapi.RefusalNotAdmitted)
	}
}

// A caller choosing a new fingerprint for every knock is a new printer to the
// once-a-minute table, so the host is told about a bounded number in all.
func TestRefusalNoticesAreBoundedPerMinuteWhateverTheFingerprint(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		heard := &notices{}
		driver := NewDriver(ModeToken, 4, 0, "", nil)
		driver.UseRefusals(heard.take)

		handler, err := driver.Handler(&takeOne{baseline: driver.Baseline()})
		if err != nil {
			t.Fatalf("handler: %v", err)
		}

		for index := range 3 * refusalNoticeLimit {
			dial(t, handler, "FP-INVENTED-"+strconv.Itoa(index), "made-up")
		}

		if got := len(heard.all()); got != refusalNoticeLimit {
			t.Fatalf("knocks under %d invented fingerprints made %d notices, want %d",
				3*refusalNoticeLimit, got, refusalNoticeLimit)
		}

		time.Sleep(refusalLogEvery)
		dial(t, handler, "FP-INVENTED-NEXT", "made-up")

		if got := len(heard.all()); got != refusalNoticeLimit+1 {
			t.Errorf("a knock in the next minute made %d notices in all, want %d", got, refusalNoticeLimit+1)
		}
	})
}
