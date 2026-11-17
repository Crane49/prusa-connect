// Package moduledeps resolves which releases of modules a server installs,
// the way Composer resolves packages.
//
// It is the one resolver every caller asks: an install that adds a module, an
// update of some modules or all of them, an install from the lock file, and a
// core upgrade, which is the update of the core platform package with the
// modules that need it updated beside it. It reads nothing from disk except
// the lock, and writes nothing except the lock: a caller hands it what is
// installed and what the index offers, and gets a plan back.
//
//	plan, err := moduledeps.Resolve(moduledeps.Request{
//		Installed: moduledeps.InstalledOf(declarations),
//		Available: releases,
//		Lock:      lock,
//		Add:       []moduledeps.Release{target},
//		Settings:  settings,
//	})
//
// The vocabulary is Composer's: require, suggest, conflict, a constraint, a
// stability, minimum-stability, prefer-stable, a lock with a content hash, a
// platform package for the core.
package moduledeps

import (
	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// Release is one release of one module an index offers.
type Release struct {
	// Module is the module path, which an index row names it by.
	Module string
	// Namespace is the namespace derived from the module path, which every
	// dependency names.
	Namespace string
	// Version is the release.
	Version string
	// Channel is the marketplace channel, beta or stable, and empty for a
	// release from no marketplace. A beta channel marks a release with no
	// prerelease in its version as beta.
	Channel string
	// Source is where the release's bytes are, and Digest their sha256.
	Source string
	Digest string
	// Core is the constraint the release puts on the core platform package.
	Core string
	// Require and Conflict are the release's require and conflict on other
	// modules.
	Require  []install.Dependency
	Conflict []install.Dependency
	// Extra is the caller's own data about the release, such as the index
	// row it came from. The resolver passes it through untouched.
	Extra any
}

// Stability is the release's stability: from the prerelease of its version,
// or beta for a release a marketplace publishes on its beta channel.
func (r Release) Stability() manifest.Stability {
	held, err := manifest.ParseRelease(r.Version)
	if err != nil {
		return manifest.StabilityDev
	}

	if held.Stability == manifest.StabilityStable && r.Channel == "beta" {
		return manifest.StabilityBeta
	}

	return held.Stability
}

// Installed is one module a server has.
type Installed struct {
	Namespace string
	// Path is the module path it was installed under, empty when the record
	// holds none.
	Path    string
	Version string
	Source  string
	Digest  string
	// Core is the constraint the release puts on the core.
	Core     string
	Require  []install.Dependency
	Conflict []install.Dependency
	// Pulled is true for a module an install added for another module.
	Pulled bool
	// Constraint is what the operator asked for when they installed the
	// module themselves, and empty for a pulled one.
	Constraint string
}

// Settings is how the resolver chooses among releases.
type Settings struct {
	// Core is the version of the core platform package: the running server,
	// or a candidate a core upgrade is planned for. Empty skips every check
	// on the core, as a development build does.
	Core string
	// MinimumStability is the lowest stability taken: dev, alpha, beta, RC
	// or stable. Empty is stable.
	MinimumStability string
	// PreferStable takes the most stable release a constraint allows before
	// the newest one.
	PreferStable bool
}

// minimum is the minimum stability, stable when it cannot be read.
func (s Settings) minimum() manifest.Stability {
	held, err := manifest.ParseStability(s.MinimumStability)
	if err != nil {
		return manifest.StabilityStable
	}

	return held
}

// Request is one resolution.
type Request struct {
	// Installed is what the server has.
	Installed []Installed
	// Available is every release the index offers.
	Available []Release
	// Lock names the releases resolved last time. A module the request does
	// not update keeps the release the lock names when it still fits.
	Lock Lock
	// Add is releases the operator chose, such as the one an install names.
	// They are taken as they are, and replace an installed release of the
	// same module.
	Add []Release
	// Require is constraints the operator puts on modules, by namespace, as
	// `module add path@constraint` writes one. A module named here is chosen
	// from Available.
	Require map[string]string
	// Update names the installed modules to resolve again, to the newest
	// release every constraint allows. UpdateAll resolves every one.
	Update    []string
	UpdateAll bool
	// WithDependencies also resolves again the modules an updated module
	// requires that an install added for it.
	WithDependencies bool
	// KeepInstalled makes a module resolved again keep its installed release
	// when that still fits, which a core upgrade wants and an update does
	// not.
	KeepInstalled bool
	Settings      Settings
}

// Step is one release a plan installs.
type Step struct {
	Release Release
	// From is the release it replaces, and empty for a module the server
	// does not have.
	From string
	// RequiredBy is who requires the module, each as module constraint.
	RequiredBy []string
	// Pulled is true for a module the plan adds for another module.
	Pulled bool
}

// Plan is the releases a resolution installs, in the order they are written:
// a module before the modules that require it.
type Plan struct {
	Steps []Step
}

// root is who stands behind a constraint the operator wrote.
const root = "the operator"
