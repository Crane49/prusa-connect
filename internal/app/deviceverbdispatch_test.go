package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The shelf's declaration, as the host hands it to the graph. It is the thing
// that was impossible before this record: ten commands an adapter declares,
// registrable and conformance-passing and undispatchable, because the core's
// verb table did not name them and dispatch refuses a verb nothing classifies.
const (
	shelfKind      = authz.DeviceKind("acme.io/filament-shelf")
	shelfAttention = "acme.io.filament-shelf.command.set_slot_attention"
	shelfReset     = "acme.io.filament-shelf.command.factory_reset"
)

func shelfDeclaration() authz.KindDeclaration {
	return authz.KindDeclaration{
		Module:      "github.com/acme/prusa-adapter-shelf",
		Kind:        shelfKind,
		DisplayName: "Filament shelf",
		Verbs: map[string]authz.DeclaredVerb{
			shelfAttention: {Class: authz.ActionOperate},
			// opaque_payload is the host's to compute from the descriptor and
			// never a declaration's to claim, so it is deliberately absent.
			//nolint:exhaustive // an adapter marks irreversible and nothing else
			shelfReset: {Class: authz.ActionConfigure, Markings: map[authz.Marking]authz.Marker{
				authz.MarkingIrreversible: authz.MarkerAdapter,
			}},
		},
	}
}

// TestADeclaredVerbDispatchesAndTheTwoStageRefusalHolds is ADR-20260901-b2z rules 1 and
// 6 at the boundary every transport passes through.
//
// A shelf's verb reaches the device plane, is classified against the shelf's
// own kind, is authorized by the class the declaration gave it, and is sent.
// The same verb against a printer is refused, and the refusal for a verb no
// kind declares carries no fact about any device.
func TestADeclaredVerbDispatchesAndTheTwoStageRefusalHolds(t *testing.T) {
	graph := newGraph(t)
	plane := planeOn(t, graph)

	t.Cleanup(authz.ResetVerbTable)

	_, err := graph.InstallKindDeclarations(t.Context(), shelfDeclaration())
	require.NoError(t, err)

	printerID := arrive(t, plane, "SIM-SHELF-PRINTER")
	shelfID := arriveAs(t, plane, "SIM-SHELF", shelfKind)

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, shelfID, authz.RelOperator, operator),
		tuple(t, authz.TypeDevice, shelfID, authz.RelManager, manager),
		tuple(t, authz.TypeDevice, shelfID, authz.RelOwner, owner),
		tuple(t, authz.TypeDevice, printerID, authz.RelManager, manager)))

	// The kind reached the directory, so the verb is classified against the
	// shelf's own table.
	kind, err := graph.DeviceKind(t.Context(), shelfID)
	require.NoError(t, err)
	require.Equal(t, shelfKind, kind, "the device was not registered under the declared kind")

	// Stage one, before any device is resolved: a verb no compiled-in kind
	// names is refused with the same fault whatever id is named, and the
	// refusal carries the verb and nothing about a device.
	for _, id := range []string{printerID, shelfID, "00000000-0000-4000-8000-00000n0th1ng"} {
		_, cmdErr := plane.Command(t.Context(), operator, id,
			httpapi.PrinterCommand{Verb: "acme.io.filament-shelf.command.melt"})
		require.Truef(t, fault.HasCode(cmdErr, fault.PrinterVerbUnknown.Code()),
			"the unknown-verb refusal for %s was %v", id, cmdErr)
	}

	// Stage two, behind the read grant: the shelf's verb against a printer is
	// refused by name to a caller who may see the printer.
	_, err = plane.Command(t.Context(), manager, printerID,
		httpapi.PrinterCommand{Verb: shelfAttention})
	require.True(t, fault.HasCode(err, fault.PrinterVerbUnknown.Code()),
		"a shelf verb on a printer was not refused as unknown for that kind: %v", err)

	// And a caller who holds nothing on that printer is told it is not there,
	// so stage two is no device-kind oracle.
	_, err = plane.Command(t.Context(), stranger, printerID,
		httpapi.PrinterCommand{Verb: shelfAttention})
	require.True(t, fault.HasCode(err, fault.PrinterNotFound.Code()),
		"stage two answered a caller holding nothing: %v", err)

	// The permit. The operator holds the class the declaration gave the verb,
	// so the command reaches the session, and what refuses it is the machine:
	// the stand-in session declares no capability.
	_, err = plane.Command(t.Context(), operator, shelfID,
		httpapi.PrinterCommand{Verb: shelfAttention})
	require.True(t, fault.HasCode(err, connectorapi.CapabilityNotSupported.Code()),
		"a declared verb did not reach the shelf's session: %v", err)

	// The dangerous verb is conferred by no level, and by ownership.
	_, err = plane.Command(t.Context(), manager, shelfID, httpapi.PrinterCommand{Verb: shelfReset})
	require.True(t, fault.HasCode(err, authz.CodeAccessDenied()),
		"a manager reached a verb the adapter marked irreversible: %v", err)

	_, err = plane.Command(t.Context(), owner, shelfID, httpapi.PrinterCommand{Verb: shelfReset})
	require.True(t, fault.HasCode(err, connectorapi.CapabilityNotSupported.Code()),
		"the owner did not reach the dangerous verb on their own machine: %v", err)
}

// arriveAs is arrive for a driver whose declaration names a device kind, so the
// machine reaches the directory under that kind rather than under printer.
func arriveAs(t *testing.T, plane *devices, serial string, kind authz.DeviceKind) string {
	t.Helper()

	host := plane.Arrivals("shelf", nil, capability.NewResolver(profile.Registry{}, discard()), kind)

	session := &countedSession{
		identity: connectorapi.Identity{Vendor: "Acme", Model: "Shelf", Serial: serial},
		patches:  make(chan connectorapi.StatePatch),
	}

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { close(session.patches) })

	return plane.idOf(session.identity)
}
