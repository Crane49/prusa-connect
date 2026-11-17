//go:build !windows

package procgroup_test

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/procgroup"
)

func TestKillEndsTheWholeGroup(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 300 & echo $!; sleep 300")
	procgroup.Prepare(cmd)

	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}

	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}

	background, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}

	group, err := procgroup.Attach(cmd)
	if err != nil {
		t.Fatal(err)
	}

	if err = group.Kill(); err != nil {
		t.Fatal(err)
	}

	_ = cmd.Wait() // killed, so it reports the signal.

	for _, pid := range []int{cmd.Process.Pid, background} {
		deadline := time.Now().Add(2 * time.Second)
		for !ended(pid) {
			if time.Now().After(deadline) {
				t.Fatalf("process %d is still running 2 s after Kill", pid)
			}

			time.Sleep(20 * time.Millisecond)
		}
	}
}

// ended reports whether pid has gone. An orphan zombie counts as gone: under
// Docker its new parent may be a pid 1 that never collects it.
func ended(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return true
	}

	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")

	return err == nil && strings.Contains(string(stat), ") Z ")
}
