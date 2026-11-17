package procgroup_test

import (
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/prusa3d/prusa-connect/internal/kernel/procgroup"
)

func TestTheReaperLeavesAnOwnedChildToWait(t *testing.T) {
	reaper := procgroup.NewReaper()

	run(t, reaper)

	for run := range 50 {
		cmd := exec.Command("true")
		procgroup.Prepare(cmd) // a module leader, outside this process's group.

		release, err := reaper.Start(cmd)
		if err != nil {
			t.Fatal(err)
		}

		err = cmd.Wait()
		release()

		if err != nil || cmd.ProcessState.ExitCode() != 0 {
			t.Fatalf("run %d: Wait() = %v, exit code %d", run, err, cmd.ProcessState.ExitCode())
		}
	}
}

func TestTheReaperCollectsAnOrphan(t *testing.T) {
	reaper := procgroup.NewReaper()

	var subreaper int
	if err := unix.Prctl(
		unix.PR_GET_CHILD_SUBREAPER,
		uintptr(unsafe.Pointer(&subreaper)),
		0,
		0,
		0,
	); err != nil ||
		subreaper == 0 {
		t.Skip("this process is not a subreaper")
	}

	run(t, reaper)

	cmd := exec.Command("sh", "-c", "sleep 0.2 & echo $!")
	procgroup.Prepare(cmd) // the orphan inherits a group other than this process's.

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	release, err := reaper.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}

	out, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}

	err = cmd.Wait()
	release()

	if err != nil {
		t.Fatal(err)
	}

	orphan, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		zombies := zombieChildren(t)
		if !exists(orphan) && len(zombies) == 0 {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("2 s after the orphan exited, zombie children remain: %v", zombies)
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func exists(pid int) bool {
	_, err := os.Stat("/proc/" + strconv.Itoa(pid))

	return err == nil
}

func zombieChildren(t *testing.T) []int {
	t.Helper()

	self := strconv.Itoa(os.Getpid())

	var zombies []int

	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		data, readErr := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if readErr != nil {
			continue
		}

		stat := string(data)
		fields := strings.Fields(stat[strings.LastIndexByte(stat, ')')+1:])

		if len(fields) > 1 && fields[0] == "Z" && fields[1] == self {
			pid, _ := strconv.Atoi(entry.Name())
			zombies = append(zombies, pid)
		}
	}

	return zombies
}

// run runs reaper until the test ends and waits for it to stop, so it cannot
// collect the children of a later test.
func run(t *testing.T, reaper *procgroup.Reaper) {
	t.Helper()

	stopped := make(chan struct{})

	go func() {
		reaper.Run(t.Context())
		close(stopped)
	}()

	t.Cleanup(func() { <-stopped }) // t.Context is cancelled before cleanups run.
}
