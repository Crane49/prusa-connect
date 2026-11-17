package join

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
)

// TestTheFunctionAdaptersCallTheFunction checks that VerifierFunc,
// RevocationFunc and RequesterFunc call the function they wrap.
func TestTheFunctionAdaptersCallTheFunction(t *testing.T) {
	t.Parallel()
	want := request("printer-1", "child-key")

	t.Run("the verifier adapter", func(t *testing.T) {
		t.Parallel()
		var verifier ProofVerifier = VerifierFunc(
			func(_ context.Context, envelope []byte) (fractal.JoinRequest, error) {
				if string(envelope) != "bytes" {
					return fractal.JoinRequest{}, errors.New("another envelope")
				}
				return want, nil
			})
		got, err := verifier.Verify(t.Context(), []byte("bytes"))
		if err != nil || got != want {
			t.Errorf("the verifier returned %+v and %v", got, err)
		}
	})

	t.Run("the revocation adapter", func(t *testing.T) {
		t.Parallel()
		var seen Admission
		var hook RevocationHook = RevocationFunc(func(_ context.Context, a Admission) error {
			seen = a
			return nil
		})
		if err := hook.Revoke(t.Context(), Admission{NodeID: "printer-1"}); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		if seen.NodeID != "printer-1" {
			t.Errorf("the hook received %+v, wanted the admission of printer-1", seen)
		}
	})

	t.Run("the requester adapter", func(t *testing.T) {
		t.Parallel()
		var requester Requester = RequesterFunc(
			func(_ context.Context, req fractal.JoinRequest) (Reply, error) {
				return Reply{Granted: req.NodeID == "printer-1"}, nil
			})
		reply, err := requester.Request(t.Context(), want)
		if err != nil || !reply.Granted {
			t.Errorf("the requester returned %+v and %v", reply, err)
		}
	})
}

// TestTrustOnFirstUseNeedsNoCredential covers fractal-nats.md, TOFU Mode: a
// node needs no join credential. TOFU authorizes a request with an empty
// credential.
func TestTrustOnFirstUseNeedsNoCredential(t *testing.T) {
	t.Parallel()
	if err := (TOFU{}).Authorize(t.Context(), fractal.JoinRequest{}, parentPath); err != nil {
		t.Errorf("TOFU refused a request: %v", err)
	}
}

// TestARevocationHookThatFailsIsReported covers a RevocationHook that returns
// an error. Revoke has saved the revoked record by then, so the Parent denies
// the node from then on. Revoke returns the error of the hook, so the caller
// knows the credentials of the open connection may still work.
//
// The caller then calls Revoke again. The hook runs again, because the node's
// access is still there until it returns nil.
func TestARevocationHookThatFailsIsReported(t *testing.T) {
	broken := errors.New("the account server is unreachable")
	f := newFixture(t)
	f.hook.err = broken
	f.mustGrant(t, request("printer-1", "child-key"))
	if err := f.parent.Revoke(t.Context(), "printer-1"); !errors.Is(err, broken) {
		t.Fatalf("the error is %v, wanted %v", err, broken)
	}
	f.mustDeny(t, request("printer-1", "child-key"), fractal.CodeJoinRevoked)

	t.Run("the hook still fails", func(t *testing.T) {
		before := f.hook.calls()
		if err := f.parent.Revoke(t.Context(), "printer-1"); !errors.Is(err, broken) {
			t.Errorf("the second Revoke returned %v, want the error of the hook", err)
		}
		if after := f.hook.calls(); after != before+1 {
			t.Errorf("the hook ran %d times, want one more than %d", after, before)
		}
	})

	t.Run("the hook recovers", func(t *testing.T) {
		f.hook.err = nil
		if err := f.parent.Revoke(t.Context(), "printer-1"); err != nil {
			t.Errorf("the Revoke after the hook recovered returned %v", err)
		}
		before := f.hook.calls()
		if err := f.parent.Revoke(t.Context(), "printer-1"); err != nil {
			t.Errorf("a repeated Revoke returned %v", err)
		}
		if after := f.hook.calls(); after != before {
			t.Errorf("the hook ran again after it had succeeded: %d, want %d", after, before)
		}
	})
}

// TestAParentWithNoRevocationHookStillBarsTheNodeID checks that Revoke works
// with ParentConfig.Revocation nil: the Parent denies the revoked Node ID.
func TestAParentWithNoRevocationHookStillBarsTheNodeID(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(cfg *ParentConfig) { cfg.Revocation = nil })
	f.mustGrant(t, request("printer-1", "child-key"))
	if err := f.parent.Revoke(t.Context(), "printer-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	f.mustDeny(t, request("printer-1", "child-key"), fractal.CodeJoinRevoked)
}

// TestMarkConnectedNamesAnAdmittedChild checks MarkConnected. It returns
// ErrUnknownChild for a Node ID with no admission. For an admitted node it
// sets EverConnected, and a second call is not an error.
func TestMarkConnectedNamesAnAdmittedChild(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if err := f.parent.MarkConnected(t.Context(), "nobody"); !errors.Is(err, ErrUnknownChild) {
		t.Errorf("the error is %v, wanted %v", err, ErrUnknownChild)
	}
	f.mustGrant(t, request("printer-1", "child-key"))
	for range 2 {
		if err := f.parent.MarkConnected(t.Context(), "printer-1"); err != nil {
			t.Fatalf("mark connected: %v", err)
		}
	}
	if a, _ := f.parent.Admission("printer-1"); !a.EverConnected {
		t.Error("EverConnected is false after MarkConnected")
	}
}

// TestJoiningReconfiguresNoOtherNode covers fractal.md, Joining the Network:
// joining MUST NOT require any node other than the joining node to be
// reconfigured or restarted. The test admits a second node into a running
// Parent. The Node Path of the Parent and the admission of the first node do
// not change.
func TestJoiningReconfiguresNoOtherNode(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.mustGrant(t, request("printer-1", "child-key"))
	before := f.parent.Admissions()

	f.mustGrant(t, request("printer-2", "second-key"))
	if f.parent.Path() != parentPath {
		t.Errorf("the path of the Parent changed to %q", f.parent.Path())
	}
	after := f.parent.Admissions()
	if len(after) != len(before)+1 {
		t.Fatalf("the Parent has %d admissions, wanted %d", len(after), len(before)+1)
	}
	if after[0] != before[0] {
		t.Errorf("the admission of %s changed to %+v", before[0].NodeID, after[0])
	}
}

// TestTheChildStopsWhenTheCredentialSourceCloses checks that Join returns
// ErrNoCredentialSource when the Credentials channel is closed while the
// Joiner waits on it for a new join credential.
func TestTheChildStopsWhenTheCredentialSourceCloses(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		source := make(chan fractal.JoinCredential)
		r := &scriptedRequester{now: time.Now, answers: []answer{denied(fractal.CodeJoinUnauthorized)}}
		j, _ := newJoiner(t, r, func(cfg *JoinerConfig) { cfg.Credentials = source })
		done := make(chan error, 1)
		go func() { _, err := j.Join(context.Background()); done <- err }()
		synctest.Wait()
		close(source)
		if err := <-done; !errors.Is(err, ErrNoCredentialSource) {
			t.Errorf("Join returned %v, wanted %v", err, ErrNoCredentialSource)
		}
	})
}
