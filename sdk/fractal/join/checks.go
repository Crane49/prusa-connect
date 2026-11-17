package join

import (
	"context"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// step is one check a Parent runs on a join request. Evaluate runs the steps
// table in order and answers with the code of the first step that fails.
type step struct {
	// Name is the name of the check. It goes to Outcome.Check.
	Name string
	// Run returns the denial code, or the zero code when the check passes.
	Run func(ctx context.Context, ev *evaluation) (fractal.Code, error)
}

// steps lists the checks in the order the protocol requires. checkVersion
// calls Verify. When Verify fails it passes, and checkProof denies.
var steps = [...]step{
	{"parent's own node path", checkOwnPath},
	{"protocol version", checkVersion},
	{"well-formedness and proof of identity", checkProof},
	{"node id", checkNodeID},
	{"node path", checkNodePath},
	{"authorization", checkAuthorization},
	{"admission and uniqueness", checkAdmission},
}

// evaluation is the data the checks collect about one join request.
type evaluation struct {
	parent   *Parent
	envelope []byte

	verified  bool
	request   fractal.JoinRequest
	verifyErr error

	// ownPath is the Node Path of this Parent. childPath is the Node Path the
	// joining node gets: ownPath, a slash and the requested Node ID.
	ownPath   fractal.NodePath
	childPath fractal.NodePath
	// claimed is true while this request has the Node ID and the identity
	// reserved in the Parent. release ends the reservation.
	claimed bool
}

// verify calls the ProofVerifier, once per request. No code reads the envelope
// before this call.
func (ev *evaluation) verify(ctx context.Context) {
	if ev.verified {
		return
	}
	ev.verified = true
	ev.request, ev.verifyErr = ev.parent.verifier.Verify(ctx, ev.envelope)
	if ev.verifyErr != nil {
		ev.request = fractal.JoinRequest{}
	}
}

// release ends the share of this request in the reservation when the request
// did not end in a grant.
func (ev *evaluation) release() {
	if !ev.claimed {
		return
	}
	p := ev.parent
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unclaim(ev.request.NodeID, ev.request.Identity)
	ev.claimed = false
}

// unclaim ends the share of one request in the reservation of id by identity.
// The caller must hold the lock.
func (p *Parent) unclaim(id fractal.NodeID, identity fractal.Identity) {
	if p.claimed[id] != identity {
		return
	}
	p.claimants[id]--
	if p.claimants[id] > 0 {
		return
	}
	delete(p.claimants, id)
	delete(p.claimed, id)
	delete(p.claimedBy, identity)
}

// checkOwnPath is check 1: a node that has no Node Path of its own denies
// every join request.
func checkOwnPath(_ context.Context, ev *evaluation) (fractal.Code, error) {
	ev.ownPath = ev.parent.Path()
	if ev.ownPath == "" {
		return fractal.CodeJoinUnavailable, ErrNoNodePath
	}
	return 0, nil
}

// checkVersion is check 2: the Parent must support the protocol version of the
// request. It denies a request of another version and does not drop it.
func checkVersion(ctx context.Context, ev *evaluation) (fractal.Code, error) {
	ev.verify(ctx)
	if ev.verifyErr != nil {
		// Verify failed, so there is no version to read. checkProof denies the
		// request with the error from Verify.
		return 0, nil //nolint:nilerr // checkProof, the next step, reports this error.
	}
	if !fractal.SupportedVersion(ev.request.Version) {
		return fractal.CodeJoinRejected,
			fmt.Errorf("version %d: %w", ev.request.Version, fractal.ErrUnsupportedVersion)
	}
	return 0, nil
}

// checkProof is check 3: the request must parse and its signature must be
// valid for the identity in the request.
func checkProof(ctx context.Context, ev *evaluation) (fractal.Code, error) {
	ev.verify(ctx)
	if ev.verifyErr != nil {
		return fractal.CodeJoinRejected, ev.verifyErr
	}
	if ev.request.Identity == "" {
		return fractal.CodeJoinRejected, fractal.ErrMissingIdentity
	}
	return 0, nil
}

// checkNodeID is check 4: the requested Node ID must conform to the Node ID
// definition.
func checkNodeID(_ context.Context, ev *evaluation) (fractal.Code, error) {
	if err := ev.request.NodeID.Validate(); err != nil {
		return fractal.CodeJoinRejected, err
	}
	return 0, nil
}

// checkNodePath is check 5: the path of this Parent, a slash and the requested
// Node ID must not be longer than a Node Path may be.
func checkNodePath(_ context.Context, ev *evaluation) (fractal.Code, error) {
	child, err := ev.ownPath.Child(ev.request.NodeID)
	if err != nil {
		return fractal.CodeJoinRejected, err
	}
	ev.childPath = child
	return 0, nil
}

// checkAuthorization is check 6: the Authorizer checks the join credential. It
// is skipped when the identity in the request already owns the Node ID.
func checkAuthorization(ctx context.Context, ev *evaluation) (fractal.Code, error) {
	if ev.parent.holds(ev.request.NodeID, ev.request.Identity) {
		return 0, nil
	}
	err := ev.parent.authorizer.Authorize(ctx, ev.request, ev.ownPath)
	switch {
	case err == nil:
		return 0, nil
	case errors.Is(err, ErrUnauthorized):
		return refined(fractal.CodeJoinUnauthorized, err), err
	default:
		return refined(fractal.CodeJoinUnavailable, err),
			fmt.Errorf("the authorization check could not run: %w", err)
	}
}

// refined returns the Code of a CodeError in err when it is a valid refinement
// of base, and base otherwise.
func refined(base fractal.Code, err error) fractal.Code {
	var carried *CodeError
	if !errors.As(err, &carried) {
		return base
	}
	if !carried.Code.Is(base) || !carried.Code.Valid() {
		return base
	}
	return carried.Code
}

// checkAdmission is check 7: the admission of the requested Node ID must not
// be revoked, and no other identity may own that Node ID under this Parent.
func checkAdmission(_ context.Context, ev *evaluation) (fractal.Code, error) {
	return ev.parent.claim(ev)
}

// claim applies the uniqueness rules. For a Node ID with no admission it also
// reserves the Node ID and the identity in memory, so a concurrent request
// for either from another party is denied.
func (p *Parent) claim(ev *evaluation) (fractal.Code, error) {
	id, identity := ev.request.NodeID, ev.request.Identity
	p.mu.Lock()
	defer p.mu.Unlock()
	if a, ok := p.byID[id]; ok {
		switch {
		case a.Revoked:
			return fractal.CodeJoinRevoked, fmt.Errorf("%s: %w", id, ErrRevoked)
		case a.Identity != identity:
			return fractal.CodeJoinCollision, fmt.Errorf("%s: %w", id, ErrNodeIDHeld)
		}
		return 0, nil
	}
	if held, ok := p.byIdentity[identity]; ok && held != id {
		return fractal.CodeJoinCollision, fmt.Errorf("%s: %w", held, ErrIdentityHolds)
	}
	if other, ok := p.claimed[id]; ok && other != identity {
		return fractal.CodeJoinCollision, fmt.Errorf("%s: %w", id, ErrNodeIDHeld)
	}
	if held, ok := p.claimedBy[identity]; ok && held != id {
		return fractal.CodeJoinCollision, fmt.Errorf("%s: %w", held, ErrIdentityHolds)
	}
	p.claimed[id] = identity
	p.claimedBy[identity] = id
	p.claimants[id]++
	ev.claimed = true
	return 0, nil
}
