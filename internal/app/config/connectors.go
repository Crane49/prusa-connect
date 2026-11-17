package config

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
)

// Connector is one printer dialect this process mounts, as an operator declares
// it under core.connectors.
//
// It is a core key and not a resource instance because a connector lives as
// long as the process: its routes are mounted on the sealed router, which is
// built once, so changing one needs a restart. The supervision plane behind
// resources: starts and stops instances while the process runs, and a
// connector needs none of that.
type Connector struct {
	// Type is the registered dialect, bare or namespace-qualified.
	Type string `yaml:"type"`
	// Enabled is the operator's switch. An absent key reads as true.
	Enabled *bool `yaml:"enabled"`
	// Config is the dialect's own settings. The core never reads inside it. It
	// carries the block to the driver, which owns the schema and validates it.
	Config yaml.Node `yaml:"config"`
}

// NamedConnector is one declaration with the instance name it was declared
// under. The name reaches a URL path, a log field and a metric label, so it is
// held to the label rules of every other instance name.
type NamedConnector struct {
	// Name is the operator-chosen instance name.
	Name string
	// Connector is the declaration.
	Connector Connector
}

// On reports whether this connector is mounted. An absent enabled key is on.
func (c Connector) On() bool { return c.Enabled == nil || *c.Enabled }

// Settings renders the dialect's own block as the JSON a driver is handed. An
// empty block is nil, which leaves every one of the driver's defaults in place.
func (c Connector) Settings() (json.RawMessage, error) {
	if c.Config.IsZero() {
		return nil, nil
	}

	var value any
	if err := c.Config.Decode(&value); err != nil {
		return nil, err
	}

	if value == nil {
		return nil, nil
	}

	return json.Marshal(value)
}

// DeclaredConnectors returns the declarations sorted by instance name, so two
// starts on one file mount the same routes in the same order.
func (c Core) DeclaredConnectors() []NamedConnector {
	out := make([]NamedConnector, 0, len(c.Connectors))
	for _, name := range slices.Sorted(maps.Keys(c.Connectors)) {
		out = append(out, NamedConnector{Name: name, Connector: c.Connectors[name]})
	}

	return out
}

// validateConnectors refuses what can be refused before a driver is built. The
// dialect checks its own settings block when the composition root builds it.
func (c Core) validateConnectors() error {
	for _, declared := range c.DeclaredConnectors() {
		key := "core.connectors." + declared.Name

		if err := ref.ValidateID(declared.Name); err != nil {
			return fmt.Errorf("%w: %s is not a name an instance can carry: %w", ErrInvalid, key, err)
		}

		if declared.Connector.Type == "" {
			return fmt.Errorf("%w: %s.type is empty, so nothing says which dialect to mount", ErrInvalid, key)
		}

		if _, err := declared.Connector.Settings(); err != nil {
			return fmt.Errorf("%w: %s.config: %w", ErrInvalid, key, err)
		}
	}

	return nil
}
