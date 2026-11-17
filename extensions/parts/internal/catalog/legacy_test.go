package catalog_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/stateapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"github.com/prusa3d/parts/internal/catalog"
	"github.com/prusa3d/parts/internal/dbtest"
	"github.com/prusa3d/parts/internal/hostfake"
)

// Releases up to 0.2.3 kept the catalogue in the server's record store. The
// cases here hold the move of those records into the module's own database:
// it moves everything a person kept, it runs once, and a move that cannot read
// the store writes nothing.

// seed writes one record straight into the store, the way an earlier release
// left it.
func seed(t *testing.T, records *hostfake.Records, key, body string) {
	t.Helper()

	if _, err := records.Put(context.Background(), stateapi.Key(key), []byte(body)); err != nil {
		t.Fatalf("seed %s: %v", key, err)
	}
}

// anEarlierCatalogue is what release 0.2.3 wrote for one folder, one part in
// it with two tags, one version with two files, and one comment.
func anEarlierCatalogue(t *testing.T) *hostfake.Records {
	t.Helper()

	records := &hostfake.Records{}

	seed(t, records, "counter", `{"v":1,"folder":2,"part":2,"comment":2}`)
	seed(t, records, "folder/0000000001",
		`{"v":1,"id":1,"parent":0,"name":"Garage","description":"Hooks",`+
			`"created":"2026-09-01T08:00:00Z","changed":"2026-09-01T08:00:00Z","changed_by":"user:alice"}`)
	seed(t, records, "part/0000000001",
		`{"v":1,"id":1,"folder":1,"name":"Bike hook","summary":"Holds a bicycle","tags":["bike","petg"],`+
			`"cover":"parts/1/a/hook.png","last_version":1,`+
			`"created":"2026-09-01T09:00:00Z","created_by":"user:alice",`+
			`"changed":"2026-09-02T09:00:00Z","changed_by":"user:bob"}`)
	seed(t, records, "version/0000000001/00001",
		`{"v":1,"part":1,"number":1,"note":"first print","files":[`+
			`{"name":"hook.png","storage":"local","path":"parts/1/a/hook.png","size":12,"sha256":"aa"},`+
			`{"name":"hook.bgcode","storage":"local","path":"parts/1/a/hook.bgcode","size":34}],`+
			`"created":"2026-09-01T10:00:00Z","created_by":"user:alice",`+
			`"changed":"2026-09-01T10:00:00Z","changed_by":"user:alice"}`)
	seed(t, records, "comment/0000000001/0000000001",
		`{"v":1,"id":1,"part":1,"body":"fits a 28 mm tyre","by":"user:bob",`+
			`"created":"2026-09-03T10:00:00Z","edited":"2026-09-04T10:00:00Z"}`)

	return records
}

func TestTheRecordsOfAnEarlierReleaseAreMovedIntoTheDatabase(t *testing.T) {
	t.Parallel()

	for _, engine := range dbtest.Engines(t) {
		t.Run(string(engine.Name), func(t *testing.T) {
			t.Parallel()

			records := anEarlierCatalogue(t)
			grant := engine.Grant(t)
			held, files := over(t, grant, records, &hostfake.Files{})

			assertTheEarlierCatalogue(t, held)

			// The next start reads the database alone: the records are not
			// needed any more, and they are not moved a second time.
			again, _ := over(t, grant, nil, files)
			assertTheEarlierCatalogue(t, again)

			// An id is never given out twice: the counter of the earlier
			// release goes on.
			if next := aPart(t, again, "Shelf"); next.ID != 2 {
				t.Errorf("the first part added after the move took id %d, and 1 was given out", next.ID)
			}

			// The records stay where they were, for a release before this one.
			if keys, _ := records.List(context.Background(), ""); len(keys) != 5 {
				t.Errorf("the record store holds %v after the move", keys)
			}
		})
	}
}

// assertTheEarlierCatalogue holds a catalogue to what anEarlierCatalogue
// wrote.
func assertTheEarlierCatalogue(t *testing.T, held *catalog.Catalog) {
	t.Helper()

	folders := held.Folders()
	if len(folders) != 1 || folders[0].Name != "Garage" || folders[0].Description != "Hooks" ||
		folders[0].Parts != 1 {
		t.Fatalf("the folders came back as %+v", folders)
	}

	detail, err := held.Detail(1)
	if err != nil {
		t.Fatalf("the part did not move: %v", err)
	}

	if detail.Name != "Bike hook" || detail.Folder != 1 || strings.Join(detail.Tags, ",") != "bike,petg" ||
		detail.Cover != "parts/1/a/hook.png" || detail.ChangedBy != "user:bob" {
		t.Errorf("the part came back as %+v", detail.Part)
	}

	if len(detail.Versions) != 1 || len(detail.Versions[0].Files) != 2 ||
		detail.Versions[0].Files[0].SHA256 != "aa" || detail.Versions[0].Note != "first print" {
		t.Errorf("the versions came back as %+v", detail.Versions)
	}

	if len(detail.Comments) != 1 || detail.Comments[0].By != "user:bob" || detail.Comments[0].Edited == nil {
		t.Errorf("the comments came back as %+v", detail.Comments)
	}

	if detail.Picture == nil || detail.Picture.Kind != catalog.PictureImage {
		t.Errorf("the cover the person chose is not drawn: %+v", detail.Picture)
	}
}

// A change made after the move is not undone by a second start that still
// sees the records.
func TestTheMoveRunsOnce(t *testing.T) {
	t.Parallel()

	records := anEarlierCatalogue(t)
	grant := dbtest.Engines(t)[0].Grant(t)
	held, files := over(t, grant, records, &hostfake.Files{})

	if _, err := held.DeletePart(context.Background(), 1); err != nil {
		t.Fatalf("delete the moved part: %v", err)
	}

	again, _ := over(t, grant, records, files)
	if page := again.List(catalog.Query{}); page.Held != 0 {
		t.Errorf("the part deleted after the move came back, and the catalogue holds %d", page.Held)
	}
}

// A store that fails while it is read writes nothing. The next request moves
// the whole catalogue once the store answers again.
func TestAMoveThatCannotReadTheStoreWritesNothing(t *testing.T) {
	t.Parallel()

	records := anEarlierCatalogue(t)

	grant := dbtest.Engines(t)[0].Grant(t)
	held := catalog.New(records, &hostfake.Files{}, place, at)
	held.Attach(dbtest.Open(t, grant), grant.Engine)

	records.Fail = func(call string, _ stateapi.Key) error {
		if call == "list" {
			return stateapi.ErrBusy
		}

		return nil
	}

	if err := held.Ensure(context.Background()); !errors.Is(err, catalog.ErrStore) {
		t.Fatalf("a store that cannot list answered %v", err)
	}

	records.Fail = nil

	if err := held.Ensure(context.Background()); err != nil {
		t.Fatalf("the store answers again, and the move answered %v", err)
	}

	assertTheEarlierCatalogue(t, held)
}

// A server that declares no record backend serves no state method. There is
// nothing to move, and the catalogue starts empty.
func TestAServerWithNoRecordStoreHasNothingToMove(t *testing.T) {
	t.Parallel()

	records := anEarlierCatalogue(t)
	records.Fail = func(string, stateapi.Key) error {
		return &wirerpc.Error{Code: wirerpc.CodeMethodNotFound, Message: "this side serves no method state.list"}
	}

	grant := dbtest.Engines(t)[0].Grant(t)
	held, files := over(t, grant, records, &hostfake.Files{})

	if page := held.List(catalog.Query{}); page.Held != 0 {
		t.Errorf("a server with no record store moved %d parts", page.Held)
	}

	// Once the move is recorded, a record store that shows up later is not
	// read into a catalogue somebody has started using.
	aPart(t, held, "Shelf")

	records.Fail = nil

	again, _ := over(t, grant, records, files)
	if page := again.List(catalog.Query{}); page.Held != 1 || page.Parts[0].Name != "Shelf" {
		t.Errorf("the catalogue reads %+v after the record store appeared", page)
	}
}

func TestALoopInTheRecordsIsMovedUntangled(t *testing.T) {
	t.Parallel()

	records := &hostfake.Records{}

	// Two folders inside each other, and a third under one of them. A release
	// before the checks were whole could write that.
	seed(t, records, "counter", `{"v":1,"folder":4,"part":1,"comment":1}`)
	seed(t, records, "folder/0000000001", `{"v":1,"parent":2,"name":"X"}`)
	seed(t, records, "folder/0000000002", `{"v":1,"parent":1,"name":"Y"}`)
	seed(t, records, "folder/0000000003", `{"v":1,"parent":1,"name":"Z"}`)

	grant := dbtest.Engines(t)[0].Grant(t)
	held, files := over(t, grant, records, &hostfake.Files{})
	ctx := context.Background()

	assertNoLoop(t, held)

	// The correction is in the database, not only in the copy.
	again, _ := over(t, grant, nil, files)
	assertNoLoop(t, again)

	// Every path that walks the tree, on the folders that were in the loop.
	if _, err := again.ChangeFolder(ctx, 3, catalog.FolderDraft{Parent: new(2)}, "user:alice"); err != nil {
		t.Errorf("moving a folder under the old loop answered %v", err)
	}

	if _, err := again.AddFolder(ctx, catalog.FolderDraft{Name: new("W"), Parent: new(1)}, "user:alice"); err != nil {
		t.Errorf("adding a folder under the old loop answered %v", err)
	}
}

func TestRecordsWrittenBeforeTheVersionMarkCountFromTheirVersions(t *testing.T) {
	t.Parallel()

	records := &hostfake.Records{}

	seed(t, records, "counter", `{"v":1,"folder":1,"part":2,"comment":1}`)
	seed(t, records, "part/0000000001", `{"v":1,"name":"Bracket"}`)
	seed(t, records, "version/0000000001/00001", `{"v":1,"files":[]}`)
	seed(t, records, "version/0000000001/00002", `{"v":1,"files":[]}`)

	grant := dbtest.Engines(t)[0].Grant(t)
	held, files := over(t, grant, records, &hostfake.Files{})

	if next := aVersion(t, held, files, 1, "three.stl"); next.Number != 3 {
		t.Errorf("the next version of a part holding 1 and 2 is number %d", next.Number)
	}
}

func TestOneUnreadableRecordDoesNotStopTheMove(t *testing.T) {
	t.Parallel()

	records := &hostfake.Records{}

	seed(t, records, "counter", `{"v":1,"folder":1,"part":4,"comment":1}`)
	seed(t, records, "part/0000000001", `{"v":1,"name":"Bracket"}`)
	seed(t, records, "part/0000000002", `not json`)
	// A later release wrote this one, with a format of its own.
	seed(t, records, "part/0000000003", `{"v":2,"name":"Hinge"}`)
	seed(t, records, "version/0000000003/00001", `{"v":2,"files":[]}`)
	// The version of a part whose delete failed half way, in a release that
	// deleted a part before its versions.
	seed(t, records, "version/0000000009/00001", `{"v":1,"files":[]}`)
	seed(t, records, "comment/0000000009/0000000001", `{"v":1,"body":"x","by":"user:bob"}`)

	grant := dbtest.Engines(t)[0].Grant(t)
	held, _ := over(t, grant, records, &hostfake.Files{})

	if page := held.List(catalog.Query{}); page.Held != 1 || page.Parts[0].Name != "Bracket" {
		t.Errorf("the catalogue serves %+v, and one part of three is readable", page)
	}

	// A part the move skipped keeps its id: the next part does not take it.
	if next := aPart(t, held, "Shelf"); next.ID != 4 {
		t.Errorf("the first part added after the move took id %d", next.ID)
	}
}

func TestAnUnreadableCounterStopsTheMove(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]string{
		"not json":       `not json`,
		"a later format": `{"v":2,"folder":1,"part":1,"comment":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			records := &hostfake.Records{}
			seed(t, records, "counter", body)

			grant := dbtest.Engines(t)[0].Grant(t)
			held := catalog.New(records, &hostfake.Files{}, place, at)
			held.Attach(dbtest.Open(t, grant), grant.Engine)

			if err := held.Ensure(context.Background()); !errors.Is(err, catalog.ErrStore) {
				t.Errorf("a counter that is %s answered %v", name, err)
			}
		})
	}
}
