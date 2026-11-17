//go:build !noothermodels

package profile

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
)

// The alias rows of every model. The printer build reads the rows of its one
// model from its own profile file instead.
//
//go:embed aliases.json
var embeddedAliases []byte

// aliasFile is the shape of aliases.json. The rows reach a caller through
// Registry.Aliases, so nothing outside this file reads the wrapper.
type aliasFile struct {
	Schema  int     `json:"schema"`
	Aliases []Alias `json:"aliases"`
}

// loadAliases parses the alias file. A file this build cannot read is refused
// and not skipped. An empty alias table and an unreadable one produce the same
// unmatched printer, and only the unreadable one is a mistake.
func loadAliases(data []byte) ([]Alias, error) {
	var file aliasFile

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("parse aliases: %w", err)
	}

	if file.Schema != CurrentSchema {
		return nil, fmt.Errorf("%w: alias file declares %d, this build reads %d",
			ErrSchema, file.Schema, CurrentSchema)
	}

	return file.Aliases, nil
}
