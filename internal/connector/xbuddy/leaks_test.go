package xbuddy

import (
	"testing"

	"go.uber.org/goleak"
)

// A session is four goroutines and a connection is three more, and every one of
// them ends on a channel this package closes. A case that closed neither the
// session nor the link would still pass, because a goroutine nobody is waiting
// on is invisible to the assertions, and the leak would show up in a fleet of
// four hundred printers reconnecting rather than here.
//
// This is what makes it visible. The suite fails when a goroutine this package
// started is still running after the last case.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
