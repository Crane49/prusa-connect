package resourcehost

import (
	"fmt"
	"os"
	"strconv"
)

// moduleOOMScore is the oom_score_adj of a module's process, so the kernel
// kills a module before the server when memory runs out. Raising the score
// needs no privilege.
const moduleOOMScore = "800"

// raiseOOMScore writes moduleOOMScore for pid.
func raiseOOMScore(pid int) error {
	path := "/proc/" + strconv.Itoa(pid) + "/oom_score_adj"

	if err := os.WriteFile(path, []byte(moduleOOMScore), 0); err != nil {
		return fmt.Errorf("raise the oom score of %d: %w", pid, err)
	}

	return nil
}
