// Package prusa embeds the definition files of the Prusa printer interfaces,
// in the format that package sdk/fractal/schema loads.
//
//	registry, err := schema.Load(prusa.Files)
//
// To add a property to an interface, add one line to its JSON file.
// enums.json defines every enumeration.
package prusa

import "embed"

// Files is the embedded filesystem with every definition file, in its root
// directory. Pass it to schema.Load.
//
//go:embed *.json
var Files embed.FS
