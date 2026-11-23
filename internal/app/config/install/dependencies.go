package install

import (
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// What a module cannot run without and the modules it depends on, as the
// declaration records them. It is the manifest's shape kept in the file this
// server writes, so the host reads one record whether the module was installed
// by the command or over the API.

// Requirement is one thing a module cannot run without, as the declaration
// records it. Exactly one of Capability, Core and Module is set.
type Requirement struct {
	// Capability is a wire method the host must serve the instance. It is met
	// when the method is in the set the host installs for this instance.
	Capability string `json:"capability,omitempty" yaml:"capability"`
	// Core is the range of core versions the module runs on.
	Core string `json:"core,omitempty" yaml:"core"`
	// Module is the namespace of another module that must be installed and
	// switched on, with a release in Version.
	Module  string `json:"module,omitempty"  yaml:"module"`
	Version string `json:"version,omitempty" yaml:"version"`
	// Since is the contract version that introduced the requirement, and empty
	// when the module did not say.
	Since string `json:"since,omitempty" yaml:"since"`
	// Header is a request header the module marked required in routes.headers
	// (ADR-20261118-nnm). It is met when the instance's service block grants
	// the header.
	Header string `json:"header,omitempty" yaml:"header"`
}

// Dependency is the module half of the requirement, and the zero Dependency
// for a requirement on a capability or a core version.
func (r Requirement) Dependency() Dependency {
	return Dependency{Module: r.Module, Version: r.Version}
}

// Subject returns the one thing the requirement names, and the empty string
// for a requirement that names none.
func (r Requirement) Subject() string {
	switch {
	case r.Capability != "":
		return r.Capability
	case r.Module != "":
		return r.Dependency().String()
	case r.Header != "":
		return r.Header
	}

	return r.Core
}

// Describe renders one requirement as a sentence an operator can act on.
func (r Requirement) Describe() string {
	switch {
	case r.Capability != "":
		return "the host capability " + r.Capability
	case r.Module != "":
		return "the module " + r.Dependency().String()
	case r.Core != "":
		return "a core version in " + r.Core
	case r.Header != "":
		return "the request header " + r.Header
	}

	return "nothing"
}

// Dependency is one other module and a constraint on its releases, in
// Composer's grammar (manifest.ParseConstraint), as the declaration records
// it: an entry of require under Requirement, or of conflict under
// ModuleEntry.Conflicts.
type Dependency struct {
	Module  string `json:"module"  yaml:"module"`
	Version string `json:"version" yaml:"version"`
}

// String writes the dependency as an operator reads it, such as
// com.acme.threemf ^1.2.
func (d Dependency) String() string { return d.Module + " " + d.Version }

// Holds reports whether a release is in the constraint. A constraint or a
// release the grammar cannot read holds nothing. Stability is not checked
// here.
func (d Dependency) Holds(release string) bool {
	wanted, err := manifest.ParseConstraint(d.Version)
	if err != nil {
		return false
	}

	return wanted.HoldsText(release)
}

// Suggestion is one module a module works better with, and why, as the
// declaration records the manifest's suggest.
type Suggestion struct {
	Module string `json:"module"           yaml:"module"`
	Reason string `json:"reason,omitempty" yaml:"reason"`
}
