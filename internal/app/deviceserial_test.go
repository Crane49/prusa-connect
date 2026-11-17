package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// What a printer is identified by in the listing.
//
// BUG-006: /api/v1/devices rendered "serial":"" for all three machines on the
// bench, while the record fact beside it carried a populated unit_uid such as
// 8LFD1PKK5836E558. Buddy 6.5.7 and 6.8.1 send no serial number when they
// register, so the field an operator matches a machine by was blank on every
// machine, and the identifier the printer does send reached no API reader.

// benchUnitUID is the MK4S the bug was filed against, spelled as the bench
// recorded it.
const benchUnitUID = "8LFD1PKK5836E558"

// arriveUnder puts one printer in the plane under an identity the case wrote.
func arriveUnder(t *testing.T, plane *devices, identity connectorapi.Identity) string {
	t.Helper()

	host := plane.Arrivals("test", nil, capability.NewResolver(profile.Registry{}, discard()), "")

	session := &countedSession{identity: identity, patches: make(chan connectorapi.StatePatch)}

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { close(session.patches) })

	return plane.idOf(identity)
}

// TestAPrinterThatSentNoSerialIsListedByItsUnitID is the gate. A printer that
// reported no serial number still reported something to be told apart by, and
// the listing has to render it.
func TestAPrinterThatSentNoSerialIsListedByItsUnitID(t *testing.T) {
	t.Parallel()

	plane, _ := planeAndBus(t)

	id := arriveUnder(t, plane, connectorapi.Identity{
		Vendor: "Prusa", Model: "MK4S", UnitUID: benchUnitUID,
	})

	listed, err := plane.Get(t.Context(), "user:"+config.DefaultPrincipal, id)
	require.NoError(t, err)

	require.Equal(t, benchUnitUID, listed.UnitUID,
		"the listing renders no unit id, so the only identifier the printer sent reaches nobody")
	require.Equal(t, benchUnitUID, listed.Serial,
		"the listing renders an empty serial for a printer that reported a unit id")

	// The display name is the model and the serial, so a fleet of machines that
	// reported no serial would be a fleet of rows all called MK4S.
	require.Contains(t, listed.Name, benchUnitUID,
		"the display name is %q, which names no machine", listed.Name)

	t.Logf("observed: the printer lists as serial=%q unit_uid=%q name=%q",
		listed.Serial, listed.UnitUID, listed.Name)
}

// TestTheListingRendersTheUnitID is the same gate one route over. BUG-006 was
// filed against /api/v1/devices, which is the listing and not the single
// device, and the two render through separate methods: a projection that fell
// back on one and not the other would still leave an operator a column of
// blanks on the page they actually read.
func TestTheListingRendersTheUnitID(t *testing.T) {
	t.Parallel()

	plane, _ := planeAndBus(t)

	arriveUnder(t, plane, connectorapi.Identity{
		Vendor: "Prusa", Model: "MK4S", UnitUID: benchUnitUID,
	})

	page, err := plane.List(t.Context(), "user:"+config.DefaultPrincipal, httpapi.DeviceQuery{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1, "the printer that arrived is not in the listing")

	row := page.Items[0]

	require.Equal(t, benchUnitUID, row.UnitUID,
		"the listing renders no unit id, so the only identifier the printer sent reaches nobody")
	require.Equal(t, benchUnitUID, row.Serial,
		"the listing renders an empty serial for a printer that reported a unit id")
	require.Contains(t, row.Name, benchUnitUID,
		"the listed name is %q, which names no machine", row.Name)

	t.Logf("observed: the listing row reads serial=%q unit_uid=%q name=%q",
		row.Serial, row.UnitUID, row.Name)
}

// TestAPrinterThatSentASerialKeepsIt is the other half. The fallback is for a
// printer that reported nothing, and it must not overwrite one that did.
func TestAPrinterThatSentASerialKeepsIt(t *testing.T) {
	t.Parallel()

	plane, _ := planeAndBus(t)

	id := arriveUnder(t, plane, connectorapi.Identity{
		Vendor: "Prusa", Model: "MK4S", Serial: "SIM-STICKER", UnitUID: benchUnitUID,
	})

	listed, err := plane.Get(t.Context(), "user:"+config.DefaultPrincipal, id)
	require.NoError(t, err)

	require.Equal(t, "SIM-STICKER", listed.Serial)
	require.Equal(t, benchUnitUID, listed.UnitUID,
		"the unit id is dropped for a printer that also reported a serial")

	t.Logf("observed: the printer lists as serial=%q unit_uid=%q",
		listed.Serial, listed.UnitUID)
}
