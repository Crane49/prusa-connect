package devtools_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/devtools"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// errFactory is what a driver type reports when its config cannot be acted on.
var errFactory = errors.New("the factory refused the spec")

// frozen is an inbound driver whose devices dial paths it declares itself,
// which is the shape a dialect with shipping firmware has.
type frozen struct {
	routes   []string
	baseline connectorapi.Set
}

func (f frozen) Baseline() connectorapi.Set { return f.baseline }
func (f frozen) Routes() []string           { return f.routes }

func (f frozen) Handler(connectorapi.Arrivals) (http.Handler, error) {
	return http.NotFoundHandler(), nil
}

// hosted is an inbound driver that can be told where to dial, so the host picks
// its prefix and nothing in the registry records which.
type hosted struct {
	baseline connectorapi.Set
}

func (h hosted) Baseline() connectorapi.Set { return h.baseline }

func (h hosted) Handler(connectorapi.Arrivals) (http.Handler, error) {
	return http.NotFoundHandler(), nil
}

// dialled is an outbound driver: the host reaches the device, so there is no
// path for a device to arrive on.
type dialled struct {
	baseline connectorapi.Set
}

func (d dialled) Baseline() connectorapi.Set { return d.baseline }

func (d dialled) Connect(
	_ context.Context, _ connectorapi.Endpoint, _ connectorapi.Set,
) (connectorapi.Session, error) {
	return nil, errFactory
}

// directionless implements neither half, which the contract refuses.
type directionless struct{}

func (directionless) Baseline() connectorapi.Set { return connectorapi.Set{} }

func capabilities(keys ...string) connectorapi.Set {
	out := connectorapi.Set{}
	for _, key := range keys {
		out[connectorapi.Capability(key)] = connectorapi.Value{Flag: true}
	}

	return out
}

func doc() extension.Doc {
	return extension.Doc{
		Summary:     "A driver declared inside a test.",
		Description: "It exists so the inventory can be read without a real dialect behind it.",
		Version:     "2.1.0",
		Contract:    connectorapi.Contract,
		Stability:   extension.StabilityExperimental,
	}
}

func declare(t *testing.T, name string, factory connectorapi.Factory) connectorapi.Registration {
	t.Helper()

	registration, err := connectorapi.Declare(connectorapi.TypeName(name), doc(), factory)
	require.NoError(t, err)

	return registration
}

func registry(t *testing.T, registrations ...connectorapi.Registration) *connectorapi.Registry {
	t.Helper()

	types, err := connectorapi.NewRegistryFrom(registrations...)
	require.NoError(t, err)

	return types
}

func byName(t *testing.T, inventory devtools.Inventory, name string) devtools.Connector {
	t.Helper()

	for _, connector := range inventory.Connectors {
		if connector.Name == name {
			return connector
		}
	}

	t.Fatalf("the inventory carries no connector named %q: %+v", name, inventory.Connectors)

	return devtools.Connector{}
}

// TestTheInventoryReportsWhetherAFrozenDialectIsServed is the row a person reads
// first at a bench: a printer that cannot reach its route is a printer that
// never appears, and the difference between that and a dialect this build does
// not carry is the whole of the diagnosis.
func TestTheInventoryReportsWhetherAFrozenDialectIsServed(t *testing.T) {
	t.Parallel()

	driver := frozen{
		routes:   []string{"/p/register", "/p/ws"},
		baseline: capabilities("core.command.stop", "core.feature.dialogs"),
	}

	build := func(connectorapi.Spec) (connectorapi.Driver, error) { return driver, nil }

	types := registry(t, declare(t, "served", build), declare(t, "unserved", build))

	inventory := devtools.ReadInventory(types, []devtools.Route{
		{Pattern: "/p/register", Reason: "a frozen device wire"},
		{Pattern: "/p/ws", Reason: "a frozen device wire"},
	})

	served := byName(t, inventory, "served")
	require.Equal(t, devtools.StatusMounted, served.Status)
	require.Equal(t, []string{"/p/register", "/p/ws"}, served.Routes)
	require.Equal(t, 2, served.Baseline)
	require.Equal(t, "inbound", served.Direction)
	require.Equal(t, "2.1.0", served.Version)
	require.Empty(t, served.Failure)

	// The same driver read against an origin serving only one of its two paths.
	partial := devtools.ReadInventory(types, []devtools.Route{
		{Pattern: "/p/register", Reason: "a frozen device wire"},
	})
	require.Equal(t, devtools.StatusUnmounted, byName(t, partial, "served").Status)

	// And against an origin serving neither.
	none := devtools.ReadInventory(types, nil)
	require.Equal(t, devtools.StatusUnmounted, byName(t, none, "unserved").Status)
}

func TestTheInventoryReportsEveryOtherPlacement(t *testing.T) {
	t.Parallel()

	types := registry(t,
		declare(t, "hosted", func(connectorapi.Spec) (connectorapi.Driver, error) {
			return hosted{baseline: capabilities("core.command.stop")}, nil
		}),
		declare(t, "dialled", func(connectorapi.Spec) (connectorapi.Driver, error) {
			return dialled{baseline: capabilities("core.command.stop", "core.command.pause")}, nil
		}),
		declare(t, "broken", func(connectorapi.Spec) (connectorapi.Driver, error) {
			return nil, errFactory
		}),
		declare(t, "nowhere", func(connectorapi.Spec) (connectorapi.Driver, error) {
			return directionless{}, nil
		}),
	)

	inventory := devtools.ReadInventory(types, nil)

	hostChosen := byName(t, inventory, "hosted")
	require.Equal(t, devtools.StatusHostChosen, hostChosen.Status)
	require.Empty(t, hostChosen.Routes)
	require.Equal(t, "inbound", hostChosen.Direction)

	outbound := byName(t, inventory, "dialled")
	require.Equal(t, devtools.StatusNoRoutes, outbound.Status)
	require.Equal(t, "outbound", outbound.Direction)
	require.Equal(t, 2, outbound.Baseline)

	broken := byName(t, inventory, "broken")
	require.Equal(t, devtools.StatusFailed, broken.Status)
	require.Contains(t, broken.Failure, errFactory.Error())

	nowhere := byName(t, inventory, "nowhere")
	require.Equal(t, devtools.StatusFailed, nowhere.Status)
	require.Contains(t, nowhere.Failure, connectorapi.ErrNoDirection.Error())
}

// TestTheInventoryCarriesTheOriginsRoutesUnchanged holds the half of the answer
// the registry cannot give. A driver mounted under a prefix the host chose has
// no route of its own to report, so the page would show nothing at all about
// where a printer dials if this list were dropped.
func TestTheInventoryCarriesTheOriginsRoutesUnchanged(t *testing.T) {
	t.Parallel()

	routes := []devtools.Route{
		{Pattern: "/connectors/hosted/*", Reason: "under the prefix this host chose"},
		{Pattern: "/*", Reason: "the frontend"},
	}

	inventory := devtools.ReadInventory(registry(t), routes)

	require.Equal(t, routes, inventory.Routes)
	require.Empty(t, inventory.Connectors)
}
