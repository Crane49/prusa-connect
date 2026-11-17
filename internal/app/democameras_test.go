package app

import (
	"context"
	stilljpeg "image/jpeg"
	"net/http"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTheDemoGivesEveryPrinterACameraAndAnswersItsGCode is the gate under the
// two things the demonstration is for: a page that shows a picture of every
// machine, and a bench that answers a line somebody typed.
//
// It reads both the way a person does. The camera listing, one still image off
// the media path, and one G-code line through the published gcode operation. A
// demonstration whose cameras are declared and serve nothing still lists them,
// which is the quiet failure this refuses.
func TestTheDemoGivesEveryPrinterACameraAndAnswersItsGCode(t *testing.T) {
	t.Parallel()

	addr := freeAddr(t)

	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	// The demo's own lines are kept and shown when the case fails: a listing
	// that never fills is explained by the demo saying "no camera" and why,
	// which io.Discard throws away.
	said := &demoSaid{}

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the demo said:\n%s", said.text())
		}
	})

	go func() {
		done <- Demo(ctx, DemoOptions{
			APIAddr: addr, Flap: 0, Cameras: true,
			Password: demoTestPassword, Out: said,
		})
	}()

	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done, "a demo the person stopped must stop cleanly")
	})

	base := "http://" + addr
	reader := &demoFleet{client: &http.Client{Timeout: demoRequest}}

	require.Eventually(t, func() bool {
		token, err := reader.signIn(t.Context(), DemoOptions{APIAddr: addr},
			demoLogin{username: demoUsername, password: demoTestPassword})
		reader.token = token

		return err == nil
	}, demoSettle, demoPoll, "the demo account never signed in")

	var fleet demoListing

	require.Eventually(t, func() bool {
		fleet = demoRead[demoListing](t, reader, base+"/api/v1/devices")

		return connectedIn(fleet) >= demoFleetSize
	}, demoSettle, demoPoll, "the demo never listed %d connected printers", demoFleetSize)

	// One camera per printer, and each one bound to the printer it watches. The
	// declaration is written over the resource API after the fleet has arrived
	// and the instance is started after that, so this waits for the binding
	// rather than for the row: a camera listed with no printer is a camera whose
	// declaration landed and whose start-up has not finished. The budget is
	// doubled for the same reason the print wait's is: a loaded runner starts
	// several demonstrations at once, and this wait comes last.
	var cameras demoCameraListing

	// The listing the last finished poll read, shown when the case fails.
	// require.Eventually takes its message as ordinary arguments, so a listing
	// named there is the empty one this line declares and never what the poll
	// found. The poll runs on a goroutine of its own, so the value is passed
	// through a pointer the race detector is happy with.
	var seen atomic.Pointer[demoCameraListing]

	t.Cleanup(func() {
		last := seen.Load()
		if !t.Failed() || last == nil {
			return
		}

		t.Logf("the last camera listing read was %+v, against a fleet of %d machines",
			last.Items, len(fleet.Items))
	})

	require.Eventually(t, func() bool {
		page := demoRead[demoCameraListing](t, reader, base+"/api/v1/cameras")
		seen.Store(&page)
		cameras = page

		return watchedOnce(page, fleet)
	}, 2*demoSettle, demoPoll,
		"every machine in the fleet should have exactly one camera watching it")

	// A still image off the media path, which is what a grid tile renders. It
	// has to decode, because a camera that answered 200 with something that is
	// not an image is a broken tile and a working test.
	snapshotOf(t, reader, base, cameras.Items[0].Name)

	// One G-code line, through the operation a person's dev page sends it
	// through, and the reading the machine answered with. The listing is sorted
	// by urgency, so the first printer is whichever stands out at that moment.
	answered := demoGCode(t, reader, base, gcodeTaker(t, fleet), "M105")

	require.Len(t, answered.Steps, 1)
	require.Equal(t, "accepted", answered.Steps[0].Status,
		"the step was refused: %+v", answered.Steps[0])
	require.Contains(t, answered.Steps[0].Answer, "T:",
		"the printer answered the temperature request with %q", answered.Steps[0].Answer)
}

// watchedOnce reports whether every listed printer has exactly one camera
// watching it, and no camera watches a printer that is not in the fleet.
func watchedOnce(cameras demoCameraListing, fleet demoListing) bool {
	watching := make(map[string]int, len(cameras.Items))
	for _, camera := range cameras.Items {
		watching[camera.DeviceID]++
	}

	if len(watching) != len(fleet.Items) {
		return false
	}

	for _, printer := range fleet.Items {
		if watching[printer.ID] != 1 {
			return false
		}
	}

	return true
}

// TestAFleetReadTooEarlyNeverAgreesWithTheCameraListing holds the reason the
// case above waits for every machine and not only the printers.
//
// The demo gives a camera to every machine in its own listing, and it reads
// that listing only once all of them have arrived. A case that read the fleet
// while one machine was still dialling in would hold three machines beside four
// cameras. watchedOnce answers false for that and goes on answering false,
// whatever the server does, so the wait ends in a timeout that blames the
// cameras for a fleet the case read too early.
func TestAFleetReadTooEarlyNeverAgreesWithTheCameraListing(t *testing.T) {
	t.Parallel()

	whole := demoListing{Items: []demoDevice{
		{ID: "p1", Serial: "DEMO-01", Connected: true},
		{ID: "p2", Serial: "DEMO-02", Connected: true},
		{ID: "p3", Serial: "DEMO-03", Connected: true},
		{ID: "p4", Serial: demoEchoSerial, Connected: true},
	}}

	serving := demoCameraListing{}
	for _, machine := range whole.Items {
		serving.Items = append(serving.Items,
			demoCamera{Name: demoCameraName(machine.Serial), DeviceID: machine.ID})
	}

	require.True(t, watchedOnce(serving, whole),
		"the whole fleet and one camera each is what the demo stands up")

	early := demoListing{Items: whole.Items[:len(whole.Items)-1]}

	require.False(t, watchedOnce(serving, early),
		"a fleet read before the last machine arrived can never agree, so the case must not read one")
}

// demoCameraListing is the part of the camera listing these cases read.
type demoCameraListing struct {
	Items []demoCamera `json:"items"`
}

// demoCamera is one row of that listing.
type demoCamera struct {
	Name     string `json:"name"`
	DeviceID string `json:"device_id"`
}

// demoGCodeOutcome is the part of the gcode answer this case reads.
type demoGCodeOutcome struct {
	Steps []struct {
		Status        string `json:"status"`
		Answer        string `json:"answer"`
		Code          string `json:"code"`
		Detail        string `json:"detail"`
		MachineReason string `json:"machine_reason"`
	} `json:"steps"`
}

// snapshotOf fetches one camera's still image and returns its size, refusing
// anything that is not a JPEG this process can decode.
func snapshotOf(t *testing.T, reader *demoFleet, base, camera string) {
	t.Helper()

	url := base + "/api/v1/cameras/" + camera + "/snapshot"

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+reader.token)

	resp, err := reader.client.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "image/jpeg", resp.Header.Get("Content-Type"))

	img, err := stilljpeg.Decode(resp.Body)
	require.NoError(t, err)
	require.Positive(t, img.Bounds().Dx())
	require.Positive(t, img.Bounds().Dy())
}

// demoGCode sends one line through the published operation and decodes what
// became of it.
func demoGCode(t *testing.T, reader *demoFleet, base, device, line string) demoGCodeOutcome {
	t.Helper()

	var outcome demoGCodeOutcome

	status, err := reader.call(t.Context(), http.MethodPost,
		base+"/api/v1/devices/"+device+"/gcode", `{"lines":["`+line+`"]}`, &outcome)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	return outcome
}

// gcodeTaker is the first printer in the listing that takes a G-code line.
func gcodeTaker(t *testing.T, fleet demoListing) string {
	t.Helper()

	for _, device := range fleet.Items {
		if slices.Contains(device.Capabilities, "core.command.send_gcode") {
			return device.ID
		}
	}

	t.Fatalf("no printer in the demo fleet takes G-code: %+v", fleet.Items)

	return ""
}
