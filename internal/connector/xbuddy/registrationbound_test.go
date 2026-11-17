package xbuddy_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi/connectorapitest"
)

// The bound this suite is written against. It is small so a test fills it, and
// the shipping default is defaultMaxRegistrations.
const testLimit = 8

// wire is the two registration requests a printer makes, against the driver's
// own handler over a real listener.
type wire struct {
	server *httptest.Server
	driver *xbuddy.Driver
}

func newWire(t *testing.T, mode xbuddy.Mode, limit int) wire {
	t.Helper()

	driver := xbuddy.NewDriver(mode, 4, 0, "", nil, xbuddy.WithMaxRegistrations(limit))

	handler, err := driver.Handler(connectorapitest.NewHost(driver.Baseline()))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return wire{server: server, driver: driver}
}

// approve is the operator's half, which every mode but none waits for.
func (w wire) approve(t *testing.T, code string) {
	t.Helper()

	if _, err := w.driver.Approve(t.Context(), code, "user:tester"); err != nil {
		t.Fatalf("approve %s: %v", code, err)
	}
}

// register is POST /p/register. It returns the status and the Code header.
func (w wire) register(t *testing.T, fingerprint string) (int, string) {
	t.Helper()

	body := fmt.Sprintf(`{"sn":"SN-%s","fingerprint":%q,"printer_type":"7.1.0","firmware":"6.4.0-0"}`,
		fingerprint, fingerprint)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		w.server.URL+"/p/register", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build the registration: %v", err)
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := w.server.Client().Do(request)
	if err != nil {
		t.Fatalf("register %s: %v", fingerprint, err)
	}

	defer func() { _ = response.Body.Close() }()

	return response.StatusCode, response.Header.Get("Code")
}

// poll is GET /p/register, which is what a printer does until it is approved.
// It returns the status and the Token header.
func (w wire) poll(t *testing.T, code string) (int, string) {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, w.server.URL+"/p/register", nil)
	if err != nil {
		t.Fatalf("build the poll: %v", err)
	}

	request.Header.Set("Code", code)

	response, err := w.server.Client().Do(request)
	if err != nil {
		t.Fatalf("poll %s: %v", code, err)
	}

	defer func() { _ = response.Body.Close() }()

	return response.StatusCode, response.Header.Get("Token")
}

// TestTheRegistrationTableDropsWhatNoPrinterCompleted is the bound on a path
// that carries no credential.
//
// A printer has none before it registers, so `POST /p/register` cannot ask for
// one. Anything that can open a socket may therefore put an entry in the table,
// and a bench run publishes the port on a cable. What separates a printer from
// everything else is the second request: a printer polls with its code and
// collects the token, and a caller minting fingerprints does not.
//
// So the entry dropped is the oldest one nobody completed. A printer that
// finished its handshake keeps its code through a flood of registrations it did
// not make.
func TestTheRegistrationTableDropsWhatNoPrinterCompleted(t *testing.T) {
	t.Parallel()

	printers := newWire(t, xbuddy.ModeToken, testLimit)

	status, kept := printers.register(t, "FP-KEEP")
	if status != http.StatusOK {
		t.Fatalf("a printer registering was answered %d", status)
	}

	// Mode token waits for an operator, so this is the printer somebody let
	// through. The flood below is what nobody decided.
	printers.approve(t, kept)

	if polled, token := printers.poll(t, kept); polled != http.StatusOK || token == "" {
		t.Fatalf("the printer's poll answered %d with token %q", polled, token)
	}

	status, abandoned := printers.register(t, "FP-EARLY")
	if status != http.StatusOK {
		t.Fatalf("a second registration was answered %d", status)
	}

	// Four times the bound, so the table turns over completely and then some.
	for index := range testLimit * 4 {
		if status, _ = printers.register(t, fmt.Sprintf("FP-FLOOD-%d", index)); status != http.StatusOK {
			t.Fatalf("registration %d was answered %d, and nothing here may be refused", index, status)
		}
	}

	// The printer that completed still holds the code it was given.
	if again, code := printers.register(t, "FP-KEEP"); again != http.StatusOK || code != kept {
		t.Errorf("the printer that completed its handshake answered %d with code %q, want %q",
			again, code, kept)
	}

	if polled, token := printers.poll(t, kept); polled != http.StatusOK || token == "" {
		t.Errorf("the printer that completed its handshake polls %d with token %q", polled, token)
	}

	// The one nobody completed is gone, which is what bounds the table.
	if polled, _ := printers.poll(t, abandoned); polled != http.StatusNotFound {
		t.Errorf("a registration no printer completed answers %d after %d more, want %d",
			polled, testLimit*4, http.StatusNotFound)
	}
}

// TestADroppedRegistrationTakesItsTokenWithIt is the half that makes dropping
// an entry safe to write. Four tables hold one registration, and a token left
// in the allowlist after its entry went would admit a printer this driver no
// longer has a record of.
//
// The drop here is an operator's rejection, which is the only one that can take
// a registration holding a token. Approving one takes an operator, and an entry a person endorsed is never evicted
// to make room: a table holding nothing else refuses the new printer instead,
// which is TestATableOfOperatorApprovedRegistrationsRefusesANewPrinter.
func TestADroppedRegistrationTakesItsTokenWithIt(t *testing.T) {
	t.Parallel()

	printers := newWire(t, xbuddy.ModeToken, 2)

	_, first := printers.register(t, "FP-1")

	printers.approve(t, first)

	_, token := printers.poll(t, first)
	if token == "" {
		t.Fatalf("the first printer collected no token")
	}

	if err := printers.driver.Reject(t.Context(), first); err != nil {
		t.Fatalf("reject %s: %v", first, err)
	}

	if polled, _ := printers.poll(t, first); polled != http.StatusNotFound {
		t.Fatalf("the first registration still answers %d, so nothing was dropped", polled)
	}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, printers.server.URL+"/p/ws", nil)
	if err != nil {
		t.Fatalf("build the handshake: %v", err)
	}

	request.Header.Set("Fingerprint", "FP-1")
	request.Header.Set("Token", token)
	request.Header.Set("Sec-WebSocket-Protocol", "prusa-connect")

	response, err := printers.server.Client().Do(request)
	if err != nil {
		t.Fatalf("dial the websocket: %v", err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusUnauthorized {
		t.Errorf("the token of a dropped registration was answered %d, want %d",
			response.StatusCode, http.StatusUnauthorized)
	}
}

// TestATableOfOperatorApprovedRegistrationsRefusesANewPrinter is the one case
// where a registration is refused rather than made room for.
//
// An entry an operator approved is a decision a person took, and
// dropping it silently undoes that decision. A table holding nothing else says
// so with 503 and a Retry-After, which is an answer a printer can act on.
func TestATableOfOperatorApprovedRegistrationsRefusesANewPrinter(t *testing.T) {
	t.Parallel()

	driver := xbuddy.NewDriver(xbuddy.ModeToken, 4, 0, "", nil, xbuddy.WithMaxRegistrations(2))

	handler, err := driver.Handler(connectorapitest.NewHost(driver.Baseline()))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	printers := wire{server: server, driver: driver}

	for _, fingerprint := range []string{"FP-1", "FP-2"} {
		_, code := printers.register(t, fingerprint)

		if _, approveErr := driver.Approve(t.Context(), code, "user:tester"); approveErr != nil {
			t.Fatalf("approve %s: %v", fingerprint, approveErr)
		}
	}

	status, code := printers.register(t, "FP-3")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("a printer meeting a full table was answered %d with code %q, want %d",
			status, code, http.StatusServiceUnavailable)
	}

	// The two an operator approved are untouched, which is the point of the
	// refusal.
	for _, fingerprint := range []string{"FP-1", "FP-2"} {
		if again, _ := printers.register(t, fingerprint); again != http.StatusOK {
			t.Errorf("the approved registration for %s answers %d", fingerprint, again)
		}
	}
}
