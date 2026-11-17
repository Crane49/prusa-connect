package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/tools/camerasim"
)

// demoCameraFPS is how fast a simulated camera renders. Five frames a second is
// enough for a browser to read as moving and slow enough that three of them
// cost nothing measurable next to the fleet they sit beside.
const demoCameraFPS = 5

// demoCameraMinInterval is what the driver is told about the device's own
// cadence. The shipped default is ten seconds, which is right for a camera on
// somebody's network and wrong for a demonstration: a snapshot that is nine
// seconds stale reads as a frozen picture. The simulated device is in this
// process, so fetching from it costs nothing worth rate-limiting.
const demoCameraMinInterval = "200ms"

// demoCameras starts one simulated camera per printer and adds it to that
// printer, through the same camera API an operator would use.
//
// It runs after the fleet has arrived rather than from the configuration file,
// and the reason is the identity. A camera is bound to a printer by the id this
// server minted, and that id exists only once the machine has dialled in, so a
// camera written into the start-up configuration would have to name an id
// nobody has issued yet. Declaring it over the API is also the path worth
// demonstrating: it is what an operator does when they hang a camera on a
// printer that is already running.
//
// Nothing here is fatal. A demonstration whose printers all work and whose
// cameras did not start is still the demonstration; the lines it prints say
// which cameras are there, so a missing one is visible rather than silent.
func (f *demoFleet) demoCameras(ctx context.Context, opts DemoOptions, page demoListing) []string {
	if !opts.Cameras {
		return nil
	}

	lines := make([]string, len(page.Items))

	// The declarations run side by side. Each one is a request the core may
	// answer late under load, and six of them in a row is six times that
	// wait before the last camera is listed; side by side it is one wait.
	var declared sync.WaitGroup

	for i, printer := range page.Items {
		name := demoCameraName(printer.Serial)

		source, err := camerasim.Start(ctx, camerasim.Options{
			Addr:   "127.0.0.1:0",
			FPS:    demoCameraFPS,
			Label:  printer.Serial,
			Status: f.printerStatus(printer.Serial),
		})
		if err != nil {
			lines[i] = fmt.Sprintf("  %-10s no camera: %s", printer.Serial, err.Error())

			continue
		}

		f.cameras = append(f.cameras, source)

		declared.Go(func() {
			if refused := f.declareDemoCamera(ctx, opts, name, printer.ID, source); refused != nil {
				lines[i] = fmt.Sprintf("  %-10s no camera: %s", printer.Serial, refused.Error())

				return
			}

			lines[i] = fmt.Sprintf("  %-10s %s", printer.Serial,
				opts.baseURL()+"/api/v1/cameras/"+name+"/stream")
		})
	}

	declared.Wait()

	return lines
}

// declareDemoCamera adds one camera through the camera API.
//
// The declaration is the mjpeg driver pointed at the simulated device, which is
// the same declaration an operator writes for an ESP32-CAM: two URLs and the
// printer the camera watches. Nothing about it is demonstration-only except the
// cadence, which is explained where it is declared.
func (f *demoFleet) declareDemoCamera(
	ctx context.Context, opts DemoOptions, name, printer string, source *camerasim.Server,
) error {
	body := fmt.Sprintf(
		`{"type":"mjpeg","config":{`+
			`"printer":%q,"snapshot_url":%q,"stream_url":%q,"min_interval":%q}}`,
		printer, source.SnapshotURL(), source.StreamURL(), demoCameraMinInterval)

	url := opts.baseURL() + "/api/v1/cameras/" + name

	// If-None-Match: * is what makes this a create rather than a blind
	// overwrite. The camera API fences every write with a token, and a write
	// carrying none is refused with 428 rather than allowed to clobber whatever
	// somebody else wrote under the same name.
	status, err := f.retry(ctx, func() (int, error) {
		return f.write(ctx, http.MethodPut, url, body,
			http.Header{"If-None-Match": []string{"*"}})
	})
	if err != nil {
		return err
	}

	// A kept directory already holds this camera from the last run, and the
	// create is refused with 412. The row is the demo's own, so it is written
	// over with the version the settings listing answers, quoted in If-Match.
	if status == http.StatusPreconditionFailed {
		version, readErr := f.version(ctx, opts.baseURL(), name)
		if readErr != nil {
			return readErr
		}

		status, err = f.retry(ctx, func() (int, error) {
			return f.write(ctx, http.MethodPut, url, body,
				http.Header{"If-Match": []string{version}})
		})
		if err != nil {
			return err
		}
	}

	if status != http.StatusCreated && status != http.StatusOK {
		return fmt.Errorf("the camera declaration was refused with status %d", status)
	}

	return nil
}

// printerStatus is what the camera watching one printer draws on every frame:
// the machine's own state and how far its print has gone.
//
// It reads the simulated printer rather than the API, because the camera and
// the machine are on the same side of the wire. A camera that polled this
// server for the state of the printer in front of it would be drawing what the
// server believes rather than what the machine is doing, and the two disagreeing
// is one of the things a person runs the demonstration to see.
//
// A serial with no printer behind it reports nothing, which draws a frame with
// no state line rather than one that says IDLE about a machine nobody found.
func (f *demoFleet) printerStatus(serial string) func() camerasim.Status {
	for _, machine := range f.printers {
		if machine.Serial() != serial {
			continue
		}

		return func() camerasim.Status {
			return camerasim.Status{State: machine.State(), Progress: machine.Progress()}
		}
	}

	return nil
}

// stopDemoCameras shuts the simulated devices down. The core stops them as
// resources when it stops; these are the servers on the other end of the wire,
// which nothing else owns.
func (f *demoFleet) stopDemoCameras() {
	for _, source := range f.cameras {
		_ = source.Close()
	}

	f.cameras = nil
}

// demoCameraName is the instance name one printer's camera is declared under.
// It is derived from the serial rather than from the minted id, so the name a
// person reads in a URL is the name on the machine's label.
func demoCameraName(serial string) string {
	return "cam-" + strings.ToLower(strings.ReplaceAll(serial, "_", "-"))
}

// version reads one camera's version from the settings listing, quoted the
// way If-Match takes it.
func (f *demoFleet) version(ctx context.Context, base, name string) (string, error) {
	url := base + "/api/v1/camera-settings"

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build GET %s: %w", url, err)
	}

	if f.token != "" {
		request.Header.Set("Authorization", "Bearer "+f.token)
	}

	response, err := f.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the camera settings could not be read: status %d", response.StatusCode)
	}

	var listing struct {
		Items []httpapi.CameraSetting `json:"items"`
	}

	if err = json.NewDecoder(response.Body).Decode(&listing); err != nil {
		return "", fmt.Errorf("read the camera settings: %w", err)
	}

	for _, camera := range listing.Items {
		if camera.Name == name {
			return `"` + camera.Version + `"`, nil
		}
	}

	return "", errors.New("the camera settings do not list " + name)
}
