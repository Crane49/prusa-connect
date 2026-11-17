package moduledeps

import (
	"slices"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
)

// CoreImpact is the plan a core upgrade to the candidate version needs: the
// core is a platform package, as php is in Composer, and the upgrade is one
// update with the modules that need it.
//
// A module whose core constraint does not hold the candidate is resolved
// again, and so is every module that requires one of those, all the way up.
// Each keeps its installed release when that still fits and takes the newest
// one that does otherwise. A module no release fits refuses the upgrade with
// MODULE_DEPENDENCY_CONFLICT, which names it and the core. An empty plan is
// an upgrade every installed module already allows.
func CoreImpact(
	installed []Installed,
	available []Release,
	lock Lock,
	candidate string,
	settings Settings,
) (Plan, error) {
	return CoreUpgrade(Request{
		Installed: installed, Available: available, Lock: lock, Settings: settings,
	}, candidate)
}

// CoreUpgrade is CoreImpact for an upgrade that also brings releases of its
// own, the extensions a signed offline bundle carries: they are added as an
// install adds a release, and every installed module the candidate core or
// those releases leave without a fitting release is resolved again with
// them. Installed, Available, Lock, Add and Settings are read from the
// request; Settings.Core becomes the candidate.
func CoreUpgrade(request Request, candidate string) (Plan, error) {
	request.Settings.Core = candidate

	broken := CoreBroken(request.Installed, candidate)
	if len(broken) == 0 && len(request.Add) == 0 {
		return Plan{}, nil
	}

	request.Update = append(slices.Clone(request.Update), broken...)
	request.KeepInstalled = true
	request.Available = withInstalled(request.Available, request.Installed)

	return Resolve(request)
}

// withInstalled offers every installed release beside what the index offers.
// A module the upgrade resolves again keeps the release it has when that
// still fits, and an offline server, which reads no index, has nothing else
// to keep it with.
func withInstalled(available []Release, installed []Installed) []Release {
	out := slices.Clone(available)

	for _, one := range installed {
		if slices.ContainsFunc(out, func(offered Release) bool {
			return offered.Namespace == one.Namespace && offered.Version == one.Version
		}) {
			continue
		}

		out = append(out, Release{
			Module: one.Path, Namespace: one.Namespace, Version: one.Version, Source: one.Source,
			Digest: one.Digest, Core: one.Core, Require: one.Require, Conflict: one.Conflict,
		})
	}

	return out
}

// CoreBroken is every installed module a core upgrade to the candidate leaves
// without its core constraint, and every module that requires one of those,
// all the way up, sorted. They are the modules the upgrade resolves again.
func CoreBroken(installed []Installed, candidate string) []string {
	broken := map[string]bool{}

	for _, one := range installed {
		if one.Core != "" && !(Release{Namespace: one.Namespace, Core: one.Core}).coreHolds(candidate) {
			broken[one.Namespace] = true
		}
	}

	for moved := len(broken) > 0; moved; {
		moved = false

		for _, one := range installed {
			if broken[one.Namespace] {
				continue
			}

			if slices.ContainsFunc(one.Require, func(dependency install.Dependency) bool {
				return broken[dependency.Module]
			}) {
				broken[one.Namespace] = true
				moved = true
			}
		}
	}

	out := make([]string, 0, len(broken))
	for namespace := range broken {
		out = append(out, namespace)
	}

	slices.Sort(out)

	return out
}

// coreHolds reports whether the release's core constraint holds a core
// version.
func (r Release) coreHolds(core string) bool {
	return (&resolver{settings: Settings{Core: core}}).platformOf(r) == ""
}
