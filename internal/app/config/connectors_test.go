package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/internal/app/config"
)

// TestAConnectorDeclarationIsRefusedBeforeAnythingIsBuilt covers what can be
// decided without a driver. What is inside a settings block belongs to the
// dialect and is refused when the composition root builds it.
func TestAConnectorDeclarationIsRefusedBeforeAnythingIsBuilt(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		name      string
		connector config.Connector
		names     string
	}{
		"a name no url path and no metric label could carry": {
			name:      "Lab Printers",
			connector: config.Connector{Type: "echo"},
			names:     "core.connectors.Lab Printers",
		},
		"a block that names no dialect": {
			name:      "lab",
			connector: config.Connector{},
			names:     "core.connectors.lab.type",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			core := config.Default()
			core.Connectors = map[string]config.Connector{tc.name: tc.connector}

			require.ErrorContains(t, core.Validate(), tc.names)
		})
	}
}

// TestAConnectorIsOnUnlessItsOperatorSaidOtherwise pins the default of the
// switch. Somebody who wrote the block down meant it, so an absent key is on
// and only an explicit false is off.
func TestAConnectorIsOnUnlessItsOperatorSaidOtherwise(t *testing.T) {
	t.Parallel()

	off := false
	on := true

	require.True(t, config.Connector{}.On())
	require.True(t, config.Connector{Enabled: &on}.On())
	require.False(t, config.Connector{Enabled: &off}.On())
}

// TestTheSettingsBlockReachesTheDriverAsTheOperatorWroteIt holds the one thing
// the core does with a dialect's own settings, which is carry them without
// reading them. A number the operator typed keeps its digits, and a block that
// is absent stays absent so the driver's own defaults survive.
func TestTheSettingsBlockReachesTheDriverAsTheOperatorWroteIt(t *testing.T) {
	t.Parallel()

	var block yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("queue_depth: 9007199254740993\nread_deadline: 5s\n"), &block))

	settings, err := config.Connector{Type: "echo", Config: block}.Settings()
	require.NoError(t, err)
	require.JSONEq(t, `{"queue_depth":9007199254740993,"read_deadline":"5s"}`, string(settings))

	absent, err := config.Connector{Type: "echo"}.Settings()
	require.NoError(t, err)
	require.Nil(t, absent, "an absent block became an empty one, which is not the same to a driver")
}

// TestDeclaredConnectorsAreInOneOrder keeps two starts on one file mounting the
// same routes in the same order, which a map range does not.
func TestDeclaredConnectorsAreInOneOrder(t *testing.T) {
	t.Parallel()

	core := config.Default()
	core.Connectors = map[string]config.Connector{
		"zulu": {Type: "echo"}, "alpha": {Type: "echo"}, "mike": {Type: "echo"},
	}

	declared := core.DeclaredConnectors()
	require.Len(t, declared, 3)
	require.Equal(t, []string{"alpha", "mike", "zulu"},
		[]string{declared[0].Name, declared[1].Name, declared[2].Name})
}

// TestAConnectorDeclarationNamesNoAddress is the owner's answer of 2026-08-28,
// held where it could be undone.
//
// An address this server dials lives in the machine-owned overlay, because a
// customer adds a printer from the interface and from the API and never edits a
// file. The cheapest way to break that is a key here: an
// address under core.connectors would be an address only an operator with an
// editor can write, and the API surface would then have two doors onto one
// list.
//
// The dialect's own settings block is not searched. It is opaque to the core by
// design, and a dialect that reads an address out of it is a dialect that has
// answered this question for itself.
func TestAConnectorDeclarationNamesNoAddress(t *testing.T) {
	t.Parallel()

	forbidden := []string{"address", "endpoint", "endpoints", "host", "url", "port", "device"}
	for field := range reflect.TypeFor[config.Connector]().Fields() {
		tag, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")

		for _, name := range forbidden {
			require.NotEqual(t, name, strings.ToLower(field.Name),
				"config.Connector.%s names an address, and an address belongs in the overlay", field.Name)
			require.NotEqual(t, name, tag,
				"core.connectors carries a %q key, and an address belongs in the overlay", tag)
		}
	}
}
