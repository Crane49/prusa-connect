package wire

import (
	"os"
	"strconv"
)

// MemoryBudgetEnv names the environment variable in which the host tells a
// module its memory budget, in bytes.
const MemoryBudgetEnv = "PCL_MEMORY_BUDGET_BYTES"

// MemoryBudget returns the memory budget the host set for this process, in
// bytes. ok is false when the variable is unset or not a positive integer, as
// in a module started by hand.
func MemoryBudget() (bytes int64, ok bool) {
	n, err := strconv.ParseInt(os.Getenv(MemoryBudgetEnv), 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}

	return n, true
}
