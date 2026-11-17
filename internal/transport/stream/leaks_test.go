package stream

import (
	"testing"

	"go.uber.org/goleak"
)

// The hub is the package that starts goroutines on purpose: one fan-out worker
// per lane, one pump per subscription, and one timer per resume window. Every
// one of them is meant to end when its subscription or its hub closes, and a
// test that closed neither would still pass, because a goroutine nobody is
// waiting for is invisible to the assertions.
//
// This is what makes it visible. The suite fails when a goroutine this package
// started is still running after the last case.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
