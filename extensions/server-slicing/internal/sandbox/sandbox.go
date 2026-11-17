// Package sandbox starts the slicer the same way every time: its own process
// group, an environment built from a short list rather than inherited whole,
// and a home directory of its own, so it reads no profile of the person the
// server runs as and writes nothing outside the job's directory.
package sandbox

import (
	"os"
	"os/exec"
	"runtime"
	"time"
)

// WaitDelay is how long a stopped slicer may hold its output pipes open
// before Wait gives up on them.
const WaitDelay = 5 * time.Second

// passed are the variables a slicer receives from the server's own
// environment. Everything else, a credential among it, stays behind.
var passed = []string{"PATH", "SystemRoot", "SYSTEMDRIVE", "WINDIR", "PATHEXT", "COMSPEC"}

// Env is the environment of one slicer run whose home is home. The locale is
// C, so a decimal is always written with a point.
func Env(home string) []string {
	out := []string{
		"HOME=" + home, "USERPROFILE=" + home, "TMPDIR=" + home, "TEMP=" + home, "TMP=" + home,
		"XDG_CONFIG_HOME=" + home, "XDG_CACHE_HOME=" + home, "XDG_DATA_HOME=" + home,
		"LC_ALL=C", "LANG=C",
	}

	for _, name := range passed {
		if value, set := os.LookupEnv(name); set {
			out = append(out, name+"="+value)
		}
	}

	if runtime.GOOS == "darwin" {
		// The macOS builds read no display, and this keeps them from asking
		// for one.
		out = append(out, "__CFPREFERENCES_AVOID_DAEMON=1")
	}

	return out
}

// Prepare puts a command in a process group of its own and makes a cancelled
// context stop the whole group, so a slicer that started helpers takes them
// down with it.
func Prepare(command *exec.Cmd) {
	group(command)

	command.Cancel = func() error { return stop(command) }
	command.WaitDelay = WaitDelay
}

// Stop ends a started command and every process in its group.
func Stop(command *exec.Cmd) error { return stop(command) }
