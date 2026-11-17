package procgroup_test

import (
	"bufio"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/prusa3d/prusa-connect/internal/kernel/procgroup"
)

// TestAChildDoesNotInheritThePortRight raises CAP_NET_BIND_SERVICE in the
// ambient set, which works only where the process holds it as permitted and
// inheritable, as under the shipped systemd unit. A child started after
// KeepPortRightFromChildren then holds no ambient capability.
func TestAChildDoesNotInheritThePortRight(t *testing.T) {
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_RAISE, unix.CAP_NET_BIND_SERVICE, 0, 0); err != nil {
		t.Skipf("this process may not raise CAP_NET_BIND_SERVICE: %v", err)
	}

	if got := childAmbient(t); got == "0000000000000000" {
		t.Fatal("the raised capability did not reach the child, so the test proves nothing")
	}

	procgroup.KeepPortRightFromChildren()

	if got := childAmbient(t); got != "0000000000000000" {
		t.Fatalf("a child holds CapAmb %s, want none", got)
	}

	procgroup.PassPortRightToExec()

	if got := childAmbient(t); got == "0000000000000000" {
		t.Fatal("the right was not raised again for an exec")
	}

	procgroup.KeepPortRightFromChildren()
}

// childAmbient is the CapAmb line of a child's /proc status.
func childAmbient(t *testing.T) string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "cat", "/proc/self/status").Output()
	if err != nil {
		t.Fatal(err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		if value, found := strings.CutPrefix(scanner.Text(), "CapAmb:"); found {
			return strings.TrimSpace(value)
		}
	}

	t.Fatal("no CapAmb line")

	return ""
}
