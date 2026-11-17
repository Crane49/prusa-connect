// Package hostfeature is what a build of this host promises a module, as keys
// in the capability vocabulary.
//
// A module names what it cannot run without in the `requires` list of its
// manifest. Most of that list is other modules; a feature is the part the host
// itself provides. A host that lacks a named key refuses the module instead of
// starting it and failing later.
//
// One table says which keys exist and which of them this binary carries. Those
// differ: a build without the browser application has no UI to extend, so a
// module asking for the slots would find nothing to fill.
package hostfeature

import (
	"slices"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// UI is the key a module names when it fills a slot or adds a page of its own.
// Only the full build promises it: that build compiles in the core's browser
// application, and the slots belong to the application.
const UI = connectorapi.Capability("core.feature.ui")

// Feature is one key this host may promise.
type Feature struct {
	// Key is the capability key, which is what a manifest writes.
	Key connectorapi.Capability
	// Summary is what the key means, in one sentence.
	Summary string
	// Builds says which builds carry it, for the published vocabulary.
	Builds string
	// FullBuildOnly marks a key the full build carries and no other.
	FullBuildOnly bool
}

// declared is every host feature key that exists, whatever this build carries.
// It is a closed table: a key is added here and nowhere else, and
// `make capabilities-check` publishes it.
//
//nolint:gochecknoglobals // a closed table, read but never written.
var declared = []Feature{
	{
		Key: UI,
		Summary: "The host serves a browser application a module can extend: six slots it can fill, " +
			"across the app, a printer's page, the printer listing and the settings screen",
		Builds:        "the full build",
		FullBuildOnly: true,
	},
}

// Declared is every host feature key, whatever this build carries. The
// published vocabulary is rendered from it, so the page describes the product
// and not the binary that generated it.
func Declared() []Feature { return slices.Clone(declared) }

// Advertised is the subset this build carries. It is what a module's
// requirement is answered against, and what the running server publishes.
func Advertised() []Feature {
	out := make([]Feature, 0, len(declared))

	for _, feature := range declared {
		if feature.FullBuildOnly && !fullBuild {
			continue
		}

		out = append(out, feature)
	}

	return out
}
