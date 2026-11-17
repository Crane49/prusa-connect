package join

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// The values NewJoiner uses for JoinerConfig.MinBackoff, MaxBackoff and
// RenewFraction when they are not set.
const (
	defaultMinBackoff    = time.Second
	defaultMaxBackoff    = time.Minute
	defaultRenewFraction = 0.75
)

var (
	// ErrNoCredentialSource is the error Join returns when the channel
	// JoinerConfig.Credentials is closed while the Joiner waits on it for a
	// new join credential.
	ErrNoCredentialSource = errors.New("no source of join credentials")
	// ErrExtensionRefused is the error Join returns when
	// JoinerConfig.CheckExtensions refused a grant. It wraps the error of the
	// check.
	ErrExtensionRefused = errors.New("an extension refused the grant")
)

// Reply is the answer of the node above to one join request, as a Requester
// decoded it.
type Reply struct {
	// Grant is the grant. It is set when Granted is true.
	Grant fractal.JoinGrant
	// Denial is the denial. It is set when Granted is false. Its code decides
	// whether the Joiner tries again.
	Denial fractal.JoinDenial
	// Extra is data the transport binding sent beside the grant, as bytes
	// only the binding reads. Decide copies it into Membership.Binding.
	Extra json.RawMessage
	// Granted is true for a grant and false for a denial.
	Granted bool
}

// Requester sends one join request to the node above and returns the answer.
// The transport binding implements it: it signs the request with the key of
// this node, sends it and decodes the answer. Request must return an error
// when no answer arrives. The Joiner then retries after a backoff.
type Requester interface {
	Request(ctx context.Context, req fractal.JoinRequest) (Reply, error)
}

// RequesterFunc lets a plain function serve as a Requester.
type RequesterFunc func(ctx context.Context, req fractal.JoinRequest) (Reply, error)

// Request calls f.
func (f RequesterFunc) Request(ctx context.Context, req fractal.JoinRequest) (Reply, error) {
	return f(ctx, req)
}

// DenialError is the error Join returns when the node above denied the
// request and the Joiner does not try again.
type DenialError struct {
	// Denial is the denial the node above sent.
	Denial fractal.JoinDenial
}

// Error returns the denial code, followed by the reason text of the denial
// when there is one.
func (e *DenialError) Error() string {
	if e.Denial.Reason == "" {
		return "the node above denied the join request: " + e.Denial.Code.String()
	}
	return "the node above denied the join request: " + e.Denial.Code.String() + ": " + e.Denial.Reason
}

// JoinerConfig configures a Joiner. Requester, Store, NodeID and Identity are
// required.
type JoinerConfig struct {
	// Requester sends each join request and returns the answer.
	Requester Requester
	// Store keeps the Membership, so the node has it again after a restart.
	Store MembershipStore
	// NodeID is the Node ID this node asks for.
	NodeID fractal.NodeID
	// Identity is the authentication identity of this node.
	Identity fractal.Identity
	// Credential is the join credential the first request contains. Leave it
	// empty where the deployment requires none.
	Credential fractal.JoinCredential
	// Endpoint is the address this node sends its join request to.
	Endpoint Endpoint
	// HasChildren reports whether this node has admitted at least one node
	// below it. Such a node refuses a grant from a node with a different
	// identity than the one it joined before. Nil means it has admitted none.
	HasChildren func() bool
	// Credentials is a channel the application sends a new join credential
	// on. After CodeJoinUnauthorized the Joiner waits for a value and sends
	// the request again with it. When nil, Join returns a DenialError.
	Credentials <-chan fractal.JoinCredential
	// Retry chooses how the Joiner retries after a denial code. When it is
	// nil or returns false, the Joiner uses fractal.JoinRetry. A transport
	// binding sets it for a refinement whose base-code rule does not fit.
	Retry func(c fractal.Code) (fractal.Retry, bool)
	// OnGrant is called after the Joiner saved the Membership of a grant it
	// accepted, the first one and every renewal. Another Join waits until it
	// returns, so it must not call Join. It may be nil.
	OnGrant func(ctx context.Context, d Decision)
	// Extensions are the names of the protocol extensions this node runs.
	// The Joiner keeps their join data from a grant in
	// Membership.Extensions and ignores the rest.
	Extensions []fractal.Identifier
	// CheckExtensions gets the join data the Joiner kept from a grant, before
	// it saves the Membership and before OnGrant. An error refuses the grant:
	// nothing is saved and Join returns the error wrapped in
	// ErrExtensionRefused. Nil accepts every grant.
	CheckExtensions func(map[fractal.Identifier]json.RawMessage) error
	// Clock gives the time for the backoff and for the renewal. It defaults
	// to SystemClock.
	Clock Clock
	// Rand returns a random number in [0, 1). The Joiner multiplies each
	// backoff time by it. It defaults to math/rand/v2.Float64.
	Rand func() float64
	// MinBackoff is the longest the Joiner waits before its first retry. The
	// limit doubles with each retry. It defaults to one second.
	MinBackoff time.Duration
	// MaxBackoff is the highest value that limit reaches. It defaults to one
	// minute.
	MaxBackoff time.Duration
	// RenewFraction is the part of the remaining lifetime of the connection
	// credentials that Run waits before it runs the exchange again. It must
	// be above zero and at most one. It defaults to 0.75.
	RenewFraction float64
}

// Joiner runs the join exchange for the node that joins. It sends the first
// request, retries as the denial code requires, and runs the exchange again
// before the connection credentials expire.
type Joiner struct {
	cfg JoinerConfig

	mu         sync.Mutex
	credential fractal.JoinCredential

	// exchanging has a value from a join request until the grant it brought
	// is decided, so one Join at a time sends a request.
	exchanging chan struct{}
	// refused is the refusal of a grant this Joiner refused. It is read and
	// written only while exchanging has a value.
	refused error
}

// NewJoiner checks the required fields of cfg, fills in the defaults and
// returns a Joiner.
func NewJoiner(cfg JoinerConfig) (*Joiner, error) {
	switch {
	case cfg.Requester == nil:
		return nil, errors.New("joiner config: no requester")
	case cfg.Store == nil:
		return nil, errors.New("joiner config: no membership store")
	case cfg.Identity == "":
		return nil, fmt.Errorf("joiner config: %w", fractal.ErrMissingIdentity)
	}
	if err := cfg.NodeID.Validate(); err != nil {
		return nil, fmt.Errorf("joiner config: %w", err)
	}
	if cfg.Clock == nil {
		cfg.Clock = SystemClock{}
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Float64
	}
	if cfg.MinBackoff <= 0 {
		cfg.MinBackoff = defaultMinBackoff
	}
	if cfg.MaxBackoff < cfg.MinBackoff {
		cfg.MaxBackoff = max(defaultMaxBackoff, cfg.MinBackoff)
	}
	if cfg.RenewFraction <= 0 || cfg.RenewFraction > 1 {
		cfg.RenewFraction = defaultRenewFraction
	}
	return &Joiner{cfg: cfg, credential: cfg.Credential, exchanging: make(chan struct{}, 1)}, nil
}

// Join sends the join request, with retries, until a grant arrives, a denial
// arrives that is not retried (a *DenialError), or the context ends. An
// accepted grant is saved and its Membership returned. A refused grant
// returns Decision.Reason.
//
// A node that refused a grant must not retry. Every later Join returns that
// refusal and sends no request. One Join at a time sends a request.
func (j *Joiner) Join(ctx context.Context) (Membership, error) {
	for attempt := 0; ; {
		select {
		case j.exchanging <- struct{}{}:
		case <-ctx.Done():
			return Membership{}, ctx.Err()
		}
		if j.refused != nil {
			<-j.exchanging
			return Membership{}, j.refused
		}
		reply, err := j.cfg.Requester.Request(ctx, j.request())
		if err == nil && reply.Granted {
			held, acceptErr := j.accept(ctx, reply)
			<-j.exchanging
			return held, acceptErr
		}
		<-j.exchanging
		switch {
		case err != nil:
			// The request got no answer. The Joiner waits for the backoff
			// time and sends it again.
			if waitErr := j.wait(ctx, attempt); waitErr != nil {
				return Membership{}, errors.Join(err, waitErr)
			}
			attempt++
		default:
			again, waitErr := j.afterDenial(ctx, reply.Denial, attempt)
			if waitErr != nil {
				return Membership{}, waitErr
			}
			if !again {
				return Membership{}, &DenialError{Denial: reply.Denial}
			}
			attempt++
		}
	}
}

// Run calls Join, then calls it again each time RenewFraction of the remaining
// lifetime of the connection credentials has passed. It returns when the
// context ends, or with the error of the first Join that fails.
func (j *Joiner) Run(ctx context.Context) error {
	m, err := j.Join(ctx)
	if err != nil {
		return err
	}
	return j.Renew(ctx, m)
}

// Renew is Run for a node that already has a Membership. It waits until
// RenewFraction of the remaining lifetime of the credentials in held has
// passed, calls Join, and repeats. A zero held, or one whose credentials
// have expired, joins at once.
func (j *Joiner) Renew(ctx context.Context, held Membership) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-j.cfg.Clock.After(j.renewIn(held.ExpiresAt)):
		}
		var err error
		if held, err = j.Join(ctx); err != nil {
			return err
		}
	}
}

// Credential returns the join credential the Joiner puts in its next request.
func (j *Joiner) Credential() fractal.JoinCredential {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.credential
}

// request builds the next join request.
func (j *Joiner) request() fractal.JoinRequest {
	return fractal.JoinRequest{
		Version:    fractal.Version,
		NodeID:     j.cfg.NodeID,
		Identity:   j.cfg.Identity,
		Credential: j.Credential(),
	}
}

// accept runs Decide and CheckExtensions on a grant, then saves the Membership
// and calls OnGrant. It keeps a refusal in j.refused.
func (j *Joiner) accept(ctx context.Context, reply Reply) (Membership, error) {
	held, joined, err := j.cfg.Store.LoadMembership(ctx)
	if err != nil {
		return Membership{}, fmt.Errorf("read the membership: %w", err)
	}
	d := Decide(Situation{
		Held:        held,
		Joined:      joined,
		Grant:       reply.Grant,
		Endpoint:    j.cfg.Endpoint,
		HasChildren: j.cfg.HasChildren != nil && j.cfg.HasChildren(),
		Binding:     reply.Extra,
		Extensions:  j.cfg.Extensions,
	})
	if d.Action == ActionRefuse {
		j.refused = d.Reason
		return Membership{}, d.Reason
	}
	if j.cfg.CheckExtensions != nil {
		if checkErr := j.cfg.CheckExtensions(d.Membership.Extensions); checkErr != nil {
			j.refused = fmt.Errorf("%w: %w", ErrExtensionRefused, checkErr)
			return Membership{}, j.refused
		}
	}
	if saveErr := j.cfg.Store.SaveMembership(ctx, d.Membership); saveErr != nil {
		return Membership{}, fmt.Errorf("write the membership: %w", saveErr)
	}
	if j.cfg.OnGrant != nil {
		j.cfg.OnGrant(ctx, d)
	}
	return d.Membership, nil
}

// afterDenial waits as the retry rule of the denial code requires. again is
// true when Join sends the request again.
func (j *Joiner) afterDenial(ctx context.Context, d fractal.JoinDenial, attempt int) (again bool, err error) {
	retry, known := j.retryFor(d.Code)
	if !known {
		// The Joiner handles a code the protocol does not define as it
		// handles a request with no answer.
		retry = fractal.RetryWithBackoff
	}
	switch retry {
	case fractal.RetryNever:
		return false, nil
	case fractal.RetryWithNewCredential:
		if j.cfg.Credentials == nil {
			return false, nil
		}
		return true, j.waitCredential(ctx)
	case fractal.RetryWithBackoff:
		return true, j.wait(ctx, attempt)
	}
	return false, nil
}

// retryFor returns the retry rule for a denial code. JoinerConfig.Retry comes
// before fractal.JoinRetry.
func (j *Joiner) retryFor(c fractal.Code) (fractal.Retry, bool) {
	if j.cfg.Retry != nil {
		if retry, ok := j.cfg.Retry(c); ok {
			return retry, true
		}
	}
	return fractal.JoinRetry(c)
}

// waitCredential blocks until a new join credential arrives on the Credentials
// channel, and keeps it for the next request.
func (j *Joiner) waitCredential(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c, open := <-j.cfg.Credentials:
		if !open {
			return ErrNoCredentialSource
		}
		j.mu.Lock()
		j.credential = c
		j.mu.Unlock()
		return nil
	}
}

// wait blocks for the backoff time of this attempt, or until the context ends.
func (j *Joiner) wait(ctx context.Context, attempt int) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-j.cfg.Clock.After(j.backoff(attempt)):
		return nil
	}
}

// backoff returns a random time between zero and a limit. The limit is
// MinBackoff doubled once per attempt, and at most MaxBackoff.
func (j *Joiner) backoff(attempt int) time.Duration {
	ceiling := float64(j.cfg.MaxBackoff)
	d := float64(j.cfg.MinBackoff) * math.Exp2(float64(attempt))
	if math.IsInf(d, 1) || d > ceiling {
		d = ceiling
	}
	return time.Duration(j.cfg.Rand() * d)
}

// renewIn returns how long Run waits before it runs the exchange again, given
// the expiry time of the credentials. It returns zero when they have expired.
func (j *Joiner) renewIn(expires fractal.Timestamp) time.Duration {
	remaining := expires.Time().Sub(j.cfg.Clock.Now())
	if remaining <= 0 {
		return 0
	}
	return time.Duration(float64(remaining) * j.cfg.RenewFraction)
}
