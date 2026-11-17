package moduledeps

import (
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The questions Composer's why, why-not and outdated answer, asked of the
// modules a server has.

// Reason is one answer of Why and WhyNot: a module, or the operator, and
// what it says about the module asked about.
type Reason struct {
	// By is the namespace that says it, or "the operator".
	By string `json:"by"`
	// Constraint is the constraint it puts, empty for a reason that is not
	// one.
	Constraint string `json:"constraint,omitempty"`
	// Text is the reason as an operator reads it.
	Text string `json:"text"`
}

// Why is everything that keeps a module installed: the operator, when they
// asked for it, and every installed module that requires it.
func Why(installed []Installed, namespace string) []Reason {
	var out []Reason

	for _, one := range installed {
		if one.Namespace == namespace && !one.Pulled {
			out = append(out, Reason{By: root, Constraint: one.Constraint, Text: "the operator installed it"})
		}

		for _, dependency := range one.Require {
			if dependency.Module == namespace {
				out = append(out, Reason{
					By: one.Namespace, Constraint: dependency.Version,
					Text: one.Namespace + " " + one.Version + " requires " + dependency.String(),
				})
			}
		}
	}

	return out
}

// WhyNot is everything that refuses one release of a module on this server:
// installed modules whose require does not hold it or whose conflict does,
// the release's own core constraint, require and conflict when the index
// offers it, and its stability. An empty answer is a release nothing refuses.
func WhyNot(installed []Installed, available []Release, namespace, version string, settings Settings) []Reason {
	var out []Reason

	for _, one := range installed {
		for _, dependency := range one.Require {
			if dependency.Module == namespace && !dependency.Holds(version) {
				out = append(out, Reason{By: one.Namespace, Constraint: dependency.Version,
					Text: one.Namespace + " " + one.Version + " requires " + dependency.String()})
			}
		}

		for _, dependency := range one.Conflict {
			if dependency.Module == namespace && dependency.Holds(version) {
				out = append(out, Reason{By: one.Namespace, Constraint: dependency.Version,
					Text: one.Namespace + " " + one.Version + " conflicts with " + dependency.String()})
			}
		}
	}

	at := slices.IndexFunc(available, func(release Release) bool {
		return release.Namespace == namespace && release.Version == version
	})
	if at < 0 {
		return append(out, Reason{By: namespace, Text: "no index offers " + namespace + " " + version})
	}

	release := available[at]

	if refused := (&resolver{settings: settings}).platformOf(release); refused != "" {
		out = append(out, Reason{By: namespace, Constraint: release.Core, Text: refused})
	}

	if release.Stability() < settings.minimum() {
		out = append(out, Reason{By: namespace, Text: namespace + " " + version + " is " +
			release.Stability().String() + ", and the minimum stability is " + settings.minimum().String()})
	}

	return append(out, ownReasons(installed, release)...)
}

// ownReasons is a release's own require and conflict against the installed
// modules.
func ownReasons(installed []Installed, release Release) []Reason {
	var out []Reason

	for _, dependency := range release.Require {
		// A module the server does not have is one an install adds, and
		// refuses nothing by itself.
		held, found := lockedOf(installed, dependency.Module)
		if !found || dependency.Holds(held.Version) {
			continue
		}

		out = append(out, Reason{By: release.Namespace, Constraint: dependency.Version,
			Text: release.Namespace + " " + release.Version + " requires " + dependency.String() + ", and " +
				dependency.Module + " is " + held.Version})
	}

	for _, dependency := range release.Conflict {
		if held, found := lockedOf(installed, dependency.Module); found && dependency.Holds(held.Version) {
			out = append(out, Reason{By: release.Namespace, Constraint: dependency.Version,
				Text: release.Namespace + " " + release.Version + " conflicts with " + dependency.String() +
					", and " + dependency.Module + " is " + held.Version})
		}
	}

	return out
}

// Outdated is one installed module against the index.
type Outdated struct {
	Namespace string `json:"namespace"`
	Path      string `json:"path,omitempty"`
	Installed string `json:"installed"`
	// Allowed is the newest release every installed module, the operator's
	// constraint and the stabilities allow, and Latest the newest release
	// the stabilities allow. Each is empty when the index offers none.
	Allowed string `json:"allowed,omitempty"`
	Latest  string `json:"latest,omitempty"`
	// Status is up-to-date, update-possible when Allowed is newer than the
	// installed release, or blocked when only Latest is.
	Status string `json:"status"`
}

// OutdatedOf is every installed module against the index, as Composer's
// outdated answers.
func OutdatedOf(installed []Installed, available []Release, settings Settings) []Outdated {
	out := make([]Outdated, 0, len(installed))

	for _, one := range installed {
		row := Outdated{Namespace: one.Namespace, Path: one.Path, Installed: one.Version, Status: "up-to-date"}

		for _, release := range available {
			if release.Namespace != one.Namespace || release.Stability() < settings.minimum() {
				continue
			}

			if row.Latest == "" || compareVersions(release.Version, row.Latest) > 0 {
				row.Latest = release.Version
			}

			if len(WhyNot(installed, available, one.Namespace, release.Version, settings)) == 0 && rootHolds(one,
				release.Version) && (row.Allowed == "" || compareVersions(release.Version, row.Allowed) > 0) {
				row.Allowed = release.Version
			}
		}

		switch {
		case row.Allowed != "" && compareVersions(row.Allowed, one.Version) > 0:
			row.Status = "update-possible"
		case row.Latest != "" && compareVersions(row.Latest, one.Version) > 0:
			row.Status = "blocked"
		}

		out = append(out, row)
	}

	return out
}

// rootHolds reports whether the operator's constraint on a module holds a
// release.
func rootHolds(one Installed, version string) bool {
	if one.Pulled || strings.TrimSpace(one.Constraint) == "" {
		return true
	}

	held, err := manifest.ParseConstraint(one.Constraint)

	return err == nil && held.HoldsText(version)
}
