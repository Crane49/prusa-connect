package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"github.com/prusa3d/server-slicing/internal/fixture"
	"github.com/prusa3d/server-slicing/internal/gcodemeta"
	"github.com/prusa3d/server-slicing/internal/jobs"
	"github.com/prusa3d/server-slicing/internal/runner"
	"github.com/prusa3d/server-slicing/internal/slicers"
)

// The real PrusaSlicer 2.9.6, downloaded from its GitHub release, installed
// through the routes and run on a small project. It needs the network and a
// Mac, so it runs only when SLICER_INTEGRATION is 1, and on macOS. The
// download is about 140 MB. SLICER_INTEGRATION_CACHE names a folder that keeps
// it between runs, under its SHA-256.
func TestTheRealSlicerSlicesASmallProject(t *testing.T) {
	if os.Getenv("SLICER_INTEGRATION") != "1" {
		t.Skip("set SLICER_INTEGRATION=1 to download and run PrusaSlicer 2.9.6")
	}

	if runtime.GOOS != "darwin" {
		t.Skip("the GitHub release of PrusaSlicer 2.9.6 runs on macOS and Windows, and this test unpacks the macOS one")
	}

	data := t.TempDir()

	if cache := os.Getenv("SLICER_INTEGRATION_CACHE"); cache != "" {
		if err := os.MkdirAll(cache, 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.Symlink(cache, filepath.Join(data, "cache")); err != nil {
			t.Fatal(err)
		}
	}

	r := configuredRig(t, json.RawMessage(`{"github_releases":true,"timeout_minutes":10}`), wirerpc.Grants{Data: data})

	var view slicers.View

	// The module reads the release list from GitHub after it starts.
	for start := time.Now(); time.Since(start) < time.Minute; time.Sleep(time.Second) {
		r.as(t, root, http.MethodGet, "/slicers", nil, &view)

		if offered(view, "2.9.6") && offered(view, "3.0.0-alpha12") {
			break
		}
	}

	if status := r.as(
		t,
		root,
		http.MethodPost,
		"/slicers/install",
		[]byte(`{"version":"2.9.6"}`),
		&view,
	); status != http.StatusAccepted {
		t.Fatalf("the install answered %d", status)
	}

	deadline := time.Now().Add(15 * time.Minute)

	for time.Now().Before(deadline) {
		r.as(t, root, http.MethodGet, "/slicers", nil, &view)

		if len(view.Installed) == 1 || (len(view.Installing) == 1 && view.Installing[0].Error != "") {
			break
		}

		time.Sleep(time.Second)
	}

	if len(view.Installed) != 1 || view.Default != "2.9.6" {
		t.Fatalf("the install ended as %+v", view)
	}

	r.host.Put("cubes/two.3mf", cubesAt(t, [][2]float64{{100, 100}, {130, 100}}))

	var job jobs.Job
	if status := r.as(
		t,
		alice,
		http.MethodPost,
		"/jobs",
		[]byte(
			`{"source":{"storage":"shelf","path":"cubes/two.3mf"},"format":"bgcode"}`,
		),
		&job,
	); status != http.StatusAccepted {
		t.Fatalf("the job answered %d", status)
	}

	done := r.waitJob(t, job.ID)
	if done.State != jobs.Done {
		t.Fatalf("the job ended as %s %+v", done.State, done.Error)
	}

	result := done.Result
	t.Logf("sliced in %d ms, peak %d MiB, %d s of printing, %.1f mm of filament, objects %+v",
		result.DurationMS, result.PeakMemoryBytes>>20, result.EstimatedSeconds, result.FilamentMM, result.Objects)

	want := jobs.OutputPath(jobs.Target{Storage: "shelf", Path: "cubes/two"}, runner.Binary)
	if result.Path != want || result.Format != "bgcode" || result.Slicer != "2.9.6" {
		t.Fatalf("the result is %+v", result)
	}

	if result.EstimatedSeconds <= 0 || result.FilamentMM <= 0 || len(result.Objects) != 2 {
		t.Fatalf("the details are %+v", result)
	}

	written, ok := r.host.File(want)
	if !ok || !bytes.HasPrefix(written, []byte("GCDE")) {
		t.Fatal("the storage holds no binary G-code")
	}

	// The file the module wrote reads the same through the parser on its own.
	path := filepath.Join(t.TempDir(), "two.bgcode")
	if err := os.WriteFile(path, written, 0o600); err != nil {
		t.Fatal(err)
	}

	meta, err := gcodemeta.Read(path)
	if err != nil || !strings.HasPrefix(meta.Producer, "PrusaSlicer 2.9.6") {
		t.Fatalf("the file reads as %+v, %v", meta, err)
	}

	// PrusaSlicer 3.0.0-alpha12 is installed beside it and slices a project
	// it saved when a job names it. It writes text G-code whatever it is
	// asked, and the job says so.
	if status := r.as(
		t,
		root,
		http.MethodPost,
		"/slicers/install",
		[]byte(`{"version":"3.0.0-alpha12"}`),
		&view,
	); status != http.StatusAccepted {
		t.Fatalf("the second install answered %d", status)
	}

	for time.Now().Before(deadline) {
		r.as(t, root, http.MethodGet, "/slicers", nil, &view)

		if len(view.Installed) == 2 || (len(view.Installing) == 1 && view.Installing[0].Error != "") {
			break
		}

		time.Sleep(time.Second)
	}

	if len(view.Installed) != 2 || view.Default != "2.9.6" {
		t.Fatalf("the second install ended as %+v", view)
	}

	saved, err := os.ReadFile(filepath.Join("..", "..", "sdk", "threemf", "testdata",
		"prusaslicer-3.0.0-alpha12-two-setups.3mf"))
	if err != nil {
		t.Fatal(err)
	}

	r.host.Put("setups/two.3mf", saved)

	if status := r.as(
		t,
		alice,
		http.MethodPost,
		"/jobs",
		[]byte(`{"source":{"storage":"shelf","path":"setups/two.3mf"},`+
			`"slicer":"3.0.0-alpha12","format":"bgcode"}`),
		&job,
	); status != http.StatusAccepted {
		t.Fatalf("the job for 3.0.0-alpha12 answered %d", status)
	}

	three := r.waitJob(t, job.ID)
	if three.State != jobs.Done || three.Result.Slicer != "3.0.0-alpha12" || len(three.Result.Objects) == 0 {
		t.Fatalf("the job for 3.0.0-alpha12 ended as %s %+v %+v", three.State, three.Error, three.Result)
	}

	t.Logf("3.0.0-alpha12 wrote %s, %s, in %d ms, peak %d MiB, warnings %v", three.Result.Path, three.Result.Format,
		three.Result.DurationMS, three.Result.PeakMemoryBytes>>20, three.Result.Warnings)

	// Part of a project: the second bed alone, and one cube of the first. The
	// G-code holds the chosen cubes only, and each is matched to its item.
	r.host.Put("cubes/beds.3mf", cubesAt(t, [][2]float64{{100, 100}, {fixture.Bed1X + 100, 100}, {130, 100}}))

	for _, held := range []struct {
		selection string
		item      int
	}{{`{"bed":1}`, 1}, {`{"bed":0,"items":[2]}`, 2}} {
		if status := r.as(
			t,
			alice,
			http.MethodPost,
			"/jobs",
			[]byte(`{"source":{"storage":"shelf","path":"cubes/beds.3mf"},`+
				`"overwrite":true,"selection":`+held.selection+`}`),
			&job,
		); status != http.StatusAccepted {
			t.Fatalf("the job for %s answered %d", held.selection, status)
		}

		part := r.waitJob(t, job.ID)
		if part.State != jobs.Done || len(part.Result.Objects) != 1 || part.Result.Objects[0].Item == nil ||
			*part.Result.Objects[0].Item != held.item {
			t.Fatalf("the job for %s ended as %s %+v %+v", held.selection, part.State, part.Error, part.Result)
		}

		t.Logf("%s sliced item %d, labelled %q", held.selection, held.item, part.Result.Objects[0].Name)
	}

	// A project whose objects are all off the bed is a job with nothing to
	// print. PrusaSlicer would exit with 0 and write nothing, and the module
	// says so before it runs.
	r.host.Put("cubes/off.3mf", cubesAt(t, [][2]float64{{900, 900}}))

	if status := r.as(t, alice, http.MethodPost, "/jobs",
		[]byte(`{"source":{"storage":"shelf","path":"cubes/off.3mf"}}`), &job); status != http.StatusAccepted {
		t.Fatalf("the second job answered %d", status)
	}

	if off := r.waitJob(t, job.ID); off.Error == nil || off.Error.Code != jobs.CodeNothingToPrint {
		t.Fatalf("a project off the bed ended as %s %+v", off.State, off.Error)
	}
}

// cubesAt is a project of one 10 mm cube placed once at each point, for a
// printer with a 200 by 200 mm bed whose G-code labels each object.
func cubesAt(t *testing.T, at [][2]float64) []byte {
	t.Helper()

	data, err := fixture.Project{At: at, Settings: fixture.Bed200}.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	return data
}

// offered reports a version the page may install.
func offered(view slicers.View, version string) bool {
	for _, offer := range view.Available {
		if offer.Version == version && offer.Compatible {
			return true
		}
	}

	return false
}
