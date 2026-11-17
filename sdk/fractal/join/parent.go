package join

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// Outcome is the answer of a Parent to one join request. Evaluate returns an
// Outcome for every request. A Parent that cannot handle a request denies it
// and never drops it.
type Outcome struct {
	// Granted is true for a grant and false for a denial.
	Granted bool
	// Grant is the grant to send. It is set when every check passed.
	Grant fractal.JoinGrant
	// Denial is the denial to send otherwise. Its code is the code of the
	// first check that failed.
	Denial fractal.JoinDenial
	// Check is the name of the check that failed. It is for the log of this
	// node, and it is empty on a grant.
	Check string
	// Cause is the error behind the denial. It is for the log of this node.
	// Do not send it: the receiver is a node that has not been admitted.
	Cause error
}

// ParentConfig configures a Parent. Identity, Store, Verifier, Authorizer and
// Issuer are required.
type ParentConfig struct {
	// Identity is the authentication identity of this node. The Parent puts
	// it in every grant, so a joining node can see which node answered.
	Identity fractal.Identity
	// Path is the Node Path of this node. A node that is not the root leaves
	// it empty and calls SetPath after its own join.
	Path fractal.NodePath
	// Store keeps the admission records, so the Parent has them again after a
	// restart.
	Store AdmissionStore
	// Verifier decodes a request and checks its signature.
	Verifier ProofVerifier
	// Authorizer checks the join credential.
	Authorizer Authorizer
	// Issuer creates the connection credentials for a grant.
	Issuer Issuer
	// Revocation stops the connection credentials of a revoked node from
	// working. When it is nil, Revoke only marks the record as revoked.
	Revocation RevocationHook
	// Extensions is the join data of the protocol extensions this node offers,
	// keyed by the name of the extension. The Parent puts it in every grant.
	// Each name follows the Identifier grammar.
	Extensions map[fractal.Identifier]json.RawMessage
	// MaxJoinsInFlight limits how many requests the Parent evaluates at the
	// same time. Zero means no limit.
	MaxJoinsInFlight int
	// JoinsPerSecond limits how many requests the Parent accepts per second.
	// Zero means no limit.
	JoinsPerSecond float64
	// JoinBurst is how many requests may arrive at the same moment when
	// JoinsPerSecond is set. It defaults to one.
	JoinBurst int
	// Clock gives the time for the JoinsPerSecond limit. It defaults to
	// SystemClock.
	Clock Clock
}

// Parent is the node above in the join exchange. It evaluates the join
// requests of the nodes that ask to join under it, and it keeps their
// admission records. It is safe for concurrent use.
type Parent struct {
	identity   fractal.Identity
	store      AdmissionStore
	verifier   ProofVerifier
	authorizer Authorizer
	issuer     Issuer
	revocation RevocationHook
	extensions map[fractal.Identifier]json.RawMessage
	limiter    *limiter
	clock      Clock

	// writing is locked from the read of an admission to its record, so a
	// write never undoes a revocation. Lock it before mu.
	writing sync.Mutex

	mu         sync.Mutex
	path       fractal.NodePath
	byID       map[fractal.NodeID]Admission
	byIdentity map[fractal.Identity]fractal.NodeID
	claimed    map[fractal.NodeID]fractal.Identity
	claimedBy  map[fractal.Identity]fractal.NodeID
	claimants  map[fractal.NodeID]int
	// revocationDone is the Node IDs whose RevocationHook has returned nil
	// since this Parent started. Revoke runs the hook again for any other.
	revocationDone map[fractal.NodeID]bool
}

// NewParent checks cfg and loads every admission record from the store. It
// returns an error when a required field is missing, when Path is not valid,
// when an extension name is not an Identifier, or when the store cannot load
// the records.
func NewParent(ctx context.Context, cfg ParentConfig) (*Parent, error) {
	switch {
	case cfg.Identity == "":
		return nil, fmt.Errorf("parent config: %w", fractal.ErrMissingIdentity)
	case cfg.Store == nil:
		return nil, errors.New("parent config: no admission store")
	case cfg.Verifier == nil:
		return nil, errors.New("parent config: no proof verifier")
	case cfg.Authorizer == nil:
		return nil, errors.New("parent config: no authorizer")
	case cfg.Issuer == nil:
		return nil, errors.New("parent config: no issuer")
	}
	if cfg.Path != "" {
		if err := cfg.Path.Validate(); err != nil {
			return nil, fmt.Errorf("parent config: %w", err)
		}
	}
	for name := range cfg.Extensions {
		if err := name.Validate(); err != nil {
			return nil, fmt.Errorf("parent config: extension: %w", err)
		}
	}
	clock := cfg.Clock
	if clock == nil {
		clock = SystemClock{}
	}
	p := &Parent{
		identity:   cfg.Identity,
		store:      cfg.Store,
		verifier:   cfg.Verifier,
		authorizer: cfg.Authorizer,
		issuer:     cfg.Issuer,
		revocation: cfg.Revocation,
		extensions: maps.Clone(cfg.Extensions),
		limiter:    newLimiter(cfg.MaxJoinsInFlight, cfg.JoinsPerSecond, cfg.JoinBurst, clock.Now()),
		clock:      clock,
		path:       cfg.Path,
		byID:       map[fractal.NodeID]Admission{},
		byIdentity: map[fractal.Identity]fractal.NodeID{},
		claimed:    map[fractal.NodeID]fractal.Identity{},
		claimedBy:  map[fractal.Identity]fractal.NodeID{},
		claimants:  map[fractal.NodeID]int{},

		revocationDone: map[fractal.NodeID]bool{},
	}
	records, err := cfg.Store.LoadAdmissions(ctx)
	if err != nil {
		return nil, fmt.Errorf("restore the admissions: %w", err)
	}
	for _, a := range records {
		p.record(a)
	}
	return p, nil
}

// Path returns the Node Path of this node. It is empty until the node has one.
func (p *Parent) Path() fractal.NodePath {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.path
}

// SetPath sets the Node Path of this node. A node that is not the root calls
// it when its own join is granted. Until then the Parent denies every request
// with CodeJoinUnavailable.
func (p *Parent) SetPath(path fractal.NodePath) error {
	if err := path.Validate(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.path = path
	return nil
}

// Evaluate answers one join request. envelope is the bytes the transport
// received. Only the ProofVerifier reads them. A denial has the code of the
// first check that failed. Evaluate issues credentials and saves an admission
// record only after every check has passed.
func (p *Parent) Evaluate(ctx context.Context, envelope []byte) Outcome {
	release, ok := p.limiter.acquire(p.clock.Now())
	if !ok {
		return deny("joins in flight", fractal.CodeJoinUnavailable, ErrTooManyJoins)
	}
	defer release()

	ev := &evaluation{parent: p, envelope: envelope}
	defer ev.release()
	for _, s := range steps {
		code, err := s.Run(ctx, ev)
		if code != 0 {
			return deny(s.Name, code, err)
		}
	}
	grant, err := p.admit(ctx, ev)
	switch {
	case errors.Is(err, ErrRevoked):
		return deny("admission and uniqueness", fractal.CodeJoinRevoked, err)
	case err != nil:
		return deny("issue the credentials and record the admission", fractal.CodeJoinUnavailable, err)
	}
	return Outcome{Granted: true, Grant: grant}
}

// Revoke marks the admission of id as revoked, saves it and calls the
// RevocationHook. From then on the Parent denies every request for this Node
// ID, from any identity. It returns ErrUnknownChild when no node was admitted
// under id. When the hook fails, call Revoke again: the hook runs again until
// it has returned nil once since this Parent started.
func (p *Parent) Revoke(ctx context.Context, id fractal.NodeID) error {
	a, done, err := p.markRevoked(ctx, id)
	if err != nil || done {
		return err
	}
	if p.revocation != nil {
		if hookErr := p.revocation.Revoke(ctx, a); hookErr != nil {
			return fmt.Errorf("take access from %s: %w", id, hookErr)
		}
	}
	p.mu.Lock()
	p.revocationDone[id] = true
	p.mu.Unlock()
	return nil
}

// markRevoked saves the admission of id as revoked. done is true when the
// RevocationHook already ran for it since this Parent started.
func (p *Parent) markRevoked(ctx context.Context, id fractal.NodeID) (a Admission, done bool, err error) {
	p.writing.Lock()
	defer p.writing.Unlock()
	p.mu.Lock()
	a, ok := p.byID[id]
	done = p.revocationDone[id]
	p.mu.Unlock()
	if !ok {
		return a, false, fmt.Errorf("revoke %s: %w", id, ErrUnknownChild)
	}
	if a.Revoked {
		return a, done, nil
	}
	a.Revoked = true
	if err = p.save(ctx, a); err != nil {
		return a, false, fmt.Errorf("write the revoked admission of %s: %w", id, err)
	}
	return a, false, nil
}

// MarkConnected sets EverConnected on the admission of id and saves it. A
// transport binding calls it the first time the node with that Node ID
// connects.
func (p *Parent) MarkConnected(ctx context.Context, id fractal.NodeID) error {
	p.writing.Lock()
	defer p.writing.Unlock()
	p.mu.Lock()
	a, ok := p.byID[id]
	p.mu.Unlock()
	if !ok {
		return fmt.Errorf("mark %s connected: %w", id, ErrUnknownChild)
	}
	if a.EverConnected {
		return nil
	}
	a.EverConnected = true
	if err := p.save(ctx, a); err != nil {
		return fmt.Errorf("write the admission of %s: %w", id, err)
	}
	return nil
}

// Admission returns the admission record of id. ok is false when the Parent
// has admitted no node under that Node ID.
func (p *Parent) Admission(id fractal.NodeID) (a Admission, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	a, ok = p.byID[id]
	return a, ok
}

// Admissions returns every record, revoked ones included, ordered by Node ID.
// The caller owns the returned slice.
func (p *Parent) Admissions() []Admission {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Admission, 0, len(p.byID))
	for _, a := range p.byID {
		out = append(out, a)
	}
	slices.SortFunc(out, func(x, y Admission) int { return compareNodeID(x.NodeID, y.NodeID) })
	return out
}

// admit saves the admission record of a new node, issues the credentials and
// builds the grant. It returns ErrRevoked for a node revoked since the checks.
func (p *Parent) admit(ctx context.Context, ev *evaluation) (fractal.JoinGrant, error) {
	req := ev.request
	if err := p.recordNew(ctx, req.NodeID, req.Identity); err != nil {
		return fractal.JoinGrant{}, err
	}
	credentials, expires, err := p.issuer.Issue(ctx, req.NodeID, req.Identity, ev.childPath)
	if err != nil {
		return fractal.JoinGrant{}, fmt.Errorf("issue the connection credentials: %w", err)
	}
	grant := fractal.JoinGrant{
		Version:        fractal.Version,
		NodePath:       ev.childPath,
		ParentIdentity: p.identity,
		Credentials:    credentials,
		ExpiresAt:      expires,
		Extensions:     maps.Clone(p.extensions),
	}
	if grantErr := grant.Validate(); grantErr != nil {
		return fractal.JoinGrant{}, fmt.Errorf("the issuer produced an unusable grant: %w", grantErr)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if revoked := p.revokedSinceTheCheck(req.NodeID); revoked != nil {
		return fractal.JoinGrant{}, revoked
	}
	p.unclaim(req.NodeID, req.Identity)
	ev.claimed = false
	return grant, nil
}

// recordNew saves the admission of a node that has none yet. The record must
// come before the credentials, so no credentials exist for an unrecorded node.
func (p *Parent) recordNew(ctx context.Context, id fractal.NodeID, identity fractal.Identity) error {
	p.writing.Lock()
	defer p.writing.Unlock()
	p.mu.Lock()
	_, held := p.byID[id]
	revoked := p.revokedSinceTheCheck(id)
	p.mu.Unlock()
	if revoked != nil || held {
		return revoked
	}
	if err := p.save(ctx, Admission{NodeID: id, Identity: identity}); err != nil {
		return fmt.Errorf("write the admission: %w", err)
	}
	return nil
}

// revokedSinceTheCheck returns ErrRevoked when id was revoked after the
// admission check passed. The caller must hold the lock.
func (p *Parent) revokedSinceTheCheck(id fractal.NodeID) error {
	if p.byID[id].Revoked {
		return fmt.Errorf("%s: %w", id, ErrRevoked)
	}
	return nil
}

// save writes one admission to the store and then records it. The caller
// holds p.writing.
func (p *Parent) save(ctx context.Context, a Admission) error {
	if err := p.store.SaveAdmission(ctx, a); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.record(a)
	return nil
}

// record puts one admission into p.byID and p.byIdentity, the maps the checks
// read. The caller holds the lock.
func (p *Parent) record(a Admission) {
	p.byID[a.NodeID] = a
	p.byIdentity[a.Identity] = a.NodeID
}

// holds reports whether identity already owns the Node ID id under this
// Parent. The authorization check does not run for such a request.
func (p *Parent) holds(id fractal.NodeID, identity fractal.Identity) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	a, ok := p.byID[id]
	return ok && a.Identity == identity
}

// deny builds the Outcome for a check that failed.
func deny(check string, code fractal.Code, cause error) Outcome {
	return Outcome{
		Denial: fractal.JoinDenial{Version: fractal.Version, Code: code},
		Check:  check,
		Cause:  cause,
	}
}
