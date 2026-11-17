package app

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// What a driver says about a machine is text the machine chose, so the host
// cuts it before it files it. A driver that reports a model name of any length
// would otherwise put that whole string in the device record, in every log line
// about the machine and on a metric label.
func TestTheHostBoundsTheIdentityTextADriverReports(t *testing.T) {
	t.Parallel()

	plane, _ := planeAndBus(t)

	host := plane.Arrivals("test", talkingBaseline(),
		capability.NewResolver(profile.Registry{}, discard()), "")

	long := strings.Repeat("M", machinetext.IdentityMaxBytes*2)

	session := &countedSession{
		identity: connectorapi.Identity{
			Vendor:   "Prusa",
			Model:    long,
			Serial:   "SIM-LONG-IDENTITY",
			Firmware: connectorapi.NewFirmwareLabel(long),
		},
		patches: make(chan connectorapi.StatePatch),
	}

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { close(session.patches) })

	id := plane.idOf(machinetext.ClipIdentity(session.identity))

	plane.mu.Lock()
	entry, live := plane.entries[id]
	plane.mu.Unlock()

	require.True(t, live, "the printer arrived and the plane holds no entry for it")

	bound := machinetext.IdentityMaxBytes + len(machinetext.Marker)

	require.LessOrEqual(t, len(entry.identity.Model), bound,
		"the model a driver reported was stored whole")
	require.LessOrEqual(t, len(firmwareText(entry.identity.Firmware)), bound,
		"the firmware a driver reported was stored whole")
}
