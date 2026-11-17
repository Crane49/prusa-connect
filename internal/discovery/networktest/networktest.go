// Package networktest decides whether a test may use the real local network.
//
// A test that joins the mDNS group, or binds an address another machine can
// reach, calls Require first. On Windows every new test binary that does so
// makes the firewall ask the person at the keyboard to allow it, so a
// developer run leaves such tests out. CI sets the variable on its Linux
// runners, and scripts/ci-local.sh sets it when it runs on Linux.
package networktest

import (
	"os"
	"testing"
)

// Env is the variable that lets a test use the real local network. Only the
// value 1 does.
const Env = "PCL_NETWORK_TESTS"

// Require skips the calling test unless PCL_NETWORK_TESTS is 1, and says how
// to run it.
func Require(t testing.TB) {
	t.Helper()

	if os.Getenv(Env) != "1" {
		t.Skipf("this test uses the real local network; run it with %s=1", Env)
	}
}
