// Package builtin holds the resource types this binary registers for itself.
//
// A type registers from an init function, because registration has to happen
// before main. The composition root reaches it through a blank import. A type
// an operator installs arrives the same way, from its own module.
package builtin

import (
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// TypeFilesystem is the registered name of the built-in storage type, as an
// operator writes it in the resources block.
const TypeFilesystem = "filesystem"

//nolint:gochecknoinits // registration has to happen before main; see the package comment.
func init() {
	registry.Register(ref.KindStorage, TypeFilesystem, filesystemDoc(), newFilesystem)
}

// filesystemDoc is what the published plugin reference says about the built-in
// storage type.
func filesystemDoc() extension.Doc {
	return extension.Doc{
		Summary: "A storage root on the local disk, the built-in every deployment has.",
		Description: "It is what makes the resource plane observable without an operator installing " +
			"anything first. The root is created when it does not exist, with permissions that make it the " +
			"operator's and nobody else's. It fails rather than degrades: a root that cannot be used is a " +
			"resource the deployment does not have.",
		Version:   "1.0.0",
		Contract:  registry.Contract,
		Stability: extension.StabilityStable,
		Options: []extension.Option{
			{
				Key:      "root",
				Type:     extension.OptionString,
				Required: true,
				Summary:  "The directory this instance owns. It is created when it does not exist.",
			},
			{
				Key:  "allowed_extensions",
				Type: extension.OptionList,
				Summary: "What an upload into this storage may be called. Omit the key for the built-in " +
					"list of print-file and image extensions; write an empty list to permit any extension.",
			},
		},
	}
}
