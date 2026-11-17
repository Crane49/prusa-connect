package connectorapitest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

const (
	verbHome  = connectorapi.Capability("core.command.home")
	verbPrint = connectorapi.Capability("core.command.start_print")
	argPath   = connectorapi.Capability("core.command.start_print#path")
)

// A suite that passes everything proves nothing, so each of these drives an
// assertion with input that must break it.

func TestBaselineErrorsCatchesABrokenDescriptor(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		baseline connectorapi.Set
	}{
		{name: "empty", baseline: connectorapi.Set{}},
		{
			name:     "a key the vocabulary's syntax rejects",
			baseline: connectorapi.Set{"core.command.START_PRINT": {Flag: true}},
		},
		{
			name:     "a kind outside the defined set",
			baseline: connectorapi.Set{verbHome: {Flag: true, Kind: connectorapi.Kind(99)}},
		},
		{
			name: "a default outside its own supported set",
			baseline: connectorapi.Set{
				verbPrint: {Flag: true},
				argPath:   {Flag: true, Kind: connectorapi.KindString, Supported: []string{"a"}, Default: "b"},
			},
		},
		{
			name:     "a flag that constrains values nothing will read",
			baseline: connectorapi.Set{verbHome: {Flag: true, Supported: []string{"a"}}},
		},
		{
			name:     "an argument whose verb is not declared",
			baseline: connectorapi.Set{argPath: {Flag: true, Kind: connectorapi.KindString}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if problems := BaselineErrors(tc.baseline); len(problems) == 0 {
				t.Error("the suite accepted a baseline that breaks the contract")
			}
		})
	}
}

func TestBaselineErrorsAcceptsAGoodDescriptor(t *testing.T) {
	t.Parallel()

	if problems := BaselineErrors(goodBaseline()); len(problems) != 0 {
		t.Errorf("the suite rejected a conforming baseline: %v", problems)
	}
}

func TestResolutionErrorsCatchesAWidenedSet(t *testing.T) {
	t.Parallel()

	baseline := connectorapi.Set{
		verbPrint: {Flag: true},
		verbHome:  {Flag: false},
		argPath: {
			Flag: true, Kind: connectorapi.KindString,
			Supported: []string{"a", "b"}, Limit: 10, HasLimit: true,
		},
	}

	for _, tc := range []struct {
		name     string
		resolved connectorapi.Set
	}{
		{
			name:     "a key the baseline never declared",
			resolved: connectorapi.Set{"core.command.beep": {Flag: true}},
		},
		{
			name:     "a capability turned on that the baseline has off",
			resolved: connectorapi.Set{verbHome: {Flag: true}},
		},
		{
			name: "a value the baseline does not accept",
			resolved: connectorapi.Set{argPath: {
				Flag: true, Kind: connectorapi.KindString,
				Supported: []string{"a", "z"}, Limit: 10, HasLimit: true,
			}},
		},
		{
			name: "a limit raised above the baseline's",
			resolved: connectorapi.Set{argPath: {
				Flag: true, Kind: connectorapi.KindString,
				Supported: []string{"a"}, Limit: 99, HasLimit: true,
			}},
		},
		{
			name: "a limit dropped entirely",
			resolved: connectorapi.Set{argPath: {
				Flag: true, Kind: connectorapi.KindString, Supported: []string{"a"},
			}},
		},
		{
			name: "a kind changed under the same key",
			resolved: connectorapi.Set{argPath: {
				Flag: true, Kind: connectorapi.KindInteger, Limit: 10, HasLimit: true,
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if problems := ResolutionErrors(baseline, tc.resolved); len(problems) == 0 {
				t.Error("the suite accepted a resolved set that permits more than the baseline")
			}
		})
	}
}

func TestResolutionErrorsAcceptsANarrowedSet(t *testing.T) {
	t.Parallel()

	baseline := goodBaseline()
	narrowed := baseline.Clone()
	delete(narrowed, verbHome)

	if problems := ResolutionErrors(baseline, narrowed); len(problems) != 0 {
		t.Errorf("the suite rejected a set that only subtracts: %v", problems)
	}
}

// A subject whose halves are not declared is refused rather than run. The
// self-declared Connectable defaulting to false reduced the whole suite to its
// baseline half and reported PASS, which is the one result a conformance suite
// must never produce for a driver it did not exercise.
func TestSubjectErrorRefusesAnUndeclaredSubject(t *testing.T) {
	t.Parallel()

	driver := fakeDriver{}

	for _, tc := range []struct {
		name    string
		subject Subject
		wantErr bool
	}{
		{"connectable with a safe verb", Subject{Driver: driver, Connectable: true, SafeVerb: verbHome}, false},
		{"connectable with no safe verb", Subject{Driver: driver, Connectable: true, NoSafeVerb: true}, false},
		{"no loopback", Subject{Driver: driver, NoLoopback: true}, false},
		{"neither half declared", Subject{Driver: driver}, true},
		{"both halves declared", Subject{Driver: driver, Connectable: true, NoLoopback: true}, true},
		{"connectable, neither verb declared", Subject{Driver: driver, Connectable: true}, true},
		{
			"connectable, both verbs declared",
			Subject{Driver: driver, Connectable: true, SafeVerb: verbHome, NoSafeVerb: true},
			true,
		},
		{"no driver", Subject{Connectable: true, SafeVerb: verbHome}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := subjectError(tc.subject)
			if tc.wantErr && err == nil {
				t.Error("the suite accepted a subject it cannot properly exercise")
			}

			if !tc.wantErr && err != nil {
				t.Errorf("the suite refused a well-declared subject: %v", err)
			}
		})
	}
}

// A resolved set that keeps a verb but drops its required argument has widened,
// however much smaller it looks: the verb may now be invoked with nothing.
func TestResolutionErrorsCatchesADroppedRequirement(t *testing.T) {
	t.Parallel()

	baseline := connectorapi.Set{
		verbPrint: {Flag: true},
		argPath:   {Flag: true, Kind: connectorapi.KindString, Required: true},
	}

	resolved := connectorapi.Set{verbPrint: {Flag: true}}

	if problems := ResolutionErrors(baseline, resolved); len(problems) == 0 {
		t.Error("the suite accepted a resolved set whose verb lost the argument its code demands")
	}

	// Subtracting the verb along with its argument is the narrowing answer and
	// must stay accepted.
	if problems := ResolutionErrors(baseline, connectorapi.Set{}); len(problems) != 0 {
		t.Errorf("the suite rejected a set that subtracted the verb outright: %v", problems)
	}
}

// The whole suite, end to end, against a driver that obeys the contract.
func TestSuitePassesAConformingDriver(t *testing.T) {
	RunDriverContract(t, func(_ *testing.T) Subject {
		return Subject{Driver: fakeDriver{}, Connectable: true, SafeVerb: verbHome}
	})
}

func goodBaseline() connectorapi.Set {
	return connectorapi.Set{
		verbHome:  {Flag: true},
		verbPrint: {Flag: true},
		argPath: {
			Flag: true, Kind: connectorapi.KindString,
			Supported: []string{"a.gcode", "b.gcode"}, Default: "a.gcode",
		},
	}
}

// fakeDriver resolves by intersection, which is what the suite checks for.
type fakeDriver struct{}

func (fakeDriver) Baseline() connectorapi.Set { return goodBaseline() }

func (d fakeDriver) Connect(
	_ context.Context, _ connectorapi.Endpoint, profile connectorapi.Set,
) (connectorapi.Session, error) {
	return &fakeSession{caps: d.Baseline().Intersect(profile), patches: make(chan connectorapi.StatePatch)}, nil
}

type fakeSession struct {
	caps    connectorapi.Set
	patches chan connectorapi.StatePatch
	closed  bool
}

func (s *fakeSession) Identity() connectorapi.Identity {
	return connectorapi.Identity{Vendor: "test", Model: "FAKE", Firmware: connectorapi.NewFirmwareLabel("1.0.0")}
}

// The set is copied out, because connectorapi.Session promises it does not move
// within an epoch and a caller handed the live map can move it.
func (s *fakeSession) Capabilities() (connectorapi.Set, uint64) { return s.caps.Clone(), 1 }

func (s *fakeSession) Do(ctx context.Context, cmd connectorapi.Command) (connectorapi.Result, error) {
	if err := ctx.Err(); err != nil {
		return connectorapi.Result{}, err
	}

	if !s.caps.Has(cmd.Verb) {
		return connectorapi.Result{}, connectorapi.CapabilityNotSupported.New("capability not supported")
	}

	return connectorapi.Result{OK: true}, nil
}

func (s *fakeSession) State() <-chan connectorapi.StatePatch { return s.patches }

func (s *fakeSession) Close() error {
	if !s.closed {
		s.closed = true

		close(s.patches)
	}

	return nil
}

// The whole suite, end to end, against a driver whose devices dial it. It is
// the same set of cases the outbound run above executes: the inbound half is
// not a reduced contract.
func TestSuitePassesAConformingInboundDriver(t *testing.T) {
	RunDriverContract(t, func(_ *testing.T) Subject {
		return Subject{Driver: inboundFakeDriver{}, Dial: dialOneDevice, SafeVerb: verbHome}
	})
}

// A driver that implements both halves is held to both, because a dialect that
// resolves differently depending on who opened the socket is a dialect whose
// printers behave differently for no reason an operator can see.
func TestSuiteRunsBothHalvesOfATwoWayDriver(t *testing.T) {
	RunDriverContract(t, func(_ *testing.T) Subject {
		return Subject{
			Driver:      twoWayFakeDriver{},
			Connectable: true,
			Dial:        dialOneDevice,
			SafeVerb:    verbHome,
		}
	})
}

func TestSubjectErrorRefusesAnUndeclaredDirection(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		subject Subject
		wantErr bool
	}{
		{"inbound with a dial", Subject{Driver: inboundFakeDriver{}, Dial: dialOneDevice, SafeVerb: verbHome}, false},
		{"inbound with no dial", Subject{Driver: inboundFakeDriver{}, SafeVerb: verbHome}, true},
		{
			"inbound claiming the outbound half",
			Subject{Driver: inboundFakeDriver{}, Dial: dialOneDevice, Connectable: true, SafeVerb: verbHome},
			true,
		},
		{
			"inbound opting out of a loopback it cannot need",
			Subject{Driver: inboundFakeDriver{}, Dial: dialOneDevice, NoLoopback: true, SafeVerb: verbHome},
			true,
		},
		{"outbound with a dial", Subject{Driver: fakeDriver{}, Connectable: true, Dial: dialOneDevice, SafeVerb: verbHome}, true},
		{"neither direction", Subject{Driver: baselineOnlyDriver{}, SafeVerb: verbHome}, true},
		{
			"both directions",
			Subject{Driver: twoWayFakeDriver{}, Connectable: true, Dial: dialOneDevice, SafeVerb: verbHome},
			false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := subjectError(tc.subject)
			if tc.wantErr && err == nil {
				t.Error("the suite accepted a subject it cannot properly exercise")
			}

			if !tc.wantErr && err != nil {
				t.Errorf("the suite refused a well-declared subject: %v", err)
			}
		})
	}
}

// A host that says no is the only thing standing between a device that dialled
// in and a printer nothing supervises.
func TestTheHostFakeRefusesADevice(t *testing.T) {
	t.Parallel()

	host := Refusing(connectorapi.ErrDeviceRefused)

	handler, err := inboundFakeDriver{}.Handler(host)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	if dialled := dialOneDevice(t, server.URL); dialled == nil {
		t.Error("a device the host refused was served anyway")
	}

	if got := len(host.Sessions()); got != 0 {
		t.Errorf("the host fake holds %d sessions for a device it refused", got)
	}
}

func TestAwaitSessionEndsWithTheContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := NewHost(goodBaseline()).AwaitSession(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("AwaitSession did not end with its context: %v", err)
	}
}

// dialOneDevice is the device half of the fake inbound dialect: it says what it
// is and the driver takes it from there.
func dialOneDevice(t *testing.T, baseURL string) error {
	t.Helper()

	body := strings.NewReader(`{"model":"FAKE","firmware":"1.0.0","serial":"sn-1"}`)

	resp, err := http.Post(baseURL+"/connect", "application/json", body)
	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the driver answered %s", resp.Status)
	}

	return nil
}

// inboundFakeDriver is the dialect this product ships, reduced to what the
// suite needs: the device opens the connection and says what it is.
type inboundFakeDriver struct{}

func (inboundFakeDriver) Baseline() connectorapi.Set { return goodBaseline() }

func (d inboundFakeDriver) Handler(arrivals connectorapi.Arrivals) (http.Handler, error) {
	return inboundHandler(arrivals, d.Baseline(), func(caps connectorapi.Set) connectorapi.Session {
		return &fakeSession{caps: caps, patches: make(chan connectorapi.StatePatch)}
	}), nil
}

// twoWayFakeDriver is dialled out to and dialled into, which a dialect that
// supports both a serial port and a network handshake really is.
type twoWayFakeDriver struct {
	fakeDriver
	inboundFakeDriver
}

func (d twoWayFakeDriver) Baseline() connectorapi.Set { return goodBaseline() }

// baselineOnlyDriver declares what it can do and gives the host no way to reach
// a device through it.
type baselineOnlyDriver struct{}

func (baselineOnlyDriver) Baseline() connectorapi.Set { return goodBaseline() }

// inboundHandler is the fake dialect's one route, shared with the violating
// drivers so that each of those breaks one clause and nothing else.
func inboundHandler(
	arrivals connectorapi.Arrivals,
	baseline connectorapi.Set,
	build func(caps connectorapi.Set) connectorapi.Session,
) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /connect", func(w http.ResponseWriter, r *http.Request) {
		var reported struct {
			Model    string `json:"model"`
			Firmware string `json:"firmware"`
			Serial   string `json:"serial"`
		}

		if err := json.NewDecoder(r.Body).Decode(&reported); err != nil {
			http.Error(w, "malformed identity", http.StatusBadRequest)

			return
		}

		identity := connectorapi.Identity{
			Vendor:   "test",
			Model:    reported.Model,
			Firmware: connectorapi.NewFirmwareLabel(reported.Firmware),
			Serial:   reported.Serial,
		}

		profile, err := arrivals.Admit(r.Context(), identity)
		if err != nil {
			http.Error(w, "refused", http.StatusForbidden)

			return
		}

		session := build(baseline.Intersect(profile))

		if taken := arrivals.Arrived(r.Context(), session); taken != nil {
			_ = session.Close()
			http.Error(w, "not taken", http.StatusServiceUnavailable)

			return
		}

		w.WriteHeader(http.StatusOK)
	})

	return mux
}
