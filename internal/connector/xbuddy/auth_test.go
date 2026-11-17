package xbuddy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// naming is a host that answers the seam /p/auth needs. The id it mints carries
// the fingerprint and the model, so a test can tell an identity the driver
// assembled from one it passed the fingerprint through.
type naming struct {
	takeOne
}

func (h *naming) DeviceID(identity connectorapi.Identity) string {
	return "dev-" + identity.UnitUID + "-" + identity.Model
}

// The frozen identity lookup, end to end over the driver's own wire.
//
// A printer that has registered asks what this server calls it and is answered
// with the id the device paths use. The two refusals are the halves that keep
// the path from being a lookup service: a request short of a header never
// reaches the table, and a credential this driver did not issue is refused
// whatever fingerprint it names.
func TestThePrinterLearnsTheIDThisServerKnowsItBy(t *testing.T) {
	t.Parallel()

	const (
		fingerprint = "FINGERPRINT-AUTH"
		deviceType  = "MK4"
	)

	driver := NewDriver(ModeToken, 4, 0, "", nil)
	host := &naming{takeOne{baseline: driver.Baseline(), sessions: make(chan *Session, 1)}}

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	token := registered(t, driver, server.URL, fingerprint, deviceType)

	cases := []struct {
		name        string
		fingerprint string
		token       string
		status      int
		member      string
		value       string
	}{
		{
			name: "no headers at all", status: http.StatusUnauthorized, member: authErrorMember,
		},
		{
			name: "a fingerprint and no token", fingerprint: fingerprint,
			status: http.StatusUnauthorized, member: authErrorMember,
		},
		{
			name: "a token and no fingerprint", token: token,
			status: http.StatusUnauthorized, member: authErrorMember,
		},
		{
			name: "a token this driver did not issue", fingerprint: fingerprint, token: "NOTATOKEN",
			status: http.StatusForbidden, member: authErrorMember,
		},
		{
			name: "another printer's fingerprint", fingerprint: "SOMEBODY-ELSE", token: token,
			status: http.StatusForbidden, member: authErrorMember,
		},
		{
			name: "the credentials it registered with", fingerprint: fingerprint, token: token,
			status: http.StatusOK, member: authIDMember, value: "dev-" + fingerprint + "-" + deviceType,
		},
	}

	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()

			status, body := askAuth(t, server, item.fingerprint, item.token)

			if status != item.status {
				t.Fatalf("GET %s answered %d with %v, want %d", authPath, status, body, item.status)
			}

			answer, carried := body[item.member]
			if !carried {
				t.Fatalf("the answer carries %v, and a %d owes the caller a %q member",
					body, item.status, item.member)
			}

			if item.value != "" && answer != item.value {
				t.Errorf("the printer was told it is %q, want %q", answer, item.value)
			}

			if _, leaked := body[authIDMember]; leaked && item.status != http.StatusOK {
				t.Errorf("a refusal carried an id: %v", body)
			}
		})
	}
}

// A host that cannot name a device says so rather than answering with something
// it made up. The seam is optional for the reason admits is, and this is the
// answer a driver mounted on a host that does not implement it gives.
func TestAHostThatMintsNoIDRefusesTheLookup(t *testing.T) {
	t.Parallel()

	const fingerprint = "FINGERPRINT-NAMELESS"

	driver := NewDriver(ModeToken, 4, 0, "", nil)
	host := &takeOne{baseline: driver.Baseline(), sessions: make(chan *Session, 1)}

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	token := registered(t, driver, server.URL, fingerprint, "MK4")

	status, body := askAuth(t, server, fingerprint, token)
	if status != http.StatusNotImplemented {
		t.Fatalf("GET %s answered %d with %v, want %d", authPath, status, body, http.StatusNotImplemented)
	}

	if _, leaked := body[authIDMember]; leaked {
		t.Errorf("a host that mints no id answered with one: %v", body)
	}
}

// The route answers one method, and the 405 names it.
func TestTheIdentityLookupAnswersOneMethod(t *testing.T) {
	t.Parallel()

	driver := NewDriver(ModeNone, 4, 0, "", nil)
	host := &naming{takeOne{baseline: driver.Baseline(), sessions: make(chan *Session, 1)}}

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+authPath, nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("POST %s: %v", authPath, err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST %s answered %d, want %d", authPath, response.StatusCode, http.StatusMethodNotAllowed)
	}

	if allow := response.Header.Get("Allow"); allow != http.MethodGet {
		t.Errorf("the 405 named %q as what this route answers, want %q", allow, http.MethodGet)
	}
}

// askAuth makes one GET /p/auth and reads the flat string map back.
func askAuth(t *testing.T, server *httptest.Server, fingerprint, token string) (int, map[string]string) {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+authPath, nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}

	if fingerprint != "" {
		request.Header.Set(fingerprintHeader, fingerprint)
	}

	if token != "" {
		request.Header.Set(tokenHeader, token)
	}

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", authPath, err)
	}

	defer func() { _ = response.Body.Close() }()

	body := map[string]string{}
	if decodeErr := json.NewDecoder(response.Body).Decode(&body); decodeErr != nil {
		t.Fatalf("the answer at %s is not a flat string map: %v", authPath, decodeErr)
	}

	return response.StatusCode, body
}
