package runner

import (
	"os/exec"
	"sync"
	"time"

	"github.com/prusa3d/server-slicing/internal/sandbox"
)

// watch reads the memory of a started command's process group until the
// returned function is called, and stops the group when it holds more than
// limit bytes. The function answers the most the group held and whether it
// was stopped for it. A limit of zero only measures.
func watch(command *exec.Cmd, limit int64) func() (int64, bool) {
	group := sandbox.Group(command)
	done := make(chan struct{})

	var (
		mu   sync.Mutex
		peak int64
		over bool
		wait sync.WaitGroup
	)

	wait.Go(func() {
		ticker := time.NewTicker(watchEvery)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}

			held, err := groupMemory(group)
			if err != nil {
				continue
			}

			mu.Lock()
			peak = max(peak, held)
			stop := limit > 0 && held > limit && !over
			over = over || stop
			mu.Unlock()

			if stop {
				_ = sandbox.Stop(command) // the run already fails for its memory; a group that is gone is fine
			}
		}
	})

	return func() (int64, bool) {
		close(done)
		wait.Wait()

		mu.Lock()
		defer mu.Unlock()

		return peak, over
	}
}
