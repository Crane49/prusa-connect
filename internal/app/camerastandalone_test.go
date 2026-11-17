package app

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// standaloneCamera is a declaration with no printer key at all, which is
// a design requirement's host camera and IP camera: neither has a printer.
func standaloneCamera(device *cameraDevice) string {
	return `{"type":"mjpeg","config":{` +
		`"snapshot_url":"` + device.snapshotURL() + `",` +
		`"stream_url":"` + device.streamURL() + `",` +
		`"min_interval":"1h"}}`
}

// TestACameraBoundToNoPrinterIsDeclaredGrantedAndServed holds ADR-20260826-5yj rules 14
// and 16 through the real composition root, which is the gate the record's
// enforcement table names for them.
//
// Three claims. The declaration is accepted, which is rule 16. The camera
// carries grants of its own, so an administrator reads and writes the grants
// written on it through the same path a printer's take, which is rule 14. And
// it serves a frame to a caller the graph reaches it for, which is the whole
// point of the other two: a camera no principal can be granted is a camera
// nobody can watch.
func TestACameraBoundToNoPrinterIsDeclaredGrantedAndServed(t *testing.T) {
	t.Parallel()

	device := newCameraDevice(t)
	running := start(t, t.TempDir(), baseConfig())

	written := running.put(t, "/api/v1/cameras/"+cameraName, standaloneCamera(device))
	require.Equal(t, http.StatusCreated, written.status, written.body)

	requireCameraReady(t, running)

	listed := listedCameras(t, running)
	require.Len(t, listed, 1, "a camera bound to no printer reached nobody")
	require.Equal(t, cameraName, listed[0].Name)
	require.Empty(t, listed[0].DeviceID, "a camera that watches no printer reported one")

	// The camera is an object in the graph, so its grants are administered
	// through the same path a printer's are. A row here is rule 14: an id, and
	// grants of its own.
	held := running.get(t, "/api/v1/grants/device/"+cameraName)
	require.Equal(t, http.StatusOK, held.status, held.body)

	grant := running.call(t, http.MethodPost, "/api/v1/grants/device/"+cameraName,
		`{"relation":"viewer","subject":"user:nina"}`)
	require.Equal(t, http.StatusCreated, grant.status, grant.body)

	require.Contains(t, grantSubjects(t, running, cameraName), "user:nina",
		"the grant was written and the camera does not carry it")

	// And the frames are served to the caller the graph reaches it for.
	shot := running.raw(t, "/api/v1/cameras/"+cameraName+"/snapshot", nil)
	require.Equal(t, http.StatusOK, shot.status, string(shot.body))
	require.Equal(t, jpeg(0x01), shot.body)
}

// grantSubjects reads back who holds something on one device.
func grantSubjects(t *testing.T, running *process, device string) []string {
	t.Helper()

	read := running.get(t, "/api/v1/grants/device/"+device)
	require.Equal(t, http.StatusOK, read.status, read.body)

	var listing struct {
		Items []struct {
			Subject string `json:"subject"`
		} `json:"items"`
	}

	require.NoError(t, json.Unmarshal([]byte(read.body), &listing))

	out := make([]string, 0, len(listing.Items))
	for _, item := range listing.Items {
		out = append(out, item.Subject)
	}

	return out
}
