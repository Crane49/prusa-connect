package app

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// servedDialect is one row of the connectors half of the device-model
// vocabulary, as a client reads it.
type servedDialect struct {
	Connector string   `json:"connector"`
	Qualified string   `json:"qualified"`
	Kind      string   `json:"kind"`
	Brands    []string `json:"brands"`
	Available bool     `json:"available"`
	Dials     bool     `json:"dials"`
	Arrives   bool     `json:"arrives"`
	Needs     string   `json:"needs"`
}

// TestAPrusaMachineIsAskedForASettingsFile holds the one dialect row every
// deployment has, over the real route, in a process that has the dialect linked.
//
// Step two of the add-a-device dialog draws its form from these words, so a
// change to any of them changes what a person is told to do. The words now come
// from the driver's own declaration in internal/connector/xbuddy, so this is
// also what holds that move: the answer a client reads did not change.
//
// It is here rather than beside the handler because internal/transport may not
// import a dialect (ADR-20260824-kkr), so the handler's own package serves a registry
// with no driver in it and cannot ask one anything.
func TestAPrusaMachineIsAskedForASettingsFile(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	read := running.get(t, "/api/v1/vocabularies/device-models")
	require.Equal(t, http.StatusOK, read.status, read.body)

	var body struct {
		Connectors []servedDialect `json:"connectors"`
	}

	require.NoError(t, json.Unmarshal([]byte(read.body), &body))

	var xbuddy servedDialect

	for _, entry := range body.Connectors {
		if entry.Connector == "xbuddy" {
			xbuddy = entry
		}
	}

	// The qualified name is derived from the module this binary was built from,
	// so it is checked for presence and not for its text.
	require.NotEmpty(t, xbuddy.Qualified)
	xbuddy.Qualified = ""

	require.Equal(t, servedDialect{
		Connector: "xbuddy",
		Kind:      "printer",
		Brands:    []string{"Prusa"},
		Available: true,
		Arrives:   true,
		Needs:     "settings_file",
	}, xbuddy)
}
