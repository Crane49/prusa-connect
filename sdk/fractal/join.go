package fractal

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Identity is the authentication identity of a node: a value, such as a public
// key, that the node can prove it owns. The transport binding defines its form.
type Identity string

// String returns the identity unchanged.
func (i Identity) String() string { return string(i) }

// JoinCredential is the secret a node sends in its join request. The receiver
// decides from it whether the sender may join. A deployment may require none.
// The transport binding defines its form.
type JoinCredential string

// ConnectionCredentials is the authorization a node issues in a join grant. The
// node that joined connects with it. It is valid only for the authentication
// identity of that node, and it expires. The transport binding defines its
// form.
type ConnectionCredentials string

// The errors Validate returns for a join message.
var (
	// ErrMissingIdentity reports that a join request or a join grant has no
	// authentication identity.
	ErrMissingIdentity = errors.New("missing authentication identity")
	// ErrMissingCredentials reports that a join grant has no connection
	// credentials.
	ErrMissingCredentials = errors.New("missing connection credentials")
	// ErrMissingExpiry reports that a join grant does not say when its
	// connection credentials expire. Connection credentials always expire.
	ErrMissingExpiry = errors.New("missing credential expiry")
	// ErrNotAJoinDenialCode reports that the code of a join denial is not one
	// of the codes JoinDenialCodes returns, or a refinement of one.
	ErrNotAJoinDenialCode = errors.New("not a join denial code")
)

// JoinRequest is the message a node sends to the node it asks to join. The
// transport binding defines how each field is encoded and how the sender
// proves that it owns Identity.
type JoinRequest struct {
	// Version is the protocol version the sender implements. The receiver
	// denies a request of an unsupported version and does not drop it.
	Version uint
	// NodeID is the Node ID the sender asks for.
	NodeID NodeID
	// Identity is the authentication identity of the sender.
	Identity Identity
	// Credential is the join credential. It is empty where the deployment
	// requires none.
	Credential JoinCredential
}

// Validate checks the protocol version and the Node ID, and that Identity is
// not empty. It does not decide whether the sender may join. Package
// sdk/fractal/join has those checks.
func (r JoinRequest) Validate() error {
	if err := checkVersion(r.Version); err != nil {
		return err
	}
	if err := r.NodeID.Validate(); err != nil {
		return err
	}
	if r.Identity == "" {
		return ErrMissingIdentity
	}
	return nil
}

// JoinGrant is the answer to a join request that passed every check.
type JoinGrant struct {
	// Version is the protocol version the sender of the grant implements. A
	// node that receives a grant of a version it does not support does not
	// connect and does not send the request again.
	Version uint
	// NodePath is the path the sender of the grant assigns to the node that
	// joined.
	NodePath NodePath
	// ParentIdentity is the authentication identity of the sender of the
	// grant. A node that joined this sender before compares it with the
	// identity it stored, before it connects.
	ParentIdentity Identity
	// Credentials is the authorization the node that joined uses to connect
	// to the sender of the grant.
	Credentials ConnectionCredentials
	// ExpiresAt is the time at which Credentials expire. It is never zero.
	ExpiresAt Timestamp
	// Extensions is the join data of the protocol extensions the sender
	// offers, keyed by extension name. A node ignores the entry of an
	// extension it does not run.
	Extensions map[Identifier]json.RawMessage
}

// Validate checks the protocol version and the Node Path, and that
// ParentIdentity, Credentials and ExpiresAt are set.
func (g JoinGrant) Validate() error {
	if err := checkVersion(g.Version); err != nil {
		return err
	}
	if err := g.NodePath.Validate(); err != nil {
		return err
	}
	if g.ParentIdentity == "" {
		return ErrMissingIdentity
	}
	if g.Credentials == "" {
		return ErrMissingCredentials
	}
	if g.ExpiresAt == 0 {
		return ErrMissingExpiry
	}
	return nil
}

// JoinDenial is the answer to a join request that failed a check. Its code is
// for the first check that failed. JoinRetry says, from the code, whether the
// node that asked sends the request again.
type JoinDenial struct {
	// Version is the protocol version the sender of the denial implements.
	Version uint
	// Code says which check failed.
	Code Code
	// Reason is free text for a person and may be empty.
	Reason string
}

// Validate checks the protocol version, and that IsJoinDenialCode is true for
// the code.
func (d JoinDenial) Validate() error {
	if err := checkVersion(d.Version); err != nil {
		return err
	}
	if !IsJoinDenialCode(d.Code) {
		return fmt.Errorf("code %s: %w", d.Code, ErrNotAJoinDenialCode)
	}
	return nil
}

// Retry says whether a node sends its join request again after a denial, and
// when.
type Retry uint8

// The retry rules.
const (
	// RetryNever means the node does not send the request again until an
	// operator changes something.
	RetryNever Retry = iota + 1
	// RetryWithNewCredential means the node sends the request again after it
	// has obtained a valid join credential.
	RetryWithNewCredential
	// RetryWithBackoff means the node sends the request again and waits longer
	// after each denial.
	RetryWithBackoff
)

// String returns "never", "with a new credential", "with backoff" or
// "unknown".
func (r Retry) String() string {
	switch r {
	case RetryNever:
		return "never"
	case RetryWithNewCredential:
		return "with a new credential"
	case RetryWithBackoff:
		return "with backoff"
	}
	return "unknown"
}

// retryRow pairs one join denial code with its retry rule.
type retryRow struct {
	code  Code
	retry Retry
}

// joinRetry lists the retry rule of each join denial code.
var joinRetry = [...]retryRow{
	{CodeJoinRejected, RetryNever},
	{CodeJoinCollision, RetryNever},
	{CodeJoinRevoked, RetryNever},
	{CodeJoinUnauthorized, RetryWithNewCredential},
	{CodeJoinUnavailable, RetryWithBackoff},
}

// JoinRetry returns the retry rule for a join denial with the code c. A
// refinement of a join denial code has the rule of its base code. ok is false
// when c is not a join denial code. A node then treats the denial like a
// request that got no answer.
func JoinRetry(c Code) (r Retry, ok bool) {
	for _, row := range joinRetry {
		if c.Is(row.code) {
			return row.retry, true
		}
	}
	return 0, false
}

// IsJoinDenialCode reports whether c is one of the codes JoinDenialCodes
// returns, or a refinement of one.
func IsJoinDenialCode(c Code) bool {
	_, ok := JoinRetry(c)
	return ok
}

// JoinDenialCodes returns every code that is allowed in a join denial. The
// caller owns the returned slice.
func JoinDenialCodes() []Code {
	out := make([]Code, 0, len(joinRetry))
	for _, row := range joinRetry {
		out = append(out, row.code)
	}
	return out
}
