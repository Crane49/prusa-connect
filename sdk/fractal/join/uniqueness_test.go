package join

import (
	"errors"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestTheParentDeniesACollisionInBothDirections covers fractal.md, Uniqueness:
// a Parent MUST deny a request with CodeJoinCollision when another
// authentication identity owns the requested Node ID, and when the identity in
// the request already owns a different Node ID.
func TestTheParentDeniesACollisionInBothDirections(t *testing.T) {
	f := newFixture(t)
	f.mustGrant(t, request("printer-1", "child-key"))

	t.Run("another identity asks for a node id that is taken", func(t *testing.T) {
		out := f.mustDeny(t, request("printer-1", "other-key"), fractal.CodeJoinCollision)
		if !errors.Is(out.Cause, ErrNodeIDHeld) {
			t.Errorf("the reason is %v, wanted %v", out.Cause, ErrNodeIDHeld)
		}
	})
	t.Run("an identity that owns a node id asks for another one", func(t *testing.T) {
		out := f.mustDeny(t, request("printer-2", "child-key"), fractal.CodeJoinCollision)
		if !errors.Is(out.Cause, ErrIdentityHolds) {
			t.Errorf("the reason is %v, wanted %v", out.Cause, ErrIdentityHolds)
		}
	})
	t.Run("a new identity that asks for a free node id is granted", func(t *testing.T) {
		f.mustGrant(t, request("printer-2", "second-key"))
	})
}

// TestTheHoldingIdentityGetsAFreshGrant covers fractal.md, Uniqueness. A
// request comes from the identity that already owns the requested Node ID, and
// the admission is not revoked. The Parent MUST answer with a grant that has
// newly issued credentials, and MUST NOT discard the state it keeps for that
// node. Here that state is the admission record with EverConnected.
func TestTheHoldingIdentityGetsAFreshGrant(t *testing.T) {
	f := newFixture(t)
	first := f.mustGrant(t, request("printer-1", "child-key"))
	if err := f.parent.MarkConnected(t.Context(), "printer-1"); err != nil {
		t.Fatalf("mark connected: %v", err)
	}
	second := f.mustGrant(t, request("printer-1", "child-key"))

	t.Run("the second grant has new credentials", func(t *testing.T) {
		if second.Credentials == first.Credentials {
			t.Error("the second grant has the credentials of the first grant")
		}
	})
	t.Run("the path is the same", func(t *testing.T) {
		if second.NodePath != first.NodePath {
			t.Errorf("the path changed from %q to %q", first.NodePath, second.NodePath)
		}
	})
	t.Run("the admission record is unchanged", func(t *testing.T) {
		a, ok := f.parent.Admission("printer-1")
		if !ok || !a.EverConnected || a.Identity != "child-key" {
			t.Errorf("the admission is %+v, ok %v", a, ok)
		}
	})
	t.Run("a revoked node gets no new grant", func(t *testing.T) {
		if err := f.parent.Revoke(t.Context(), "printer-1"); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		f.mustDeny(t, request("printer-1", "child-key"), fractal.CodeJoinRevoked)
	})
}

// TestConcurrentRequestsForOneNodeIDGrantAtMostOne covers fractal.md,
// Uniqueness: when requests with different identities ask for the same Node ID
// at the same time, a Parent MUST grant at most one of them and MUST deny the
// others with CodeJoinCollision.
func TestConcurrentRequestsForOneNodeIDGrantAtMostOne(t *testing.T) {
	t.Parallel()
	const racers = 64
	start := make(chan struct{})
	f := newFixture(t)

	bodies := make([][]byte, racers)
	for i := range racers {
		bodies[i] = envelope(t, request("printer-1", fractal.Identity("key-"+strconv.Itoa(i))))
	}
	var wg sync.WaitGroup
	outcomes := make([]Outcome, racers)
	for i := range racers {
		wg.Go(func() {
			<-start
			outcomes[i] = f.parent.Evaluate(t.Context(), bodies[i])
		})
	}
	close(start)
	wg.Wait()

	granted := 0
	for i, out := range outcomes {
		if out.Granted {
			granted++
			continue
		}
		if out.Denial.Code != fractal.CodeJoinCollision {
			t.Errorf("request %d was denied at %q with %s, wanted a collision", i, out.Check, out.Denial.Code)
		}
	}
	if granted != 1 {
		t.Fatalf("%d requests were granted, wanted exactly one", granted)
	}
	if n := len(f.parent.Admissions()); n != 1 {
		t.Errorf("the Parent has %d admissions, wanted 1", n)
	}
}

// TestConcurrentRequestsFromOneIdentityGrantOneNodeID covers fractal.md,
// Uniqueness: each authentication identity holds at most one Node ID. The
// first request waits in Issue while the second, from the same identity for
// another Node ID, is evaluated. The second MUST be denied as a collision.
func TestConcurrentRequestsFromOneIdentityGrantOneNodeID(t *testing.T) {
	held := newGate(nil)
	f := newFixture(t, func(cfg *ParentConfig) { cfg.Issuer = newGatedIssuer(held) })

	first := make(chan Outcome, 1)
	go func() { first <- f.join(t, request("printer-1", "cloned-key")) }()
	<-held.entered
	second := f.join(t, request("printer-2", "cloned-key"))
	close(held.release)

	got := results(<-first, second)
	want := []result{{Granted: true}, {Code: fractal.CodeJoinCollision}}
	if !slices.Equal(got, want) {
		t.Errorf("the outcomes are %+v, wanted %+v", got, want)
	}
}

// TestAFailedSharerKeepsTheReservation covers fractal.md, Uniqueness. Two
// requests from one identity for one Node ID share a reservation while both
// wait in Issue, as a node that asks again after its request expired does.
// The first fails in Issue. The reservation MUST hold for the second. A third
// request that asks for the same Node ID with another identity, or for
// another Node ID with the same identity, is then denied as a collision.
func TestAFailedSharerKeepsTheReservation(t *testing.T) {
	for name, third := range map[string]fractal.JoinRequest{
		"another identity": request("printer-1", "other-key"),
		"another node id":  request("printer-2", "child-key"),
	} {
		t.Run(name, func(t *testing.T) {
			failing, waiting := newGate(errors.New("the issuer fails")), newGate(nil)
			f := newFixture(t, func(cfg *ParentConfig) { cfg.Issuer = newGatedIssuer(failing, waiting) })

			first, second := make(chan Outcome, 1), make(chan Outcome, 1)
			go func() { first <- f.join(t, request("printer-1", "child-key")) }()
			<-failing.entered
			go func() { second <- f.join(t, request("printer-1", "child-key")) }()
			<-waiting.entered
			close(failing.release)
			failed := <-first
			denied := f.join(t, third)
			close(waiting.release)

			got := results(failed, <-second, denied)
			want := []result{
				{Code: fractal.CodeJoinUnavailable}, {Granted: true}, {Code: fractal.CodeJoinCollision},
			}
			if !slices.Equal(got, want) {
				t.Errorf("the outcomes are %+v, wanted %+v", got, want)
			}
		})
	}
}

// result is the part of an Outcome the concurrency tests compare.
type result struct {
	Granted bool
	Code    fractal.Code
}

// results returns the result of each Outcome, in order.
func results(outcomes ...Outcome) []result {
	got := make([]result, 0, len(outcomes))
	for _, out := range outcomes {
		got = append(got, result{out.Granted, out.Denial.Code})
	}
	return got
}

// TestTheParentKeepsANodeIDWhileTheChildIsAway covers fractal.md, Uniqueness:
// a Parent MUST NOT free a Node ID because the node that owns it has
// disconnected. No code in this package frees a Node ID. The test checks that
// another identity is still denied the Node ID, and that the identity that
// owns it is still granted.
func TestTheParentKeepsANodeIDWhileTheChildIsAway(t *testing.T) {
	f := newFixture(t)
	f.mustGrant(t, request("printer-1", "child-key"))
	if err := f.parent.MarkConnected(t.Context(), "printer-1"); err != nil {
		t.Fatalf("mark connected: %v", err)
	}

	t.Run("another identity is still denied", func(t *testing.T) {
		f.mustDeny(t, request("printer-1", "other-key"), fractal.CodeJoinCollision)
	})
	t.Run("the identity that owns the node id is still granted", func(t *testing.T) {
		f.mustGrant(t, request("printer-1", "child-key"))
	})
}

// TestAnIdentityKeepsItsNodeIDWhenIssueFails covers fractal.md, Uniqueness:
// each authentication identity holds at most one Node ID. The admission of
// printer-1 is saved, and then Issue fails. The identity holds printer-1 from
// then on, before and after a restart.
func TestAnIdentityKeepsItsNodeIDWhenIssueFails(t *testing.T) {
	failed := newGate(errors.New("the account was not minted"))
	close(failed.release)
	f := newFixture(t, func(cfg *ParentConfig) { cfg.Issuer = newGatedIssuer(failed) })
	first := f.join(t, request("printer-1", "child-key")).Denial.Code
	second := f.join(t, request("printer-2", "child-key")).Denial.Code
	restarted := newFixture(t, func(cfg *ParentConfig) { cfg.Store = f.store })

	got := map[string]any{
		"first":     first,
		"second":    second,
		"restarted": restarted.parent.Admissions(),
	}
	want := map[string]any{
		"first":     fractal.CodeJoinUnavailable,
		"second":    fractal.CodeJoinCollision,
		"restarted": []Admission{{NodeID: "printer-1", Identity: "child-key"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after the failed Issue the Parent has %v, wanted %v", got, want)
	}
}
