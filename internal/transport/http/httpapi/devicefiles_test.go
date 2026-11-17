package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// fleetWithFiles is the one printer every case here reads, holding two storages
// and one directory.
func fleetWithFiles() *fakeFleet {
	free := int64(1 << 30)
	written := time.Date(2025, time.June, 15, 15, 6, 40, 0, time.UTC)

	return &fakeFleet{
		printer: Device{ID: "pdeadbeef", Name: "MK4 SIM-1", Connected: true},
		storages: []DeviceStorage{
			{Mountpoint: "/usb", Name: "usb", Type: "USB", FreeSpace: &free},
			{Mountpoint: "/local", Name: "local", Type: "LOCAL", ReadOnly: true},
		},
		listing: DeviceListing{
			Directory: DeviceFile{Path: "usb/models", Name: "MODELS", DisplayName: "models", Folder: true},
			Items: []DeviceFile{
				{Path: "usb/models/part.bgcode", Name: "PART~1.BGC", DisplayName: "part.bgcode",
					Size: 4096, Modified: &written},
			},
		},
	}
}

func TestTheStorageListingServesWhatThePrinterReported(t *testing.T) {
	t.Parallel()

	fleet := fleetWithFiles()

	rec := call(t, serverWithFleet(t, fleet).Handler(),
		http.MethodGet, "/api/v1/devices/pdeadbeef/storages", "")

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Items []DeviceStorage `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 2)
	require.Equal(t, "/usb", body.Items[0].Mountpoint)
	require.Equal(t, "usb", body.Items[0].Name)
	require.NotNil(t, body.Items[0].FreeSpace)
	require.Equal(t, int64(1<<30), *body.Items[0].FreeSpace)
	require.True(t, body.Items[1].ReadOnly)

	t.Logf("observed: %s", rec.Body.String())
}

// A printer that reported no free space must not read as a full one. The
// distinction only survives to a client as JSON null, so it is asserted on the
// bytes rather than on the struct.
func TestAStorageWithNoReportedFreeSpaceSerialisesAsNull(t *testing.T) {
	t.Parallel()

	fleet := fleetWithFiles()

	rec := call(t, serverWithFleet(t, fleet).Handler(),
		http.MethodGet, "/api/v1/devices/pdeadbeef/storages", "")

	require.Equal(t, http.StatusOK, rec.Code)

	var raw struct {
		Items []map[string]json.RawMessage `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	require.Len(t, raw.Items, 2)
	require.JSONEq(t, `null`, string(raw.Items[1]["free_space"]),
		"a printer that reported no figure reads as a full device")
	require.JSONEq(t, `1073741824`, string(raw.Items[0]["free_space"]))
}

func TestTheFileListingAsksThePrinterForThePathTheCallerNamed(t *testing.T) {
	t.Parallel()

	fleet := fleetWithFiles()

	rec := call(t, serverWithFleet(t, fleet).Handler(),
		http.MethodGet, "/api/v1/devices/pdeadbeef/files?path=usb%2Fmodels", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []string{"usb/models"}, fleet.listed)

	var body DeviceListing

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.True(t, body.Directory.Folder)
	require.Equal(t, "usb/models", body.Directory.Path)
	require.Len(t, body.Items, 1)
	require.Equal(t, "usb/models/part.bgcode", body.Items[0].Path)
	require.Equal(t, "PART~1.BGC", body.Items[0].Name)
	require.Equal(t, int64(4096), body.Items[0].Size)

	t.Logf("observed: %s", rec.Body.String())
}

// No path is the root, which is the storages themselves. It reaches the fleet
// as the empty string rather than being refused as a missing parameter.
func TestNoPathListsTheRoot(t *testing.T) {
	t.Parallel()

	fleet := fleetWithFiles()

	rec := call(t, serverWithFleet(t, fleet).Handler(),
		http.MethodGet, "/api/v1/devices/pdeadbeef/files", "")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []string{""}, fleet.listed)
}

// A path that leaves the storage it names is refused with the code and the
// status a client can act on, not with a 500.
func TestAPathThatLeavesItsStorageIsRefusedWithItsCode(t *testing.T) {
	t.Parallel()

	fleet := fleetWithFiles()
	fleet.refuse = connectorapi.PathNotAllowed.New("the path leaves the storage it names").
		With(fault.StringParam("path", "usb/../etc"))

	rec := call(t, serverWithFleet(t, fleet).Handler(),
		http.MethodGet, "/api/v1/devices/pdeadbeef/files?path=usb%2F..%2Fetc", "")

	body := requireProblem(t, rec, http.StatusBadRequest)
	require.Equal(t, connectorapi.PathNotAllowed.Code(), body.Code)
}

// A printer the caller holds nothing on and one that is not there are the same
// refusal on these paths too. A surface that answered differently would let a
// caller enumerate printers they may not see.
func TestBothFileRoutesRefuseAnUnknownPrinterAsNotFound(t *testing.T) {
	t.Parallel()

	handler := serverWithFleet(t, fleetWithFiles()).Handler()

	for _, path := range []string{
		"/api/v1/devices/pnothing/storages",
		"/api/v1/devices/pnothing/files?path=usb",
	} {
		rec := call(t, handler, http.MethodGet, path, "")
		body := requireProblem(t, rec, http.StatusNotFound)
		require.Equal(t, fault.PrinterNotFound.Code(), body.Code)
	}
}

// The transfer listing is what a caller watching an upload polls when it is not
// subscribed, so it has to carry the byte counts and not only the state.
func TestTheTransferListingCarriesTheByteCountsAndTheState(t *testing.T) {
	t.Parallel()

	fleet := fleetWithFiles()
	started := time.Date(2025, time.August, 26, 9, 14, 5, 0, time.UTC)
	earlier := started.Add(-24 * time.Hour)
	stopped := earlier.Add(56 * time.Second)
	fleet.transfers = []DeviceTransfer{
		{
			TransferID: "01SENDING", Storage: "scratch", Source: "part.bgcode",
			Path: "usb/part.bgcode", BytesTotal: 4096, BytesSent: 1024, State: "sending",
			StartedAt: started,
		},
		{
			TransferID: "01INTERRUPTED", Storage: "scratch", Source: "old.bgcode",
			Path: "usb/old.bgcode", BytesTotal: 2048, BytesSent: 512, State: "interrupted",
			Reason: "server restarted", StartedAt: earlier, EndedAt: &stopped,
		},
	}

	rec := call(t, serverWithFleet(t, fleet).Handler(),
		http.MethodGet, "/api/v1/devices/pdeadbeef/transfers", "")

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Items []DeviceTransfer `json:"items"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 2)
	require.Equal(t, int64(1024), body.Items[0].BytesSent)
	require.Equal(t, "sending", body.Items[0].State)
	require.Equal(t, "server restarted", body.Items[1].Reason)

	t.Logf("observed: %s", rec.Body.String())
}

// Stopping a transfer answers with nothing at all, and the id reaches the
// plane. A 204 is the whole answer: what the transfer ended as is on its own
// record and on the event stream, not in the answer to the request that ended
// it.
func TestStoppingATransferAnswersNothingAndNamesTheTransfer(t *testing.T) {
	t.Parallel()

	fleet := fleetWithFiles()

	rec := call(t, serverWithFleet(t, fleet).Handler(),
		http.MethodPost, "/api/v1/devices/pdeadbeef/transfers/01SENDING/stop", "")

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, []string{"01SENDING"}, fleet.stopped)

	t.Logf("observed: 204, the plane was asked to stop %s", fleet.stopped[0])
}

// A printer file's picture is served with the storage thumbnail's headers, and
// a client holding the same entity tag is answered 304 with no body. A file
// with no picture is a 404 carrying the storage code.
func TestAPrinterFileThumbnailIsServedAndRevalidated(t *testing.T) {
	t.Parallel()

	fleet := fleetWithFiles()
	fleet.picture = StorageThumbnail{
		Bytes:     []byte("\x89PNG\r\n\x1a\n a picture"),
		MediaType: "image/png",
		Modified:  time.Unix(1_750_000_000, 0).UTC(),
		Version:   "4096-1750000000",
	}

	handler := serverWithFleet(t, fleet).Handler()
	path := "/api/v1/devices/pdeadbeef/files/thumbnail?path=usb%2Fmodels%2Fpart.bgcode&size=4096&modified=1750000000"

	rec := call(t, handler, http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	require.Equal(t, `"4096-1750000000"`, rec.Header().Get("ETag"))
	require.Equal(t, "private, no-cache", rec.Header().Get("Cache-Control"))
	require.Equal(t, fleet.picture.Bytes, rec.Body.Bytes())

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("If-None-Match", `"4096-1750000000"`)

	current := httptest.NewRecorder()
	handler.ServeHTTP(current, req)
	require.Equal(t, http.StatusNotModified, current.Code)
	require.Empty(t, current.Body.Bytes())

	fleet.picture = StorageThumbnail{}

	missing := call(t, handler, http.MethodGet, path, "")
	require.Equal(t, http.StatusNotFound, missing.Code)
	require.Contains(t, missing.Body.String(), "core.storage/NO_THUMBNAIL")
}
