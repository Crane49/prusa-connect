package xbuddy

import (
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// TestARaisedDialogAndAClosedOneAgreeWithTheTree holds the rule the whole dialog
// subtree keeps: a patch is reported under the same lock the decision was taken
// under.
//
// Session.attach lets two connections be read at once, so a raise and a close
// run on two goroutines. Report the patch after the lock and the two steps
// interleave: the close decides that no dialog is up, the raise then writes its
// id, and the state tree holds an id the session believes is gone. A caller
// reading it answers a question the machine has stopped asking.
//
// It runs the two calls against each other rather than through a socket. An
// earlier gate drove 20,000 rounds through the wire and passed against the
// shape that reports outside the lock, because the window between deciding and
// reporting is a few instructions and every frame has a socket in front of it.
// At function level the same window is most of the call.
//
// The invariant is checked and not the ordering. Both orders are legal: a raise
// that lands last leaves the id on the tree, and a close that lands last leaves
// null there. What is never legal is the tree and the session disagreeing.
func TestARaisedDialogAndAClosedOneAgreeWithTheTree(t *testing.T) {
	t.Parallel()

	// A bare session, because newSession starts the pumps and one of them takes
	// the reserved slot this reads.
	session := &Session{reserved: make(map[string]connectorapi.StatePatch, len(dialogPaths))}

	const (
		dialog = int64(42)
		rounds = 30_000
	)

	for round := 1; round <= rounds; round++ {
		var running sync.WaitGroup

		running.Add(2)

		go func() {
			defer running.Done()

			session.raiseDialog(dialog)
		}()

		go func() {
			defer running.Done()

			session.clearDialog()
		}()

		running.Wait()

		// Both goroutines have joined, so inDialog is read without the lock the
		// way the session itself never reads it.
		up, onTree := session.inDialog, session.reservedValue(dialogIDPath)

		if up != (onTree != dialogAbsent) {
			t.Fatalf("round %d: the session says a dialog is up = %v, and %s holds %s",
				round, up, dialogIDPath, onTree)
		}

		// Back to no dialog, so the next round starts where this one did.
		session.clearDialog()
	}
}

// reservedValue is what the host would read off the slot the path owns. A path
// with no slot filled reads as absent, which is what the tree holds before any
// dialog has been raised.
func (s *Session) reservedValue(path string) string {
	s.reservedMu.Lock()
	defer s.reservedMu.Unlock()

	patch, waiting := s.reserved[path]
	if !waiting {
		return dialogAbsent
	}

	return string(patch.Value)
}
