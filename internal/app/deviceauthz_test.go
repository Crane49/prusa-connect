package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// operator is somebody who runs prints on a fleet they do not own: they hold
// send_command and they do not hold send_gcode (ADR-20260825-z65 rule 8).
const (
	operator = "user:eve"
	manager  = "user:mallory"

	rawGCode    = "core.command.send_gcode"
	forcedGCode = "core.command.force_gcode"
)

// TestTheDevicePlaneTakesTheActionTheVerbTakes is ADR-20260825-z65 rule 9 where the
// verbs actually leave: the device plane is what every transport reaches, so a
// plane that checked one action for all 47 verbs would hand the raw gcode pair
// to anybody who may pause a print.
//
// Raw gcode is now the dangerous tier rather than a level: both verbs are
// marked opaque_payload, so no class, role or level confers them and even a
// manager is refused. What reaches them is ownership of the machine
// (ADR-20260901-b2z rules 16 to 19), and the refusal is an access refusal rather than
// the not-found the reach check gives, because the caller holds the class and
// the device is already in their listing.
//
// Both directions, because a permit test without its refusal is half a test.
func TestTheDevicePlaneTakesTheActionTheVerbTakes(t *testing.T) {
	t.Parallel()

	// The two spellings the vocabulary holds send_gcode, read back so a rename
	// there fails here rather than turning this into a test of two dead strings.
	require.Equal(t, []string{rawGCode, forcedGCode}, authz.RawGCodeVerbs())

	graph := newGraph(t)
	plane := planeOn(t, graph)
	id := arrive(t, plane, "SIM-GCODE")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, operator)))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelManager, manager)))
	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOwner, owner)))

	cases := map[string]struct {
		principal string
		verb      string
		reachable bool
		refusal   fault.Code
	}{
		"an operator sends an ordinary command": {
			principal: operator, verb: nozzleVerb, reachable: true,
		},
		"an operator sends raw gcode": {
			principal: operator, verb: rawGCode, refusal: authz.CodeAccessDenied(),
		},
		"an operator forces raw gcode": {
			principal: operator, verb: forcedGCode, refusal: authz.CodeAccessDenied(),
		},
		"a manager does not send raw gcode either": {
			principal: manager, verb: rawGCode, refusal: authz.CodeAccessDenied(),
		},
		"a manager sends an ordinary command": {
			principal: manager, verb: nozzleVerb, reachable: true,
		},
		"the owner sends raw gcode": {
			principal: owner, verb: rawGCode, reachable: true,
		},
		"the owner forces raw gcode": {
			principal: owner, verb: forcedGCode, reachable: true,
		},
		"a caller holding nothing is told the printer is not there": {
			principal: stranger, verb: nozzleVerb, refusal: fault.PrinterNotFound.Code(),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := plane.Command(t.Context(), tc.principal, id,
				httpapi.PrinterCommand{Verb: tc.verb})
			require.Error(t, err, "the stand-in session declares no capability, so nothing is ever accepted")

			if tc.reachable {
				require.False(t, fault.HasCode(err, fault.PrinterNotFound.Code()),
					"the caller reaches this verb and the plane reported the printer absent")
				require.False(t, fault.HasCode(err, authz.CodeAccessDenied()),
					"the caller reaches this verb and the plane refused it")

				return
			}

			require.Truef(t, fault.HasCode(err, tc.refusal),
				"a verb the caller does not reach was refused with %v and not with %s", err, tc.refusal)
		})
	}
}

// Sending a file reads a storage's contents, so it takes read_storage on the
// storage kind beside send_command on the printer.
//
// Both directions, and the refusal has to arrive before anything resolves the
// storage or the name: an operator on one printer could otherwise copy any file
// out of any declared storage, and the refusals it got back for a storage that
// is absent, a file that is absent and a type that cannot read told it which of
// each exist.
func TestSendingAFileTakesReadStorageOnTheSource(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "part.gcode"), []byte("G28\n"), 0o600))

	graph := newGraph(t)
	plane := planeReading(t, graph, stageOne(t, root))
	id := arrive(t, plane, "SIM-SEND")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, id, authz.RelOperator, operator)))

	upload := httpapi.DeviceUpload{Storage: "bench", Source: "part.gcode", Path: "usb/part.gcode"}

	// The refusal. An operator on this printer who holds nothing on the storage
	// kind is refused, and the refusal names neither the storage nor the file,
	// both of which are really there.
	_, err := plane.Send(t.Context(), operator, id, upload)
	requireDenied(t, err)
	require.False(t, fault.HasCode(err, fault.StorageNotFound.Code()),
		"the refusal says whether this deployment declares that storage")
	require.False(t, fault.HasCode(err, fault.StorageFileNotFound.Code()),
		"the refusal says whether that storage holds that file")
	t.Logf("observed, refused: %v", err)

	// The permit. The bootstrap subject holds read_storage, so the check passes,
	// the file resolves, and what refuses the transfer is the printer: the
	// stand-in session declares no capability.
	_, err = plane.Send(t.Context(), subjectOf(config.Default().Principal), id, upload)
	require.Error(t, err)
	require.True(t, fault.HasCode(err, connectorapi.CapabilityNotSupported.Code()),
		"a caller holding read_storage was stopped before the printer: %v", err)
	t.Logf("observed, permitted as far as the printer: %v", err)
}

// planeOn is a device plane over a graph the caller keeps, so a test can write
// the grants the refusal is asserted against.
func planeOn(t *testing.T, graph *authz.Store) *devices {
	t.Helper()

	return planeReading(t, graph, staging{})
}

// planeReading is the same plane over a storage a test has put a file in, for
// the paths that read one.
func planeReading(t *testing.T, graph *authz.Store, files staging) *devices {
	t.Helper()

	hub, err := stream.New(stream.Options{Site: "local", Authorizer: visibility{graph: graph}, Logger: discard()})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	plane := newDevices(graph, recordsOf(t, graph), hub, discard(), bootstrapOrg, files, fleetMeters{}, 0)

	t.Cleanup(plane.Close)

	return plane
}

// arrive puts one printer in the plane through the host surface a driver uses,
// and returns the id the plane minted for it.
func arrive(t *testing.T, plane *devices, serial string) string {
	t.Helper()

	host := plane.Arrivals("test", nil, capability.NewResolver(profile.Registry{}, discard()), "")

	session := &countedSession{
		identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: serial},
		patches:  make(chan connectorapi.StatePatch),
	}

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { close(session.patches) })

	return plane.idOf(session.identity)
}
