package app

// A printer the server runs on cannot be retired: its driver would connect
// again at once and the machine would come back under a new id
// (ADR-20261103-szc).

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

func TestAResidentPrinterIsNotRetired(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	holder := &recordingForgetter{}
	plane.useForgetter("onboard", holder)
	plane.useResident("onboard")

	id := arriveThrough(t, plane, "onboard",
		connectorapi.Identity{Vendor: "Prusa", Model: "Onboard", Serial: "SN-ONBOARD", UnitUID: "FP-ONBOARD"})
	managing(t, graph, id)

	requireFaultCode(t, plane.Retire(t.Context(), manager, id), httpfault.CodeDeviceResident())
	require.Empty(t, holder.taken(), "the connector was asked to forget the printer it runs on")

	device, err := plane.Get(t.Context(), manager, id)
	require.NoError(t, err)
	require.Nil(t, device.Retired, "the printer the server runs on was retired")
	require.True(t, device.Resident, "the record does not say the printer is resident")
}

// A caller who may not administer the printer is refused on the permission,
// as for any printer, and not with DEVICE_RESIDENT (ADR-20261103-szc rule 2).
func TestAResidentPrinterRefusesAViewerOnPermission(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	plane.useResident("onboard")

	id := arriveThrough(t, plane, "onboard",
		connectorapi.Identity{Vendor: "Prusa", Model: "Onboard", UnitUID: "FP-VIEW"})
	require.NoError(t, graph.Write(t.Context(), tuple(t, authz.TypeDevice, id, authz.RelViewer, operator)))

	requireFaultCode(t, plane.Retire(t.Context(), operator, id), authz.CodeAccessDenied())
}

// A printer an earlier build retired, before its connector said it is
// resident, answers a second removal as a repeat of the first, not with a
// refusal.
func TestARetiredResidentPrinterAnswersARepeatedRemoval(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	id := arriveThrough(t, plane, "onboard",
		connectorapi.Identity{Vendor: "Prusa", Model: "Onboard", UnitUID: "FP-OLD"})
	managing(t, graph, id)
	require.NoError(t, plane.Retire(t.Context(), manager, id))

	plane.useResident("onboard")

	require.NoError(t, plane.Retire(t.Context(), manager, id))
}

// A printer of a network connector is retired as before, while another
// connector on the same server is resident.
func TestAPrinterOfANetworkConnectorIsStillRetired(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	plane.useResident("onboard")

	id := arriveThrough(t, plane, "xbuddy", connectorapi.Identity{Vendor: "Prusa", Model: "XL", UnitUID: "FP-XL"})
	managing(t, graph, id)

	device, err := plane.Get(t.Context(), manager, id)
	require.NoError(t, err)
	require.False(t, device.Resident, "a printer of a network connector is said to be resident")

	require.NoError(t, plane.Retire(t.Context(), manager, id))

	device, err = plane.Get(t.Context(), manager, id)
	require.NoError(t, err)
	require.NotNil(t, device.Retired, "a printer of a network connector was not retired")
}

// Renaming a resident printer stays allowed: only its retirement is refused
// (ADR-20261103-szc rule 4).
func TestAResidentPrinterIsRenamed(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)
	plane.useResident("onboard")

	id := arriveThrough(t, plane, "onboard",
		connectorapi.Identity{Vendor: "Prusa", Model: "Onboard", UnitUID: "FP-NAME"})
	managing(t, graph, id)

	named, err := plane.SetName(t.Context(), manager, id, "Workshop")
	require.NoError(t, err)
	require.Equal(t, "Workshop", named)
}

// residentType is a dialect whose printer runs the server. It is registered
// globally because the case below drives the composition root, which reads the
// process registry.
const residentType connectorapi.TypeName = "resident-printer"

//nolint:gochecknoinits // the registry is read at start-up, so a test dialect has to be there before one.
func init() {
	connectorapi.Register(residentType, dialDoc(), func(connectorapi.Spec) (connectorapi.Driver, error) {
		return dialsOverTCP{baselineDriver{baseline: connectorapi.Set{
			statusVerb: {Flag: true, Kind: connectorapi.KindFlag},
		}}}, nil
	}, connectorapi.Dialect{Resident: true})
}

// TestTheServerRefusesToRetireThePrinterItRunsOn drives the whole server: the
// composition root reads Resident off the dialect, the API refuses the removal
// with 409, and the record says the printer is resident.
func TestTheServerRefusesToRetireThePrinterItRunsOn(t *testing.T) {
	t.Parallel()

	device := newListeningPrinter(t, "RESIDENT-1")
	dir := t.TempDir()

	writeEndpoint(t, dir, device.address())

	running := start(t, dir, `
core:
  log:
    level: error
  connectors:
    far:
      type: resident-printer
`)

	require.Eventually(t, func() bool {
		listed := printers(t, running)

		return len(listed) == 1 && listed[0].Connected
	}, settle, 25*time.Millisecond, "the resident printer never arrived")

	id := printers(t, running)[0].ID

	removed := running.call(t, http.MethodDelete, "/api/v1/devices/"+id, "")
	require.Equal(t, http.StatusConflict, removed.status, removed.body)

	var problem map[string]any

	require.NoError(t, json.Unmarshal([]byte(removed.body), &problem), removed.body)
	require.NotEmpty(t, problem["trace_id"])
	delete(problem, "trace_id")
	require.Equal(t, map[string]any{
		"code":      "core.printer/DEVICE_RESIDENT",
		"title":     "The printer runs this server, so it cannot be removed from it",
		"detail":    "the printer runs this server and cannot be retired",
		"params":    map[string]any{},
		"retryable": false,
		"type":      "https://beta.prusa3d.com/connect-local/docs/error-codes/#core-printer-device-resident",
		"status":    float64(http.StatusConflict),
		"instance":  "/api/v1/devices/" + id,
	}, problem)

	listed := printers(t, running)
	require.Len(t, listed, 1, "the resident printer left the listing")
	require.True(t, listed[0].Resident, "the listing does not say the printer is resident")

	running.stopAndWait(t)
}
