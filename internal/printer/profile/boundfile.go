//go:build !noothermodels

package profile

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
)

// The bound rows of every model. The printer build reads the rows of its one
// model from its own profile file instead.
//
//go:embed bounds.json
var embeddedBounds []byte

// boundFile is the shape of bounds.json. The rows reach a caller through
// Registry.Bounds, so nothing outside this file reads the wrapper.
type boundFile struct {
	Schema int     `json:"schema"`
	Bounds []Bound `json:"bounds"`
}

// loadBounds parses the bounds file. A file this build cannot read is refused
// and not skipped, for the reason loadAliases gives. An empty table and an
// unreadable one leave the same unbounded argument, and only the unreadable
// one is a mistake.
func loadBounds(data []byte) ([]Bound, error) {
	var file boundFile

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("parse bounds: %w", err)
	}

	if file.Schema != CurrentSchema {
		return nil, fmt.Errorf("%w: bound file declares %d, this build reads %d",
			ErrSchema, file.Schema, CurrentSchema)
	}

	return file.Bounds, nil
}
