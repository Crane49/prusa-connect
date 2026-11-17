package procgroup_test

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/procgroup"
)

const (
	helperEnv   = "PROCGROUP_TEST_HELPER"
	allocated   = 32 << 20
	helperSleep = 300 * time.Second
	orphaned    = "orphaned"
)

// TestMain lets the test binary act as the helper processes of
// TestMemoryCountsADescendantThatLeftTheGroup and
// TestKillReachesAnOrphanInItsOwnGroup.
func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "orphaner":
		// sh starts the child in the background and exits, so the child keeps
		// the group but loses its parent link to the leader.
		sh := exec.Command("sh", "-c", `"$0" &`, os.Args[0])
		sh.Env = append(os.Environ(), helperEnv+"=child")
		sh.Stdout = os.Stdout

		if err := sh.Run(); err != nil {
			os.Exit(1)
		}

		_, _ = fmt.Fprintln(os.Stdout, orphaned) // the test reads it from the pipe.
		time.Sleep(helperSleep)
		os.Exit(0)
	case "parent":
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), helperEnv+"=child")
		child.Stdout = os.Stdout
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Pdeathsig: syscall.SIGKILL}

		if err := child.Start(); err != nil {
			os.Exit(1)
		}

		time.Sleep(helperSleep)
		os.Exit(0)
	case "mover":
		// The child moves to a group of its own, and this leader exits at once.
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), helperEnv+"=child")
		child.Stdout = os.Stdout
		child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		if err := child.Start(); err != nil {
			os.Exit(1)
		}

		os.Exit(0)
	case "child":
		memory := make([]byte, allocated)
		for i := range memory {
			memory[i] = 1
		}

		_, _ = fmt.Fprintln(os.Stdout, os.Getpid()) // the test reads it from the pipe.
		time.Sleep(helperSleep)
		runtime.KeepAlive(memory)
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func TestMemoryCountsADescendantThatLeftTheGroup(t *testing.T) {
	// lines is how many lines the leader writes: the child's pid, and for the
	// orphaner a line once the intermediate shell has exited.
	for _, tc := range []struct {
		name, mode string
		lines      int
	}{
		{"setsid child of a living parent", "parent", 1},
		{"group member whose parent exited", "orphaner", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0])
			cmd.Env = append(os.Environ(), helperEnv+"="+tc.mode)
			procgroup.Prepare(cmd)

			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}

			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}

			group, err := procgroup.Attach(cmd)
			if err != nil {
				t.Fatal(err)
			}

			child := 0
			reader := bufio.NewReader(out)

			for range tc.lines {
				line, readErr := reader.ReadString('\n')
				if readErr != nil {
					t.Fatal(readErr)
				}

				if line = strings.TrimSpace(line); line != orphaned {
					if child, err = strconv.Atoi(line); err != nil {
						t.Fatal(err)
					}
				}
			}

			t.Cleanup(func() {
				_ = syscall.Kill(child, syscall.SIGKILL) // a setsid child is outside the group, so Kill misses it.
				_ = group.Kill()                         // the test has finished with it.
				_ = cmd.Wait()                           // killed, so it reports the signal.
			})

			got, err := group.Memory()
			if err != nil {
				t.Fatal(err)
			}

			if got < allocated {
				t.Fatalf("Memory() = %d, want at least %d", got, allocated)
			}
		})
	}
}

func TestMemoryOfAnUnwaitedLeaderIsAnError(t *testing.T) {
	cmd := exec.Command("sleep", "300")
	procgroup.Prepare(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = cmd.Wait() }) // killed, so it reports the signal.

	group, err := procgroup.Attach(cmd)
	if err != nil {
		t.Fatal(err)
	}

	if err = group.Kill(); err != nil {
		t.Fatal(err)
	}

	stat := "/proc/" + strconv.Itoa(cmd.Process.Pid) + "/stat"
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		data, readErr := os.ReadFile(stat)
		if readErr == nil && strings.Contains(string(data), ") Z ") {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("the killed leader did not become a zombie within 2 s")
		}
	}

	if got, memErr := group.Memory(); memErr == nil {
		t.Fatalf("Memory() = %d, nil; want an error", got)
	}
}

func TestMemoryOfAGoneGroupIsAnError(t *testing.T) {
	cmd := exec.Command("sleep", "300")
	procgroup.Prepare(cmd)

	if err := cmd.Start(); err != nil {
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

	if got, memErr := group.Memory(); memErr == nil {
		t.Fatalf("Memory() = %d, nil; want an error", got)
	}
}
