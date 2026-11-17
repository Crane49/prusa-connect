package resourcehost

import (
	"strconv"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/wire"
)

// gomemlimitPercent is the share of the budget a Go module's runtime aims to
// stay under, so its collector works harder before the watchdog kills it.
const gomemlimitPercent = 85

// childEnv is the environment a module's process starts with: the allowed
// variables of host, then, when the module has a budget, GOMEMLIMIT and the
// budget itself. No other variable of the server reaches the module, and the
// server's own GOMEMLIMIT does not either.
func childEnv(host []string, budget int64, windows bool) []string {
	var out []string

	for _, entry := range host {
		name, _, _ := strings.Cut(entry, "=")
		if allowedEnv(name, windows) {
			out = append(out, entry)
		}
	}

	if budget > 0 {
		out = append(out,
			"GOMEMLIMIT="+strconv.FormatInt(budget*gomemlimitPercent/100, 10),
			wire.MemoryBudgetEnv+"="+strconv.FormatInt(budget, 10))
	}

	return out
}

// allowedEnv reports whether a variable passes to a module. The name is
// compared upper-cased, because Windows spells PATH as Path and the proxy
// variables are read in either case. On Windows os.TempDir and
// os.UserHomeDir read the profile variables, so those pass there too.
func allowedEnv(name string, windows bool) bool {
	upper := strings.ToUpper(name)

	switch upper {
	case "PATH", "HOME", "TZ", "LANG", "TMPDIR", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY":
		return true
	case "TEMP", "TMP", "USERPROFILE", "APPDATA", "LOCALAPPDATA":
		return windows
	}

	return strings.HasPrefix(upper, "LC_")
}
