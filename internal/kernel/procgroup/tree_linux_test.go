package procgroup_test

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/prusa3d/prusa-connect/internal/kernel/procgroup"
)

func TestKillReachesADescendantInItsOwnGroup(t *testing.T) {
	// The shell runs in the background with no job control, so setsid is not
	// a group leader, does not fork, and $! is the grandchild's pid.
	cmd := exec.Command("sh", "-c", `setsid sleep 300 & echo $!; exec sleep 300`)
	group, grandchild := startTree(t, cmd)

	if err := group.Kill(); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "the grandchild to be gone", func() bool {
		return errors.Is(syscall.Kill(grandchild, 0), syscall.ESRCH)
	})
}

// The stop timeout, the watchdog and cancellation kill a live leader. This
// test is the other case: the leader has already exited, so its children have
// lost their parent link before the kill.
func TestKillReachesAnOrphanInItsOwnGroup(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), helperEnv+"=mover")
	group, child := startTree(t, cmd)

	waitFor(t, "the leader to exit", func() bool {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/stat")

		return err == nil && strings.Contains(string(data), ") Z ")
	})

	if err := group.Kill(); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "the child to be gone", func() bool {
		return errors.Is(syscall.Kill(child, 0), syscall.ESRCH)
	})
}

// startTree starts cmd as a module under a running Reaper, reads the pid of a
// descendant from the first line cmd writes, and waits until that descendant
// leads a group of its own. The Reaper collects the descendant once it is
// killed; without it the descendant stays a zombie of init and never reports
// ESRCH.
func startTree(t *testing.T, cmd *exec.Cmd) (*procgroup.Group, int) {
	t.Helper()

	reaper := procgroup.NewReaper()

	var subreaper int
	if err := unix.Prctl(unix.PR_GET_CHILD_SUBREAPER, uintptr(unsafe.Pointer(&subreaper)), 0, 0, 0); err != nil ||
		subreaper == 0 {
		t.Skip("this process is not a subreaper, so a killed orphan would stay a zombie")
	}

	run(t, reaper)
	procgroup.Prepare(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	release, err := reaper.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}

	group, err := procgroup.Attach(cmd)
	if err != nil {
		t.Fatal(err)
	}

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}

	descendant, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = syscall.Kill(descendant, syscall.SIGKILL) // in case Kill missed it.
		_ = group.Kill()
		_ = cmd.Wait() // killed, so it reports the signal.
		release()
	})

	waitFor(t, "the descendant to lead its own group", func() bool {
		pgid, pgidErr := syscall.Getpgid(descendant)

		return pgidErr == nil && pgid == descendant
	})

	return group, descendant
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()

	for deadline := time.Now().Add(2 * time.Second); !done(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("waited 2 s for %s", what)
		}
	}
}
