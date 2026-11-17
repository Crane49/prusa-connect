// Package interfaces is the set of Fractal interfaces this module serves.
//
// The definitions are the Prusa interfaces, data files in sdk/fractal/prusa
// that are embedded through that package. There is one copy of them.
//
// Adding a property to an interface is an edit to the definition file and an
// edit to the mapping file in packages/fractal/bridge. No Go code changes.
package interfaces

import (
	"fmt"
	"sync"

	"github.com/prusa3d/prusa-connect/sdk/fractal/prusa"
	"github.com/prusa3d/prusa-connect/sdk/fractal/schema"
)

// built holds the one registry this process needs. The files are embedded, so
// building the registry can fail only on a file this repository shipped broken,
// which the tests catch before a release.
//
//nolint:gochecknoglobals // one immutable registry, built once and read everywhere.
var built = sync.OnceValues(func() (*schema.Registry, error) {
	registry, err := schema.Load(prusa.Files)
	if err != nil {
		return nil, fmt.Errorf("load the Prusa interface definitions: %w", err)
	}

	return registry, nil
})

// Registry gives the interface definitions this module serves. The registry is
// read-only once built, so every caller shares one and no caller locks.
func Registry() (*schema.Registry, error) {
	return built()
}
