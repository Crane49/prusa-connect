package install

import (
	"runtime/debug"
	"strings"

	"github.com/prusa3d/prusa-connect/internal/kernel/version"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// develVersion is what Go records for a main module built from a directory
// with no version control information, such as a release source archive.
const develVersion = "(devel)"

// CoreVersion is this build's own version. A release build stamps it with the
// linker, and that stamp wins. Otherwise it is read from the module graph the
// linker embedded. A composed build carries the core as a dependency, and a
// checkout replaced by path carries no version.
//
// An empty answer means the build reports none. A caller that holds a module
// to a core range skips the check then, so a development build can still run
// modules.
//
// It sits here because the install command, the supervision plane and the
// version the server reports all read it, so the three agree.
func CoreVersion() string {
	if stamp := version.Stamped(); stamp != "" {
		return stamp
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}

	if info.Main.Path == extension.CoreModulePath {
		return graphVersion(info.Main.Version)
	}

	for _, dependency := range info.Deps {
		if dependency.Path == extension.CoreModulePath {
			return graphVersion(dependency.Version)
		}
	}

	return ""
}

// graphVersion drops the leading "v" of a module version, and answers empty
// for the placeholder Go writes when it knows no version.
func graphVersion(recorded string) string {
	if recorded == develVersion {
		return ""
	}

	return strings.TrimPrefix(recorded, "v")
}

// CoreHolds reports whether the running core is in a module's constraint on
// the core platform package, in Composer's grammar. readable is false when the
// running version is not a version, and the caller then skips the check.
func CoreHolds(constraint, running string) (in bool, readable bool) {
	held, err := manifest.ParseRelease(running)
	if err != nil {
		return false, false
	}

	wanted, err := manifest.ParseConstraint(constraint)
	if err != nil {
		return false, true
	}

	return wanted.HoldsCore(held), true
}
