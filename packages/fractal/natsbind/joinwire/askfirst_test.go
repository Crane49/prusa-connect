package joinwire_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/prusa3d/fractal-link/natsbind/joinwire"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	"github.com/prusa3d/prusa-connect/sdk/fractal/join"
)

// askRequest returns the join request the tests in this file use: printer-1
// with the public key "UAAAA".
func askRequest() fractal.JoinRequest {
	return fractal.JoinRequest{Version: fractal.Version, NodeID: "printer-1", Identity: "UAAAA"}
}

// TestAnUnknownIdentityWaitsAndThenJoins checks that AskFirst denies and
// records the first join request of an unknown key, and authorizes the next
// one after an operator approved.
func TestAnUnknownIdentityWaitsAndThenJoins(t *testing.T) {
	t.Parallel()
	ask, err := joinwire.NewAskFirst(t.Context(), join.TOFU{}, &joinwire.MemoryStore{})
	if err != nil {
		t.Fatalf("build the mode: %v", err)
	}
	refusal := ask.Authorize(t.Context(), askRequest(), "hq")
	if !errors.Is(refusal, joinwire.ErrAwaitingApproval) {
		t.Fatalf("the refusal is %v, want a wait for an operator", refusal)
	}
	if !errors.Is(refusal, join.ErrUnauthorized) {
		t.Errorf("the refusal does not answer with the unauthorized code: %v", refusal)
	}
	var carried *join.CodeError
	if !errors.As(refusal, &carried) || carried.Code != joinwire.CodeAwaitingApproval {
		t.Errorf("the refusal is %v, want one naming the waiting refinement", refusal)
	}
	pending := ask.Pending()
	if len(pending) != 1 || pending[0].NodeID != "printer-1" {
		t.Fatalf("the waiting set is %v, want printer-1", pending)
	}
	if approveErr := ask.Approve(t.Context(), "printer-1"); approveErr != nil {
		t.Fatalf("approve: %v", approveErr)
	}
	if authErr := ask.Authorize(t.Context(), askRequest(), "hq"); authErr != nil {
		t.Errorf("an approved identity was refused: %v", authErr)
	}
	if got := len(ask.Pending()); got != 0 {
		t.Errorf("%d identities still wait, want none", got)
	}
}

// TestAnApprovalIsForOneIdentity checks that an approval is for the public key
// that asked. A different key with the same Node ID is refused.
func TestAnApprovalIsForOneIdentity(t *testing.T) {
	t.Parallel()
	ask, err := joinwire.NewAskFirst(t.Context(), join.TOFU{}, &joinwire.MemoryStore{})
	if err != nil {
		t.Fatalf("build the mode: %v", err)
	}
	_ = ask.Authorize(t.Context(), askRequest(), "hq") // The first request is always a wait.
	if approveErr := ask.Approve(t.Context(), "printer-1"); approveErr != nil {
		t.Fatalf("approve: %v", approveErr)
	}
	other := askRequest()
	other.Identity = "UBBBB"
	if authErr := ask.Authorize(t.Context(), other, "hq"); !errors.Is(authErr, joinwire.ErrIdentityChanged) {
		t.Errorf("the refusal is %v, want another identity", authErr)
	}
}

// TestTheIdentityThatAskedFirstKeepsTheName checks that a second public key
// asking for a waiting Node ID is refused as a collision and does not replace
// the first one. The operator approves the key he saw, and the key that asked
// later is still refused (fractal.md, Uniqueness).
func TestTheIdentityThatAskedFirstKeepsTheName(t *testing.T) {
	t.Parallel()
	for _, order := range []struct {
		name   string
		first  fractal.Identity
		second fractal.Identity
	}{
		{name: "UAAAA asks first", first: "UAAAA", second: "UBBBB"},
		{name: "UBBBB asks first", first: "UBBBB", second: "UAAAA"},
	} {
		t.Run(order.name, func(t *testing.T) {
			t.Parallel()
			ask, err := joinwire.NewAskFirst(t.Context(), join.TOFU{}, &joinwire.MemoryStore{})
			if err != nil {
				t.Fatalf("build the mode: %v", err)
			}
			early := askRequest()
			early.Identity = order.first
			if authErr := ask.Authorize(t.Context(), early, "hq"); !errors.Is(authErr, joinwire.ErrAwaitingApproval) {
				t.Fatalf("the refusal is %v, want a wait for an operator", authErr)
			}
			late := askRequest()
			late.Identity = order.second
			if authErr := ask.Authorize(t.Context(), late, "hq"); !errors.Is(authErr, joinwire.ErrIdentityChanged) {
				t.Errorf("the refusal for the key that asked later is %v, want a collision", authErr)
			}
			waiting := ask.Pending()
			if len(waiting) != 1 || waiting[0].Identity != order.first {
				t.Fatalf("the operator sees %v, want only the key that asked first", waiting)
			}
			if approveErr := ask.Approve(t.Context(), "printer-1"); approveErr != nil {
				t.Fatalf("approve: %v", approveErr)
			}
			if authErr := ask.Authorize(t.Context(), early, "hq"); authErr != nil {
				t.Errorf("the key the operator approved was refused: %v", authErr)
			}
			if authErr := ask.Authorize(t.Context(), late, "hq"); !errors.Is(authErr, joinwire.ErrIdentityChanged) {
				t.Errorf("the key that asked later was admitted: %v", authErr)
			}
		})
	}
}

// TestTheBoundCountsOnlyWaitingIdentities checks that MaxPending counts the
// records an operator has not approved. An approved node does not use up a
// place, so approving nodes does not stop the next one from asking.
func TestTheBoundCountsOnlyWaitingIdentities(t *testing.T) {
	t.Parallel()
	ask := &joinwire.AskFirst{Inner: join.TOFU{}, Store: &joinwire.MemoryStore{}, MaxPending: 2}
	if err := ask.Restore(t.Context()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	for _, id := range []fractal.NodeID{"a", "b"} {
		req := askRequest()
		req.NodeID = id
		if err := ask.Authorize(t.Context(), req, "hq"); !errors.Is(err, joinwire.ErrAwaitingApproval) {
			t.Fatalf("%s: the refusal is %v, want a wait for an operator", id, err)
		}
		if err := ask.Approve(t.Context(), id); err != nil {
			t.Fatalf("approve %s: %v", id, err)
		}
	}
	req := askRequest()
	req.NodeID = "c"
	if err := ask.Authorize(t.Context(), req, "hq"); !errors.Is(err, joinwire.ErrAwaitingApproval) {
		t.Errorf("the refusal is %v, want a wait for an operator", err)
	}
}

// TestOneKeyWinsWhenManyAskForOneNameAtOnce checks the rule of fractal.md
// Uniqueness under load: of requests for one Node ID that arrive together, one
// key holds the name and every other key is refused as a collision.
func TestOneKeyWinsWhenManyAskForOneNameAtOnce(t *testing.T) {
	t.Parallel()
	ask, err := joinwire.NewAskFirst(t.Context(), join.TOFU{}, &joinwire.MemoryStore{})
	if err != nil {
		t.Fatalf("build the mode: %v", err)
	}
	const askers = 16
	var admitted sync.Map
	var group sync.WaitGroup
	start := make(chan struct{})
	for i := range askers {
		group.Go(func() {
			req := askRequest()
			req.Identity = fractal.Identity(fmt.Sprintf("U%02d", i))
			<-start
			if errors.Is(ask.Authorize(t.Context(), req, "hq"), joinwire.ErrAwaitingApproval) {
				admitted.Store(req.Identity, true)
			}
		})
	}
	close(start)
	group.Wait()

	held := ask.Records()
	if len(held) != 1 {
		t.Fatalf("the mode holds %v, want one record", held)
	}
	waiting := 0
	admitted.Range(func(key, _ any) bool {
		waiting++
		if key != any(held[0].Identity) {
			t.Errorf("the key %v was told to wait and the record holds %q", key, held[0].Identity)
		}
		return true
	})
	if waiting != 1 {
		t.Errorf("%d keys were told to wait for one name, want one", waiting)
	}
}

// TestTheBoundHoldsWhenRequestsArriveAtOnce checks that MaxPending is a bound
// and not a count taken before the write. Requests that arrive together must
// not each see room and each take it.
func TestTheBoundHoldsWhenRequestsArriveAtOnce(t *testing.T) {
	t.Parallel()
	ask := &joinwire.AskFirst{Inner: join.TOFU{}, Store: &joinwire.MemoryStore{}, MaxPending: 1}
	if err := ask.Restore(t.Context()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	var group sync.WaitGroup
	start := make(chan struct{})
	for i := range 16 {
		group.Go(func() {
			req := askRequest()
			req.NodeID = fractal.NodeID(fmt.Sprintf("junk-%d", i))
			<-start
			_ = ask.Authorize(t.Context(), req, "hq")
		})
	}
	close(start)
	group.Wait()

	if got := ask.Pending(); len(got) != 1 {
		t.Errorf("the mode holds %d waiting records against a bound of one: %v", len(got), got)
	}
}

// TestAnApprovalIsTakenBack checks that after Forget the next join request of
// that Node ID waits for an operator again.
func TestAnApprovalIsTakenBack(t *testing.T) {
	t.Parallel()
	ask, err := joinwire.NewAskFirst(t.Context(), join.TOFU{}, &joinwire.MemoryStore{})
	if err != nil {
		t.Fatalf("build the mode: %v", err)
	}
	_ = ask.Authorize(t.Context(), askRequest(), "hq") // The first request is always a wait.
	if approveErr := ask.Approve(t.Context(), "printer-1"); approveErr != nil {
		t.Fatalf("approve: %v", approveErr)
	}
	if forgetErr := ask.Forget(t.Context(), "printer-1"); forgetErr != nil {
		t.Fatalf("take the approval back: %v", forgetErr)
	}
	if authErr := ask.Authorize(t.Context(), askRequest(), "hq"); !errors.Is(authErr, joinwire.ErrAwaitingApproval) {
		t.Errorf("the refusal is %v, want a wait for an operator", authErr)
	}
}

// TestAWaitingNameCanBeCleared checks the way out of a name a machine took and
// the operator does not want to admit. Forget removes the record, so another
// machine can ask for that Node ID and wait for the operator in its turn.
func TestAWaitingNameCanBeCleared(t *testing.T) {
	t.Parallel()
	ask, err := joinwire.NewAskFirst(t.Context(), join.TOFU{}, &joinwire.MemoryStore{})
	if err != nil {
		t.Fatalf("build the mode: %v", err)
	}
	squatter := askRequest()
	squatter.Identity = "UEVIL"
	_ = ask.Authorize(t.Context(), squatter, "hq") // The first request is always a wait.
	if forgetErr := ask.Forget(t.Context(), "printer-1"); forgetErr != nil {
		t.Fatalf("clear the name: %v", forgetErr)
	}
	if got := ask.Records(); len(got) != 0 {
		t.Fatalf("the mode holds %v, want nothing after the name was cleared", got)
	}
	if authErr := ask.Authorize(t.Context(), askRequest(), "hq"); !errors.Is(authErr, joinwire.ErrAwaitingApproval) {
		t.Errorf("the machine that asked next was answered %v, want a wait for an operator", authErr)
	}
	waiting := ask.Pending()
	if len(waiting) != 1 || waiting[0].Identity != "UAAAA" {
		t.Errorf("the operator sees %v, want the key that asked after the name was cleared", waiting)
	}
}

// TestAnApprovalOutlastsTheRequestsThatArriveWithIt checks that a join request
// which arrives while an operator approves does not write the record back
// without the approval. The node that waits sends its request again and again,
// so such a request is always in flight when the operator acts.
func TestAnApprovalOutlastsTheRequestsThatArriveWithIt(t *testing.T) {
	t.Parallel()
	store := &joinwire.MemoryStore{}
	ask, err := joinwire.NewAskFirst(t.Context(), join.TOFU{}, store)
	if err != nil {
		t.Fatalf("build the mode: %v", err)
	}
	_ = ask.Authorize(t.Context(), askRequest(), "hq") // The first request is always a wait.

	asking := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		close(asking)
		for range 200 {
			_ = ask.Authorize(t.Context(), askRequest(), "hq")
		}
	}()
	<-asking
	if approveErr := ask.Approve(t.Context(), "printer-1"); approveErr != nil {
		t.Fatalf("approve: %v", approveErr)
	}
	<-done

	if got := ask.Pending(); len(got) != 0 {
		t.Errorf("%v still waits after the approval", got)
	}
	if authErr := ask.Authorize(t.Context(), askRequest(), "hq"); authErr != nil {
		t.Errorf("the key the operator approved was refused: %v", authErr)
	}
}

// TestApprovingAnIdentityNobodyHasSeenIsRefused checks that Approve returns
// ErrNoPending for a Node ID that has no record.
func TestApprovingAnIdentityNobodyHasSeenIsRefused(t *testing.T) {
	t.Parallel()
	ask, err := joinwire.NewAskFirst(t.Context(), join.TOFU{}, &joinwire.MemoryStore{})
	if err != nil {
		t.Fatalf("build the mode: %v", err)
	}
	if approveErr := ask.Approve(t.Context(), "printer-9"); !errors.Is(approveErr, joinwire.ErrNoPending) {
		t.Errorf("the refusal is %v, want no such waiting identity", approveErr)
	}
}

// TestTheCheckBehindTheModeStillRuns checks that AskFirst runs its Inner
// Authorizer first. AskFirst returns the error of Inner for a join request
// that Inner refuses, and it does not record the request.
func TestTheCheckBehindTheModeStillRuns(t *testing.T) {
	t.Parallel()
	refused := errors.New("the token was no good")
	inner := join.AuthorizerFunc(func(context.Context, fractal.JoinRequest, fractal.NodePath) error {
		return errors.Join(join.ErrUnauthorized, refused)
	})
	ask, err := joinwire.NewAskFirst(t.Context(), inner, &joinwire.MemoryStore{})
	if err != nil {
		t.Fatalf("build the mode: %v", err)
	}
	if authErr := ask.Authorize(t.Context(), askRequest(), "hq"); !errors.Is(authErr, refused) {
		t.Errorf("the refusal is %v, want the one from the check behind the mode", authErr)
	}
	if got := len(ask.Pending()); got != 0 {
		t.Errorf("%d identities wait, want none: the check behind the mode ran first", got)
	}
}

// TestTheWaitingSetIsBounded checks that AskFirst records at most MaxPending
// Node IDs, so join requests cannot use up the memory of a node.
func TestTheWaitingSetIsBounded(t *testing.T) {
	t.Parallel()
	ask := &joinwire.AskFirst{Inner: join.TOFU{}, Store: &joinwire.MemoryStore{}, MaxPending: 2}
	if err := ask.Restore(t.Context()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	for _, id := range []fractal.NodeID{"a", "b"} {
		req := askRequest()
		req.NodeID = id
		if err := ask.Authorize(t.Context(), req, "hq"); !errors.Is(err, joinwire.ErrAwaitingApproval) {
			t.Fatalf("%s: the refusal is %v, want a wait for an operator", id, err)
		}
	}
	req := askRequest()
	req.NodeID = "c"
	if err := ask.Authorize(t.Context(), req, "hq"); !errors.Is(err, joinwire.ErrTooManyPending) {
		t.Errorf("the refusal is %v, want a full waiting set", err)
	}
	if got := len(ask.Records()); got != 2 {
		t.Errorf("the mode holds %d identities, want its bound of two", got)
	}
}

// TestAnApprovalSurvivesARestart checks that a new AskFirst with the same
// FileStore file still has the operator's approval.
func TestAnApprovalSurvivesARestart(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pending.json")
	first, err := joinwire.NewAskFirst(t.Context(), join.TOFU{}, joinwire.NewFileStore(path))
	if err != nil {
		t.Fatalf("build the mode: %v", err)
	}
	_ = first.Authorize(t.Context(), askRequest(), "hq") // The first request is always a wait.
	if approveErr := first.Approve(t.Context(), "printer-1"); approveErr != nil {
		t.Fatalf("approve: %v", approveErr)
	}
	again, err := joinwire.NewAskFirst(t.Context(), join.TOFU{}, joinwire.NewFileStore(path))
	if err != nil {
		t.Fatalf("build the mode again: %v", err)
	}
	if authErr := again.Authorize(t.Context(), askRequest(), "hq"); authErr != nil {
		t.Errorf("an approved identity was refused after a restart: %v", authErr)
	}
}

// TestALapsedWaitingRecordGoesAndAnApprovedOneStays checks the expiry in both
// directions. A waiting record whose latest request is older than Expiry is
// removed, both when the next request arrives and when Expire runs. A record
// an operator approved stays however old it is.
func TestALapsedWaitingRecordGoesAndAnApprovedOneStays(t *testing.T) {
	t.Parallel()
	clock := time.Date(2026, time.September, 22, 8, 0, 0, 0, time.UTC)
	ask := &joinwire.AskFirst{
		Inner:  join.TOFU{},
		Store:  &joinwire.MemoryStore{},
		Now:    func() time.Time { return clock },
		Expiry: time.Hour,
	}
	if err := ask.Restore(t.Context()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	waiting := askRequest()
	waiting.NodeID = "stranger"
	_ = ask.Authorize(t.Context(), waiting, "hq") // The first request is always a wait.
	_ = ask.Authorize(t.Context(), askRequest(), "hq")
	if err := ask.Approve(t.Context(), "printer-1"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	clock = clock.Add(2 * time.Hour)
	if removed := ask.Expire(t.Context()); removed != 1 {
		t.Fatalf("the sweep removed %d records, want the one that lapsed", removed)
	}
	if got := len(ask.Pending()); got != 0 {
		t.Errorf("%d records still wait after the sweep", got)
	}
	if err := ask.Authorize(t.Context(), askRequest(), "hq"); err != nil {
		t.Errorf("the approved identity was refused after the sweep: %v", err)
	}
	second := askRequest()
	second.NodeID = "stranger"
	second.Identity = "UBBBB"
	if err := ask.Authorize(t.Context(), second, "hq"); !errors.Is(err, joinwire.ErrAwaitingApproval) {
		t.Errorf("the freed name refused another key: %v", err)
	}
}
