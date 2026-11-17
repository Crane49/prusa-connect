package join

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// parentPath is the Node Path of the Parent under test.
const parentPath fractal.NodePath = "hq/brno"

// credentialLife is how long the credentials countingIssuer issues are valid.
const credentialLife = time.Hour

// wireRequest is the JSON form of a join request in these tests. The member
// names are the ones fractal-nats.md gives the body of the signed envelope.
type wireRequest struct {
	Version    uint   `json:"version"`
	NodeID     string `json:"nodeId"`
	Identity   string `json:"nkey"`
	Credential string `json:"joinToken,omitempty"`
}

// jsonVerifier is a ProofVerifier that decodes a request from JSON and checks
// no signature. A test sets err to make Verify fail the way a real binding
// fails on a signature that is not valid.
type jsonVerifier struct {
	err error
}

// Verify decodes the envelope.
func (v jsonVerifier) Verify(_ context.Context, body []byte) (fractal.JoinRequest, error) {
	if v.err != nil {
		return fractal.JoinRequest{}, v.err
	}
	var w wireRequest
	if err := json.Unmarshal(body, &w); err != nil {
		return fractal.JoinRequest{}, err
	}
	return fractal.JoinRequest{
		Version:    w.Version,
		NodeID:     fractal.NodeID(w.NodeID),
		Identity:   fractal.Identity(w.Identity),
		Credential: fractal.JoinCredential(w.Credential),
	}, nil
}

// encode returns the JSON bytes jsonVerifier reads. It ignores the error of
// json.Marshal, because a struct of strings and a number always encodes.
func encode(r fractal.JoinRequest) []byte {
	data, _ := json.Marshal(wireRequest{
		Version:    r.Version,
		NodeID:     string(r.NodeID),
		Identity:   string(r.Identity),
		Credential: string(r.Credential),
	})
	return data
}

// countingIssuer is an Issuer whose credential is the identity, a "#" and the
// number of the call. It counts its calls, and it records the identity and the
// Node Path of each call by Node ID.
type countingIssuer struct {
	calls atomic.Int64
	err   error

	mu        sync.Mutex
	boundTo   map[fractal.NodeID]fractal.Identity
	boundPath map[fractal.NodeID]fractal.NodePath
}

// Issue returns the credential, or err when the test set it.
func (i *countingIssuer) Issue(
	_ context.Context,
	id fractal.NodeID,
	identity fractal.Identity,
	path fractal.NodePath,
) (fractal.ConnectionCredentials, fractal.Timestamp, error) {
	n := i.calls.Add(1)
	if i.err != nil {
		return "", 0, i.err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.boundTo == nil {
		i.boundTo = map[fractal.NodeID]fractal.Identity{}
		i.boundPath = map[fractal.NodeID]fractal.NodePath{}
	}
	i.boundTo[id] = identity
	i.boundPath[id] = path
	credential := fractal.ConnectionCredentials(string(identity) + "#" + strconv.FormatInt(n, 10))
	return credential, fractal.TimestampOf(time.Now().Add(credentialLife)), nil
}

// bound returns the identity of the last Issue call for id.
func (i *countingIssuer) bound(id fractal.NodeID) fractal.Identity {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.boundTo[id]
}

// gate holds one Issue call. The call closes entered, waits until the test
// closes release, and then fails with err when the test set it.
type gate struct {
	entered chan struct{}
	release chan struct{}
	err     error
}

// newGate returns a gate whose call fails with err, or continues when err is
// nil.
func newGate(err error) *gate {
	return &gate{entered: make(chan struct{}), release: make(chan struct{}), err: err}
}

// gatedIssuer is a countingIssuer that holds its calls at gates. Each call
// takes the next gate from gates. A call that finds no gate goes straight
// through.
type gatedIssuer struct {
	countingIssuer

	gates chan *gate
}

// newGatedIssuer returns a gatedIssuer whose first calls take these gates, in
// this order.
func newGatedIssuer(gates ...*gate) *gatedIssuer {
	g := &gatedIssuer{gates: make(chan *gate, len(gates))}
	for _, held := range gates {
		g.gates <- held
	}
	return g
}

// Issue holds the call at the next gate, if there is one.
func (g *gatedIssuer) Issue(
	ctx context.Context,
	id fractal.NodeID,
	identity fractal.Identity,
	path fractal.NodePath,
) (fractal.ConnectionCredentials, fractal.Timestamp, error) {
	select {
	case held := <-g.gates:
		close(held.entered)
		<-held.release
		if held.err != nil {
			return "", 0, held.err
		}
	default:
	}
	return g.countingIssuer.Issue(ctx, id, identity, path)
}

// recordingHook is a RevocationHook that records every admission it receives.
type recordingHook struct {
	mu      sync.Mutex
	revoked []Admission
	ran     int
	err     error
}

// Revoke records the admission.
func (h *recordingHook) Revoke(_ context.Context, a Admission) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ran++
	if h.err != nil {
		return h.err
	}
	h.revoked = append(h.revoked, a)
	return nil
}

// calls returns how many times the Parent has called the hook, failures
// included.
func (h *recordingHook) calls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ran
}

// seen returns how many admissions the hook has recorded.
func (h *recordingHook) seen() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.revoked)
}

// fixture is a Parent with the issuer, the store and the hook it was built
// with, so a test can read them.
type fixture struct {
	parent *Parent
	issuer *countingIssuer
	store  *MemoryAdmissionStore
	hook   *recordingHook
}

// newFixture builds a Parent with the Node Path parentPath, the TOFU
// authorizer and a MemoryAdmissionStore. Each option changes the ParentConfig
// before NewParent runs.
func newFixture(t *testing.T, options ...func(*ParentConfig)) *fixture {
	t.Helper()
	f := &fixture{
		issuer: &countingIssuer{},
		store:  &MemoryAdmissionStore{},
		hook:   &recordingHook{},
	}
	cfg := ParentConfig{
		Identity:   "parent-key",
		Path:       parentPath,
		Store:      f.store,
		Verifier:   jsonVerifier{},
		Authorizer: TOFU{},
		Issuer:     f.issuer,
		Revocation: f.hook,
	}
	for _, option := range options {
		option(&cfg)
	}
	p, err := NewParent(t.Context(), cfg)
	if err != nil {
		t.Fatalf("build the parent: %v", err)
	}
	f.parent = p
	return f
}

// envelope returns the request as the JSON bytes jsonVerifier reads.
func envelope(t *testing.T, r fractal.JoinRequest) []byte {
	t.Helper()
	return encode(r)
}

// request builds a well-formed request of the current protocol version.
func request(id fractal.NodeID, identity fractal.Identity) fractal.JoinRequest {
	return fractal.JoinRequest{Version: fractal.Version, NodeID: id, Identity: identity}
}

// join passes one request to Evaluate and returns the Outcome.
func (f *fixture) join(t *testing.T, r fractal.JoinRequest) Outcome {
	t.Helper()
	return f.parent.Evaluate(t.Context(), envelope(t, r))
}

// mustGrant evaluates one request and fails the test when it was denied.
func (f *fixture) mustGrant(t *testing.T, r fractal.JoinRequest) fractal.JoinGrant {
	t.Helper()
	out := f.join(t, r)
	if !out.Granted {
		t.Fatalf("the request was denied at %q with %s: %v", out.Check, out.Denial.Code, out.Cause)
	}
	return out.Grant
}

// mustDeny evaluates one request and fails the test unless it was denied with
// the code want.
func (f *fixture) mustDeny(t *testing.T, r fractal.JoinRequest, want fractal.Code) Outcome {
	t.Helper()
	out := f.join(t, r)
	if out.Granted {
		t.Fatalf("the request was granted, wanted a denial of %s", want)
	}
	if out.Denial.Code != want {
		t.Fatalf("denied at %q with %s, wanted %s: %v", out.Check, out.Denial.Code, want, out.Cause)
	}
	if err := out.Denial.Validate(); err != nil {
		t.Fatalf("the denial does not pass JoinDenial.Validate: %v", err)
	}
	return out
}

// refusingAuthorizer denies every request with an error that wraps
// ErrUnauthorized, as an Authorizer does for a bad join credential.
func refusingAuthorizer() Authorizer {
	return AuthorizerFunc(func(context.Context, fractal.JoinRequest, fractal.NodePath) error {
		return fmt.Errorf("the token names another node path: %w", ErrUnauthorized)
	})
}

// switchableAuthorizer accepts every request until a test sets refuse.
type switchableAuthorizer struct {
	refuse atomic.Bool
}

// Authorize accepts the request unless refuse is set.
func (s *switchableAuthorizer) Authorize(context.Context, fractal.JoinRequest, fractal.NodePath) error {
	if s.refuse.Load() {
		return fmt.Errorf("the token names another node path: %w", ErrUnauthorized)
	}
	return nil
}

// brokenAuthorizer returns an error that does not wrap ErrUnauthorized, as an
// Authorizer does when it cannot reach its key source.
func brokenAuthorizer() Authorizer {
	return AuthorizerFunc(func(context.Context, fractal.JoinRequest, fractal.NodePath) error {
		return errors.New("the key source is unreachable")
	})
}

// fixedClock is a Clock whose time changes only when a test calls advance.
type fixedClock struct {
	mu  sync.Mutex
	now time.Time
}

// Now returns the time the test set.
func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After returns a channel that never receives. A Parent calls only Now.
func (c *fixedClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }

// advance moves the clock forward.
func (c *fixedClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// gatedStore is a MemoryAdmissionStore that holds the first save of an
// admission with EverConnected set at a gate.
type gatedStore struct {
	MemoryAdmissionStore

	held *gate
	once sync.Once
}

// SaveAdmission holds the first save of a connected admission, then saves.
func (s *gatedStore) SaveAdmission(ctx context.Context, a Admission) error {
	if a.EverConnected {
		s.once.Do(func() {
			close(s.held.entered)
			<-s.held.release
		})
	}
	return s.MemoryAdmissionStore.SaveAdmission(ctx, a)
}
