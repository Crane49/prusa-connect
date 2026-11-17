//go:build !noothermodels

package profile

import _ "embed"

//go:embed profiles.json
var embedded []byte

// loadBuiltin reads the three embedded files as one registry. The alias and
// bound rows are validated together with the profiles, because a row is right
// or wrong only against the models the file carries.
func loadBuiltin() (Registry, error) {
	registry, err := Load(embedded)
	if err != nil {
		return Registry{}, err
	}

	registry.Aliases, err = loadAliases(embeddedAliases)
	if err != nil {
		return Registry{}, err
	}

	registry.Bounds, err = loadBounds(embeddedBounds)
	if err != nil {
		return Registry{}, err
	}

	if err = Validate(registry); err != nil {
		return Registry{}, err
	}

	registry.applyBounds()

	return registry, nil
}
