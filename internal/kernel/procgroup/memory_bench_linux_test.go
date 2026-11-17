package procgroup_test

import (
	"os/exec"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/kernel/procgroup"
)

// BenchmarkMemoryWalk reads the memory of a small group over the real /proc,
// so its cost grows with every other process on the machine.
func BenchmarkMemoryWalk(b *testing.B) {
	cmd := exec.Command("sh", "-c", "sleep 300 & sleep 300")
	procgroup.Prepare(cmd)

	if err := cmd.Start(); err != nil {
		b.Fatal(err)
	}

	group, err := procgroup.Attach(cmd)
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() {
		_ = group.Kill()
		_ = cmd.Wait() // killed, so it reports the signal.
	})

	for b.Loop() {
		if _, err = group.Memory(); err != nil {
			b.Fatal(err)
		}
	}
}
