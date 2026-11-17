package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"

	"github.com/prusa3d/threemf/internal/analysis"
	"github.com/prusa3d/threemf/internal/document"
	"github.com/prusa3d/threemf/internal/projects"
	"github.com/prusa3d/threemf/threemf"
)

// The routes and the answer to the core, each driven through the method set
// the host calls (ADR-20261027-4tq, ADR-20261027-wvj).

const multibed = "storage=local&path=projects/multibed.3mf"

// refusal reads the code out of a refused answer.
func refusal(t *testing.T, response routesapi.Response, status int, code string) {
	t.Helper()

	var body struct {
		Code   string         `json:"code"`
		Params map[string]any `json:"params"`
	}

	if err := json.Unmarshal(response.Body, &body); err != nil {
		t.Fatalf("the refusal is not JSON: %s", response.Body)
	}

	if response.Status != status || body.Code != code {
		t.Fatalf("got %d %s, want %d %s: %s", response.Status, body.Code, status, code, response.Body)
	}
}

// A PrusaSlicer 2.9 project with three beds answers its beds, its objects and
// where each copy stands, with the outlines in the coordinates of each bed.
func TestAProjectAnswersItsBedsAndWhereEachCopyStands(t *testing.T) {
	t.Parallel()

	serves, _ := aModule(t)

	response := get(t, serves, "/project", multibed)
	if response.Status != http.StatusOK {
		t.Fatalf("answered %d: %s", response.Status, response.Body)
	}

	var project document.Project
	if err := json.Unmarshal(response.Body, &project); err != nil {
		t.Fatal(err)
	}

	if project.Producer.Name != "PrusaSlicer" || project.Producer.Dialect != "prusaslicer2" ||
		!project.Producer.Supported || project.Title != "real296" {
		t.Fatalf("producer %+v, title %q", project.Producer, project.Title)
	}

	if len(project.Setups) != 1 || len(project.Setups[0].Beds) != 3 || project.Counts.Beds != 3 {
		t.Fatalf("setups %d, beds %d", len(project.Setups), project.Counts.Beds)
	}

	setup := project.Setups[0]
	if setup.PrinterModel != "MK4S" || setup.RawSettings != nil {
		t.Fatalf("printer %q, raw settings sent unasked: %v", setup.PrinterModel, setup.RawSettings != nil)
	}

	if setup.Settings.InfillPattern != "stars" || setup.Settings.Perimeters != 3 ||
		len(setup.Settings.Filaments) != 1 || setup.Settings.Filaments[0].Colour != "#29B2B2" {
		t.Fatalf("settings %+v", setup.Settings)
	}

	// Every copy on the second bed is drawn inside that bed, at the origin,
	// although the bed stands to the right of the first in the project.
	second := setup.Beds[1]
	if second.Offset.X <= 0 {
		t.Fatalf("the second bed stands at %v", second.Offset)
	}

	for _, ref := range second.Instances {
		instance := project.Objects[ref.Object].Instances[ref.Instance]
		if instance.Bed == nil || instance.Bed.Bed != 1 || len(instance.Outline) == 0 {
			t.Fatalf("instance %+v", instance)
		}

		for _, point := range instance.Outline {
			if point.X < -1e-6 || point.X > second.Size.X+1e-6 {
				t.Fatalf("a point at x %v is outside the bed of width %v", point.X, second.Size.X)
			}
		}
	}

	raw := get(t, serves, "/project", multibed+"&raw_settings=true")

	var withRaw document.Project
	if err := json.Unmarshal(raw.Body, &withRaw); err != nil {
		t.Fatal(err)
	}

	if len(withRaw.Setups[0].RawSettings) == 0 ||
		withRaw.Setups[0].RawSettings[0].Values["layer_height"] != "0.3" {
		t.Fatalf("raw settings %v", withRaw.Setups[0].RawSettings)
	}
}

// A PrusaSlicer 3 project answers each printer setup with its own beds.
func TestAProjectAnswersEveryPrinterSetup(t *testing.T) {
	t.Parallel()

	serves, files := aModule(t)
	files.Put(storage, "two.3mf", fixture(t, "prusaslicer-3.0.0-alpha12-two-setups.3mf"), 1)

	var project document.Project
	if err := json.Unmarshal(get(t, serves, "/project", "storage=local&path=two.3mf").Body, &project); err != nil {
		t.Fatal(err)
	}

	if project.Producer.Dialect != "prusaslicer3" || len(project.Setups) != 2 ||
		project.Setups[0].PrinterModel != "COREONE" || project.Setups[1].PrinterModel != "MINI" ||
		project.Setups[0].Settings.Supports != "enforcers_only" {
		t.Fatalf("project %+v", project.Setups)
	}
}

// A project's pictures are served by their index, a client that has the
// picture is answered 304, and an index the project has no picture for is
// refused.
func TestAProjectServesItsPicturesByIndex(t *testing.T) {
	t.Parallel()

	serves, _ := aModule(t)

	picture := get(t, serves, "/project/thumbnail", multibed+"&index=0")
	if picture.Status != http.StatusOK || len(picture.Body) == 0 ||
		!strings.HasPrefix(picture.Headers["Content-Type"][0], "image/") ||
		strings.Join(picture.Headers["X-Content-Type-Options"], ",") != "nosniff" {
		t.Fatalf("answered %d %v", picture.Status, picture.Headers)
	}

	tag := picture.Headers["Etag"]
	if len(tag) == 0 {
		t.Fatalf("no entity tag: %v", picture.Headers)
	}

	again := call(t, serves, routesapi.Request{
		Method: "GET", Path: "/project/thumbnail", Query: multibed + "&index=0", Principal: alice,
		Headers: map[string][]string{"If-None-Match": tag},
	})
	if again.Status != http.StatusNotModified || len(again.Body) != 0 {
		t.Fatalf("a current copy answered %d with %d bytes", again.Status, len(again.Body))
	}

	refusal(t, get(t, serves, "/project/thumbnail", multibed+"&index=1000"), http.StatusNotFound,
		"threemf/NO_THUMBNAIL")
}

// A file that is not a 3MF project is refused with its own code, both when
// its name says so and when its bytes do, and a file that is not there is
// refused as missing.
func TestAFileThatIsNotAProjectIsRefused(t *testing.T) {
	t.Parallel()

	serves, files := aModule(t)
	files.Put(storage, "part.gcode", []byte("G28"), 1)
	files.Put(storage, "renamed.3mf", []byte("G28"), 1)

	refusal(t, get(t, serves, "/project", "storage=local&path=part.gcode"),
		http.StatusUnprocessableEntity, "threemf/NOT_A_PROJECT")
	refusal(t, get(t, serves, "/project", "storage=local&path=renamed.3mf"),
		http.StatusUnprocessableEntity, "threemf/NOT_A_PROJECT")
	refusal(t, get(t, serves, "/project", "storage=local&path=missing.3mf"),
		http.StatusNotFound, "threemf/NO_FILE")
	refusal(t, get(t, serves, "/project/mesh", "storage=local&path=part.gcode&object=0"),
		http.StatusUnprocessableEntity, "threemf/NOT_A_PROJECT")
}

// The file is read as the person the host named, so a person the storage
// refuses is refused here, also for a project another person already read.
func TestAProjectIsReadAsThePerson(t *testing.T) {
	t.Parallel()

	serves, files := aModule(t)
	files.Deny("user:nobody")

	if response := get(t, serves, "/project", multibed); response.Status != http.StatusOK {
		t.Fatalf("alice was refused: %s", response.Body)
	}

	for _, path := range []string{"/project", "/project/thumbnail", "/project/mesh"} {
		refusal(t, call(t, serves, routesapi.Request{
			Method: "GET", Path: path, Query: multibed + "&object=0", Principal: "user:nobody",
		}), http.StatusForbidden, "threemf/DENIED")
	}

	refusal(t, call(t, serves, routesapi.Request{Method: "GET", Path: "/project", Query: multibed}),
		http.StatusForbidden, "threemf/NO_PRINCIPAL")
}

// One version of a file is read once however many callers ask, and the
// pictures come from the same read.
func TestAProjectIsReadOnce(t *testing.T) {
	t.Parallel()

	serves, files := aModule(t)

	for range 3 {
		get(t, serves, "/project", multibed)
		get(t, serves, "/project/thumbnail", multibed)
	}

	if opened := files.Opens.Load(); opened != 1 {
		t.Errorf("the file was opened %d times, want 1", opened)
	}

	// A new version of the file is a new read.
	files.Put(storage, "projects/multibed.3mf", fixture(t, "prusaslicer-2.9.6-multibed.3mf"), 1_761_000_001)
	get(t, serves, "/project", multibed)

	if opened := files.Opens.Load(); opened != 2 {
		t.Errorf("a new version was opened %d times in all, want 2", opened)
	}
}

// An object of a project answers its triangles, part by part, and an index
// the project has no object at is refused.
func TestAnObjectAnswersItsMesh(t *testing.T) {
	t.Parallel()

	serves, _ := aModule(t)

	response := get(t, serves, "/project/mesh", multibed+"&object=1")
	if response.Status != http.StatusOK {
		t.Fatalf("answered %d: %s", response.Status, response.Body)
	}

	var mesh document.ProjectMesh
	if err := json.Unmarshal(response.Body, &mesh); err != nil {
		t.Fatal(err)
	}

	sum := 0
	for _, part := range mesh.Parts {
		sum += part.Triangles
	}

	if mesh.Object != 1 || len(mesh.Parts) == 0 || mesh.SourceTriangles <= 0 ||
		len(mesh.Triangles) != 3*mesh.SourceTriangles || sum != len(mesh.Triangles)/3 {
		t.Fatalf("object %d, %d parts, %d triangles of %d", mesh.Object, len(mesh.Parts),
			len(mesh.Triangles)/3, mesh.SourceTriangles)
	}

	for _, index := range mesh.Triangles {
		if int(index) >= len(mesh.Vertices)/3 {
			t.Fatalf("index %d is past the %d vertices", index, len(mesh.Vertices)/3)
		}
	}

	refusal(t, get(t, serves, "/project/mesh", multibed+"&object=99"), http.StatusNotFound, "threemf/NO_OBJECT")
	refusal(t, get(t, serves, "/project/mesh", multibed), http.StatusBadRequest, "threemf/INVALID_QUERY")
}

// The core is told what a project holds, within the bounds sdk/fileapi
// publishes: the picture, the title, the printer, the facts and the objects.
func TestTheCoreIsToldWhatAProjectHolds(t *testing.T) {
	t.Parallel()

	raw := fixture(t, "prusaslicer-2.9.6-multibed.3mf")

	project, err := projects.New(nil).Analyze(context.Background(), sized{bytes.NewReader(raw)})
	if err != nil {
		t.Fatal(err)
	}

	answer := analysis.Of(project)
	if err = answer.Validate(); err != nil {
		t.Fatalf("the host would refuse the answer: %v", err)
	}

	if answer.Title != "real296" || len(answer.Thumbnails) == 0 || answer.Print == nil ||
		answer.Print.PrinterModel != "MK4S" || answer.Print.NozzleMM <= 0 || len(answer.Objects) == 0 {
		t.Fatalf("answer %+v", answer)
	}

	facts := map[string]string{}
	for _, fact := range answer.Facts {
		facts[fact.Key] = fact.Value
	}

	if facts["beds"] != "3" || facts["printer"] != "MK4S" || facts["producer"] == "" {
		t.Errorf("facts %v", facts)
	}

	if _, err = projects.New(nil).Analyze(context.Background(), sized{bytes.NewReader([]byte("G28"))}); err == nil ||
		!strings.Contains(err.Error(), threemf.ErrFormat.Error()) {
		t.Errorf("a file that is no zip answered %v", err)
	}
}

// sized is a reader over bytes with the size projects.File asks for.
type sized struct{ *bytes.Reader }
