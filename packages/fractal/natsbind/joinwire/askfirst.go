package joinwire

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/join"
)

// DefaultMaxPending is the most waiting records AskFirst keeps. An approved
// record does not count. Every machine that can reach the join endpoint can
// add a waiting record, so the number has a limit.
const DefaultMaxPending = 256

// DefaultPendingExpiry is how long AskFirst keeps a waiting record that no
// operator decided about. Every machine that reaches the join endpoint can add
// one, so a record an operator never looked at is removed after this time and
// the Node ID is free again.
const DefaultPendingExpiry = 24 * time.Hour

// The errors AskFirst returns.
var (
	// ErrAwaitingApproval means AskFirst recorded the join request and an
	// operator has not approved it yet. The denial has the code
	// CodeAwaitingApproval, and the node that asked sends the request again
	// later.
	ErrAwaitingApproval = errors.New("the identity is waiting for an operator")
	// ErrNoPending means Approve or Forget named a Node ID that has no record.
	ErrNoPending = errors.New("no request is waiting under that node id")
	// ErrTooManyPending means AskFirst already has MaxPending records, so it
	// did not record the join request.
	ErrTooManyPending = errors.New("the set of waiting identities is full")
	// ErrIdentityChanged means the public key in the join request is not the
	// key AskFirst holds for that Node ID. The key that asked first keeps the
	// Node ID, whether an operator has approved it or not.
	ErrIdentityChanged = errors.New("another identity asks under that node id")
)

// Record is one Node ID that sent a join request, with the public key that
// asked for it first.
type Record struct {
	// NodeID is the Node ID the join request asked for.
	NodeID fractal.NodeID `json:"nodeId"`
	// Identity is the public key that signed the first join request for this
	// Node ID. It never changes: a second public key asking for the same Node
	// ID is refused as a collision.
	Identity fractal.Identity `json:"identity"`
	// FirstSeen is when the first join request for this Node ID arrived.
	FirstSeen time.Time `json:"firstSeen"`
	// LastSeen is when the latest join request for this Node ID arrived.
	LastSeen time.Time `json:"lastSeen"`
	// Approved is true after an operator approved Identity for this Node ID.
	Approved bool `json:"approved"`
}

// Store saves the records of AskFirst so that they survive a restart. When
// Load cannot read the records it returns an error and never an empty list.
// With an empty list AskFirst would lose an approval, and the node would wait
// for an operator who has already approved it.
type Store interface {
	// Load returns every record.
	Load(ctx context.Context) ([]Record, error)
	// Save writes one record and replaces the record with the same Node ID.
	// It returns after the record is safe on disk.
	Save(ctx context.Context, r Record) error
	// Delete removes the record of one Node ID. A Node ID with no record is
	// not an error, so an operator can repeat the command.
	Delete(ctx context.Context, id fractal.NodeID) error
}

// AskFirst denies a join request from a public key no operator has approved,
// and records it so that an operator can approve it. After the approval it
// authorizes the next join request from that key.
//
// It implements join.Authorizer of the sdk and runs another Authorizer first.
// With join.TOFU as Inner, the operator's approval replaces trust on first
// use. With TokenAuthorizer as Inner, a join request needs a valid join
// credential and the approval.
type AskFirst struct {
	// Inner is the Authorizer that runs first. When it is nil, the approval is
	// the only check.
	Inner join.Authorizer
	// Store saves the records. It is required.
	Store Store
	// Now returns the current time. Nil means time.Now.
	Now func() time.Time
	// MaxPending is the most waiting records AskFirst keeps. An approved
	// record does not count. Zero or less means DefaultMaxPending.
	MaxPending int
	// Expiry is how long a waiting record stays. A record whose latest join
	// request is older than Expiry is removed when the next join request
	// arrives and when Expire runs. An approved record never expires. Zero or
	// less means DefaultPendingExpiry.
	Expiry time.Duration

	mu   sync.Mutex
	byID map[fractal.NodeID]Record
}

// NewAskFirst builds an AskFirst and loads its records from the store, so an
// approval an operator gave before a restart is still valid.
func NewAskFirst(ctx context.Context, inner join.Authorizer, store Store) (*AskFirst, error) {
	a := &AskFirst{Inner: inner, Store: store}
	if err := a.Restore(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

// Restore loads every record from the store. Call it before the node answers
// its first join request.
func (a *AskFirst) Restore(ctx context.Context) error {
	if a.Store == nil {
		return errors.New("ask-first: no store")
	}
	records, err := a.Store.Load(ctx)
	if err != nil {
		return fmt.Errorf("restore the waiting identities: %w", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.byID = make(map[fractal.NodeID]Record, len(records))
	for _, r := range records {
		a.byID[r.NodeID] = r
	}
	return nil
}

// Authorize runs Inner first. It then returns nil only when an operator has
// approved the public key of the request for this Node ID.
//
// It denies a key no operator has approved with CodeAwaitingApproval. The
// returned join.CodeError makes the node above send that code in place of
// fractal.CodeJoinUnauthorized.
//
// A Node ID belongs to the public key that asked for it first. A second key
// asking for the same Node ID is denied with ErrIdentityChanged and does not
// replace the record, so the operator approves the key they saw.
//
// It holds the lock from the read of the record to the write of the record.
// The node that waits sends its join request again and again while the
// operator reads the links page, so a check made against one value of the
// record and written back later would drop the operator's approval.
func (a *AskFirst) Authorize(ctx context.Context, req fractal.JoinRequest, parent fractal.NodePath) error {
	if a.Inner != nil {
		if err := a.Inner.Authorize(ctx, req, parent); err != nil {
			return err
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.expireLocked(ctx)
	held, ok := a.byID[req.NodeID]
	switch {
	case ok && held.Identity != req.Identity:
		return fmt.Errorf("%w: %s: %w", join.ErrUnauthorized, req.NodeID, ErrIdentityChanged)
	case ok && held.Approved:
		held.LastSeen = a.now()
		// A failed save loses only the new LastSeen. The approval is still
		// valid, and only an operator reads LastSeen.
		_ = a.writeLocked(ctx, held)
		return nil
	}
	if err := a.rememberLocked(ctx, req); err != nil {
		return err
	}
	return &join.CodeError{
		Code: CodeAwaitingApproval,
		Err:  fmt.Errorf("%w: %s: %w", join.ErrUnauthorized, req.NodeID, ErrAwaitingApproval),
	}
}

// Approve marks the record of id as approved. The key it approves is the one
// that signed the first join request for id, which is the key Record and
// Pending show the operator. Authorize accepts the next join request from that
// key. Approve changes nothing when the record is already approved, so an
// operator can repeat the command.
func (a *AskFirst) Approve(ctx context.Context, id fractal.NodeID) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	held, ok := a.byID[id]
	if !ok {
		return fmt.Errorf("approve %s: %w", id, ErrNoPending)
	}
	if held.Approved {
		return nil
	}
	held.Approved = true
	return a.writeLocked(ctx, held)
}

// Forget removes the record of id. The Node ID is then free: the next join
// request for it, from any public key, waits for an operator again. Use it to
// clear a name a machine asked for and the operator does not want to admit,
// and to let another machine ask for that name. For a node that has joined, an
// operator uses revocation.
func (a *AskFirst) Forget(ctx context.Context, id fractal.NodeID) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.byID[id]; !ok {
		return fmt.Errorf("forget %s: %w", id, ErrNoPending)
	}
	if a.Store == nil {
		return errors.New("ask-first: no store")
	}
	if err := a.Store.Delete(ctx, id); err != nil {
		return fmt.Errorf("remove the waiting identity %s: %w", id, err)
	}
	delete(a.byID, id)
	return nil
}

// Expire removes every waiting record whose latest join request is older than
// Expiry, and returns how many it removed. A record an operator approved is
// kept. Call it on a timer, so a record nobody asks about again still goes.
func (a *AskFirst) Expire(ctx context.Context) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.expireLocked(ctx)
}

// expireLocked removes the waiting records that are older than Expiry and
// returns how many it removed. The caller holds the lock.
//
// A record the store refuses to delete stays in memory, so the two never
// disagree about which Node IDs are waiting.
func (a *AskFirst) expireLocked(ctx context.Context) int {
	if a.Store == nil {
		return 0
	}
	limit := a.Expiry
	if limit <= 0 {
		limit = DefaultPendingExpiry
	}
	now := a.now()
	removed := 0
	for id, r := range a.byID {
		if r.Approved || now.Sub(r.LastSeen) <= limit {
			continue
		}
		if err := a.Store.Delete(ctx, id); err != nil {
			continue
		}
		delete(a.byID, id)
		removed++
	}
	return removed
}

// Records returns every record, sorted by Node ID. The caller owns the
// returned slice.
func (a *AskFirst) Records() []Record {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Record, 0, len(a.byID))
	for _, r := range a.byID {
		out = append(out, r)
	}
	slices.SortFunc(out, func(x, y Record) int { return compareID(x.NodeID, y.NodeID) })
	return out
}

// Pending returns the records an operator has not approved, sorted by Node
// ID. The caller owns the returned slice.
func (a *AskFirst) Pending() []Record {
	all := a.Records()
	return slices.DeleteFunc(all, func(r Record) bool { return r.Approved })
}

// Record returns the record of one Node ID, so an operator sees the public key
// before he approves it. The second result is false when no join request for
// id has arrived.
func (a *AskFirst) Record(id fractal.NodeID) (Record, bool) { return a.record(id) }

// record returns the record of one Node ID.
func (a *AskFirst) record(id fractal.NodeID) (Record, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r, ok := a.byID[id]
	return r, ok
}

// rememberLocked saves the record of a join request that no operator has
// approved. When AskFirst already holds MaxPending waiting records it saves
// nothing and the request is still denied, so many requests from unknown keys
// cannot use up the node's memory. The caller holds the lock.
//
// The caller has checked that the public key of the request is the one in the
// record, so rememberLocked never writes another key over it.
func (a *AskFirst) rememberLocked(ctx context.Context, req fractal.JoinRequest) error {
	now := a.now()
	held, ok := a.byID[req.NodeID]
	limit := a.MaxPending
	if limit <= 0 {
		limit = DefaultMaxPending
	}
	if !ok && a.countWaiting() >= limit {
		return fmt.Errorf("%w: %w", join.ErrUnauthorized, ErrTooManyPending)
	}
	if !ok {
		held = Record{NodeID: req.NodeID, Identity: req.Identity, FirstSeen: now}
	}
	held.LastSeen = now
	return a.writeLocked(ctx, held)
}

// countWaiting returns how many records no operator has approved. The caller
// holds the lock.
func (a *AskFirst) countWaiting() int {
	waiting := 0
	for _, r := range a.byID {
		if !r.Approved {
			waiting++
		}
	}
	return waiting
}

// writeLocked saves one record in the store and then in memory. The store
// comes first, so memory never has a record the store does not have. The
// caller holds the lock, and the store write happens under it: a save that ran
// outside the lock could write a record another call had changed in the
// meantime.
func (a *AskFirst) writeLocked(ctx context.Context, r Record) error {
	if a.Store == nil {
		return errors.New("ask-first: no store")
	}
	if err := a.Store.Save(ctx, r); err != nil {
		return fmt.Errorf("write the waiting identity %s: %w", r.NodeID, err)
	}
	if a.byID == nil {
		a.byID = map[fractal.NodeID]Record{}
	}
	a.byID[r.NodeID] = r
	return nil
}

// now returns a.Now(), or time.Now() when Now is nil.
func (a *AskFirst) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// compareID compares two Node IDs as text.
func compareID(x, y fractal.NodeID) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}
