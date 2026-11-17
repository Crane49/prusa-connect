package connectorapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const (
	verbStatus  = connectorapi.Capability("core.command.get_status")
	inboundPath = "/connect"
)

// TestAnInboundDriverIsADriver is the shape the product already ships: the
// printer dials the host and identifies itself in the handshake, so there is no
// endpoint for the host to read from configuration and nothing for it to dial.
func TestAnInboundDriverIsADriver(t *testing.T) {
	t.Parallel()

	var driver connectorapi.Driver = dialledInto{}

	inbound, outbound, err := connectorapi.Directions(driver)
	if err != nil {
		t.Fatalf("Directions: %v", err)
	}

	if inbound == nil {
		t.Fatal("a driver whose devices dial it is not recognised as inbound")
	}

	if outbound != nil {
		t.Error("a driver with no Connect method was recognised as outbound")
	}
}

// TestAMountedRouteBecomesASession is the join. A device reaches a driver's
// routes, identifies itself, and the host ends up holding a session for it,
// which is what nothing in this contract could express.
func TestAMountedRouteBecomesASession(t *testing.T) {
	t.Parallel()

	host := &recordingArrivals{profile: statusBaseline()}

	handler, err := dialledInto{}.Handler(host)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	// The host mounts under a prefix and strips it, so the driver's routes are
	// written from its own root and survive being mounted somewhere else.
	const prefix = "/connectors/dialled-into"

	server := httptest.NewServer(http.StripPrefix(prefix, handler))
	defer server.Close()

	body := strings.NewReader(`{"model":"FAKE","serial":"sn-1"}`)

	resp, err := http.Post(server.URL+prefix+inboundPath, "application/json", body)
	if err != nil {
		t.Fatalf("dialling in: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dialling in answered %d", resp.StatusCode)
	}

	if len(host.sessions) != 1 {
		t.Fatalf("the host holds %d sessions after one device dialled in", len(host.sessions))
	}

	if got := host.sessions[0].Identity().Serial; got != "sn-1" {
		t.Errorf("the announced session reports serial %q, and the device said sn-1", got)
	}
}

// TestARefusedDeviceNeverBecomesASession holds the other half: Admit is where
// the host says no, and a driver that announces anyway is announcing a device
// the host does not own.
func TestARefusedDeviceNeverBecomesASession(t *testing.T) {
	t.Parallel()

	host := &recordingArrivals{refuse: true}

	handler, err := dialledInto{}.Handler(host)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Post(server.URL+inboundPath, "application/json", strings.NewReader(`{"model":"FAKE"}`))
	if err != nil {
		t.Fatalf("dialling in: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		t.Error("a device the host refused was served anyway")
	}

	if len(host.sessions) != 0 {
		t.Errorf("the host holds %d sessions for a device it refused", len(host.sessions))
	}
}

// TestAnOutboundDriverStaysOutbound keeps the two halves symmetric: neither is
// the default and a driver says which it is by what it implements.
func TestAnOutboundDriverStaysOutbound(t *testing.T) {
	t.Parallel()

	inbound, outbound, err := connectorapi.Directions(dialledOut{})
	if err != nil {
		t.Fatalf("Directions: %v", err)
	}

	if outbound == nil {
		t.Error("a driver with Connect is not recognised as outbound")
	}

	if inbound != nil {
		t.Error("a driver with no Handler was recognised as inbound")
	}
}

// TestADriverThatIsNeitherIsRefused reports the wiring error at startup. A
// driver the host can reach in no direction is one whose printers never appear,
// which is the failure nobody can debug from the outside.
func TestADriverThatIsNeitherIsRefused(t *testing.T) {
	t.Parallel()

	if _, _, err := connectorapi.Directions(baselineOnly{}); !errors.Is(err, connectorapi.ErrNoDirection) {
		t.Errorf("a driver that is neither inbound nor outbound was accepted: %v", err)
	}
}

func statusBaseline() connectorapi.Set {
	return connectorapi.Set{verbStatus: {Flag: true}}
}

// dialledInto is the dialect this product ships: the device dials in and says
// what it is over the connection it opened.
type dialledInto struct{}

func (dialledInto) Baseline() connectorapi.Set { return statusBaseline() }

func (d dialledInto) Handler(arrivals connectorapi.Arrivals) (http.Handler, error) {
	mux := http.NewServeMux()

	mux.HandleFunc("POST "+inboundPath, func(w http.ResponseWriter, r *http.Request) {
		var reported struct {
			Model  string `json:"model"`
			Serial string `json:"serial"`
		}

		if err := json.NewDecoder(r.Body).Decode(&reported); err != nil {
			http.Error(w, "malformed identity", http.StatusBadRequest)

			return
		}

		identity := connectorapi.Identity{Model: reported.Model, Serial: reported.Serial}

		profile, err := arrivals.Admit(r.Context(), identity)
		if err != nil {
			http.Error(w, "refused", http.StatusForbidden)

			return
		}

		session := &inboundSession{
			identity: identity,
			caps:     d.Baseline().Intersect(profile),
			patches:  make(chan connectorapi.StatePatch),
		}

		if taken := arrivals.Arrived(r.Context(), session); taken != nil {
			_ = session.Close()
			http.Error(w, "not taken", http.StatusServiceUnavailable)

			return
		}

		w.WriteHeader(http.StatusOK)
	})

	return mux, nil
}

// dialledOut is the shape the contract already had.
type dialledOut struct{}

func (dialledOut) Baseline() connectorapi.Set { return statusBaseline() }

func (d dialledOut) Connect(
	_ context.Context, _ connectorapi.Endpoint, profile connectorapi.Set,
) (connectorapi.Session, error) {
	return &inboundSession{
		caps:    d.Baseline().Intersect(profile),
		patches: make(chan connectorapi.StatePatch),
	}, nil
}

// baselineOnly declares what it can do and offers the host no way to reach it.
type baselineOnly struct{}

func (baselineOnly) Baseline() connectorapi.Set { return statusBaseline() }

type recordingArrivals struct {
	profile  connectorapi.Set
	refuse   bool
	sessions []connectorapi.Session
}

func (a *recordingArrivals) Admit(_ context.Context, _ connectorapi.Identity) (connectorapi.Set, error) {
	if a.refuse {
		return nil, connectorapi.ErrDeviceRefused
	}

	return a.profile.Clone(), nil
}

func (a *recordingArrivals) Arrived(_ context.Context, session connectorapi.Session) error {
	a.sessions = append(a.sessions, session)

	return nil
}

type inboundSession struct {
	identity connectorapi.Identity
	caps     connectorapi.Set
	patches  chan connectorapi.StatePatch
	closed   bool
}

func (s *inboundSession) Identity() connectorapi.Identity          { return s.identity }
func (s *inboundSession) Capabilities() (connectorapi.Set, uint64) { return s.caps.Clone(), 1 }
func (s *inboundSession) State() <-chan connectorapi.StatePatch    { return s.patches }

func (s *inboundSession) Do(ctx context.Context, _ connectorapi.Command) (connectorapi.Result, error) {
	if err := ctx.Err(); err != nil {
		return connectorapi.Result{}, err
	}

	return connectorapi.Result{OK: true}, nil
}

func (s *inboundSession) Close() error {
	if !s.closed {
		s.closed = true

		close(s.patches)
	}

	return nil
}
