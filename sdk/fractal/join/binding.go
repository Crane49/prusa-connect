package join

import (
	"context"
	"errors"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// Reasons a Parent denies a join request or refuses a call. A Parent puts the
// reason in Outcome.Cause, or returns it from Revoke and MarkConnected. Match
// each with errors.Is.
var (
	// ErrNoNodePath means this Parent has no Node Path of its own, so it
	// denies every request.
	ErrNoNodePath = errors.New("this node has no node path of its own")
	// ErrUnauthorized means the Authorizer refused the join credential. An
	// Authorizer wraps it when the credential is missing, invalid or expired,
	// or does not match the request.
	ErrUnauthorized = errors.New("the join credential was refused")
	// ErrRevoked means an operator has revoked the admission of the requested
	// Node ID.
	ErrRevoked = errors.New("the admission has been revoked")
	// ErrNodeIDHeld means another authentication identity already owns the
	// requested Node ID.
	ErrNodeIDHeld = errors.New("another identity holds this node id")
	// ErrIdentityHolds means the identity in the request already owns a
	// different Node ID under this Parent.
	ErrIdentityHolds = errors.New("this identity already holds another node id")
	// ErrTooManyJoins means the request went over ParentConfig.MaxJoinsInFlight
	// or ParentConfig.JoinsPerSecond.
	ErrTooManyJoins = errors.New("too many joins at once")
	// ErrUnknownChild means Revoke or MarkConnected got a Node ID the Parent
	// has not admitted.
	ErrUnknownChild = errors.New("no admission for this node id")
)

// ProofVerifier decodes the bytes of a join request and checks the signature
// that proves the sender has the private key of the identity in the request.
// Verify must return the request only when that signature is valid. The
// Parent acts on no field of a request before Verify returns it. On an error
// the Parent answers CodeJoinRejected.
type ProofVerifier interface {
	Verify(ctx context.Context, envelope []byte) (fractal.JoinRequest, error)
}

// VerifierFunc lets a plain function serve as a ProofVerifier.
type VerifierFunc func(ctx context.Context, envelope []byte) (fractal.JoinRequest, error)

// Verify calls f.
func (f VerifierFunc) Verify(ctx context.Context, envelope []byte) (fractal.JoinRequest, error) {
	return f(ctx, envelope)
}

// Authorizer decides whether a node may join under the Node ID it asks for.
// The parent argument is the Node Path of the Parent.
//
// A nil error authorizes the request. An error that wraps ErrUnauthorized
// denies it with CodeJoinUnauthorized. Any other error means the check could
// not be done, and the Parent answers CodeJoinUnavailable. Return a
// *CodeError to send a more specific code.
type Authorizer interface {
	Authorize(ctx context.Context, req fractal.JoinRequest, parent fractal.NodePath) error
}

// CodeError is an error from an Authorizer that names the denial code to send.
//
// The Parent first picks CodeJoinUnauthorized or CodeJoinUnavailable from Err.
// It sends Code only when Code is a valid refinement of the code it picked.
// Otherwise it sends the code it picked.
type CodeError struct {
	// Err is the reason. To deny the request, wrap ErrUnauthorized here. The
	// Parent reads Err to tell a denial from a check that could not be done.
	Err error
	// Code is the denial code to send.
	Code fractal.Code
}

// Error returns the code, followed by the reason when there is one.
func (e *CodeError) Error() string {
	if e.Err == nil {
		return e.Code.String()
	}
	return e.Code.String() + ": " + e.Err.Error()
}

// Unwrap returns Err, so errors.Is finds a wrapped ErrUnauthorized.
func (e *CodeError) Unwrap() error { return e.Err }

// AuthorizerFunc lets a plain function serve as an Authorizer.
type AuthorizerFunc func(ctx context.Context, req fractal.JoinRequest, parent fractal.NodePath) error

// Authorize calls f.
func (f AuthorizerFunc) Authorize(ctx context.Context, req fractal.JoinRequest, parent fractal.NodePath) error {
	return f(ctx, req, parent)
}

// TOFU is an Authorizer that authorizes every request (trust on first use). A
// node needs no join credential. The signature on a request proves that the
// sender has a key, not who the sender is. Use TOFU only where no machine
// other than the intended nodes can reach the join endpoint.
type TOFU struct{}

// Authorize accepts the request.
func (TOFU) Authorize(context.Context, fractal.JoinRequest, fractal.NodePath) error { return nil }

// Issuer creates the connection credentials that a grant contains. The Parent
// calls Issue only after every check has passed.
//
// Issue returns the credentials and the time they expire. The credentials
// must work only for the given identity, and they must expire.
type Issuer interface {
	Issue(
		ctx context.Context,
		id fractal.NodeID,
		identity fractal.Identity,
		path fractal.NodePath,
	) (fractal.ConnectionCredentials, fractal.Timestamp, error)
}

// RevocationHook stops the connection credentials of a revoked node from
// working. Parent.Revoke calls it. A revoked node must lose access on the
// connection it already has, not only when it next connects.
type RevocationHook interface {
	Revoke(ctx context.Context, a Admission) error
}

// RevocationFunc lets a plain function serve as a RevocationHook.
type RevocationFunc func(ctx context.Context, a Admission) error

// Revoke calls f.
func (f RevocationFunc) Revoke(ctx context.Context, a Admission) error { return f(ctx, a) }

// Clock is where a Parent and a Joiner get the time and their timers. A test
// passes a clock it controls. A program uses SystemClock.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// After returns a channel that receives a value after d has passed.
	After(d time.Duration) <-chan time.Time
}

// SystemClock is the Clock that calls time.Now and time.After.
type SystemClock struct{}

// Now returns the current time.
func (SystemClock) Now() time.Time { return time.Now() }

// After returns a channel that receives once d has passed.
func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
