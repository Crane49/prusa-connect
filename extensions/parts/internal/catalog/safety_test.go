package catalog_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/prusa3d/parts/internal/catalog"
	"github.com/prusa3d/parts/internal/dbtest"
	"github.com/prusa3d/parts/internal/hostfake"
)

// The tests in this file hold the catalogue to what one process must keep
// true while several requests write at once.

// together runs both changes at the same moment, many times, which is what two
// people pressing a button at once do.
func together(rounds int, first, second func()) {
	for range rounds {
		var (
			start sync.WaitGroup
			done  sync.WaitGroup
		)

		start.Add(1)
		done.Add(2)

		for _, change := range []func(){first, second} {
			go func() {
				defer done.Done()

				start.Wait()
				change()
			}()
		}

		start.Done()
		done.Wait()
	}
}

func TestTwoOppositeMovesCannotBothLand(t *testing.T) {
	t.Parallel()

	grant := dbtest.Engines(t)[0].Grant(t)
	held, _ := over(t, grant, nil, &hostfake.Files{})
	ctx := context.Background()

	x, _ := held.AddFolder(ctx, catalog.FolderDraft{Name: new("X")}, "user:alice")
	y, _ := held.AddFolder(ctx, catalog.FolderDraft{Name: new("Y")}, "user:alice")

	var first, second error

	// X is moved into Y while somebody moves Y into X.
	together(1, func() {
		_, first = held.ChangeFolder(ctx, x.ID, catalog.FolderDraft{Parent: new(y.ID)}, "user:alice")
	}, func() {
		_, second = held.ChangeFolder(ctx, y.ID, catalog.FolderDraft{Parent: new(x.ID)}, "user:bob")
	})

	if first == nil && second == nil {
		t.Fatalf("both moves landed, and X and Y now sit inside each other")
	}

	assertNoLoop(t, held)

	// The tree is read again from the database, which is what a restart does.
	again, _ := over(t, grant, nil, &hostfake.Files{})
	assertNoLoop(t, again)
}

// assertNoLoop walks every folder up to the top of the tree.
func assertNoLoop(t *testing.T, held *catalog.Catalog) {
	t.Helper()

	parents := map[int]int{}
	for _, folder := range held.Folders() {
		parents[folder.ID] = folder.Parent
	}

	for id := range parents {
		at := id

		for steps := 0; at != catalog.Root; steps++ {
			if steps > len(parents) {
				t.Fatalf("folder %d sits in a loop: %v", id, parents)
			}

			at = parents[at]
		}
	}
}

func TestAFolderDeletedWhileAPartGoesIntoItLeavesNoOrphan(t *testing.T) {
	t.Parallel()

	held, _ := fresh(t)
	ctx := context.Background()

	folder, _ := held.AddFolder(ctx, catalog.FolderDraft{Name: new("Clips")}, "user:alice")

	var deleted, added error

	together(1, func() {
		deleted = held.DeleteFolder(ctx, folder.ID)
	}, func() {
		_, added = held.AddPart(ctx, catalog.PartDraft{Name: new("Clip"), Folder: new(folder.ID)}, "user:bob")
	})

	folders := map[int]bool{catalog.Root: true}
	for _, kept := range held.Folders() {
		folders[kept.ID] = true
	}

	for _, part := range held.List(catalog.Query{}).Parts {
		if !folders[part.Folder] {
			t.Errorf("part %d is in folder %d, which is gone (the delete answered %v)", part.ID, part.Folder, deleted)
		}
	}

	if deleted == nil && !errors.Is(added, catalog.ErrFolderNotFound) {
		t.Errorf("the folder was deleted and the part going into it answered %v", added)
	}
}

func TestTwoFoldersAddedAtOnceDoNotShareAName(t *testing.T) {
	t.Parallel()

	held, _ := fresh(t)
	ctx := context.Background()

	var first, second error

	together(1, func() {
		_, first = held.AddFolder(ctx, catalog.FolderDraft{Name: new("jigs")}, "user:alice")
	}, func() {
		_, second = held.AddFolder(ctx, catalog.FolderDraft{Name: new("Jigs")}, "user:bob")
	})

	failed := 0

	for _, err := range []error{first, second} {
		if errors.Is(err, catalog.ErrNameTaken) {
			failed++
		}
	}

	if failed != 1 || len(held.Folders()) != 1 {
		t.Errorf("two folders named jigs at once answered %v and %v, and the tree holds %d",
			first, second, len(held.Folders()))
	}
}

func TestAVersionNumberIsNeverGivenOutTwice(t *testing.T) {
	t.Parallel()

	grant := dbtest.Engines(t)[0].Grant(t)
	held, files := over(t, grant, nil, &hostfake.Files{})
	ctx := context.Background()
	part := aPart(t, held, "Bracket")

	aVersion(t, held, files, part.ID, "one.stl")
	second := aVersion(t, held, files, part.ID, "two.stl")

	if _, err := held.DeleteVersion(ctx, part.ID, second.Number); err != nil {
		t.Fatalf("delete the newest version: %v", err)
	}

	if third := aVersion(t, held, files, part.ID, "three.stl"); third.Number != 3 {
		t.Errorf("the version added after deleting 2 is number %d, and 2 was given out", third.Number)
	}

	if _, err := held.DeleteVersion(ctx, part.ID, 3); err != nil {
		t.Fatalf("delete the newest version again: %v", err)
	}

	// A restart reads the number back from the database.
	again, _ := over(t, grant, nil, files)
	if fourth := aVersion(t, again, files, part.ID, "four.stl"); fourth.Number != 4 {
		t.Errorf("the version added after a restart is number %d, and 3 was given out", fourth.Number)
	}
}

// Deleting a part takes its versions and comments with it, and a part added
// afterwards does not find them.
func TestDeletingAPartLeavesNothingOfItBehind(t *testing.T) {
	t.Parallel()

	grant := dbtest.Engines(t)[0].Grant(t)
	held, files := over(t, grant, nil, &hostfake.Files{})
	ctx := context.Background()
	part := aPart(t, held, "Bracket")

	aVersion(t, held, files, part.ID, "a.stl")

	if _, err := held.AddComment(ctx, part.ID, "x", "user:bob"); err != nil {
		t.Fatalf("add a comment: %v", err)
	}

	if _, err := held.DeletePart(ctx, part.ID); err != nil {
		t.Fatalf("delete the part: %v", err)
	}

	again, _ := over(t, grant, nil, files)
	if page := again.List(catalog.Query{}); page.Held != 0 {
		t.Errorf("the database holds %d parts after the only one was deleted", page.Held)
	}

	db := dbtest.Open(t, grant)

	for _, table := range []string{"part_version", "version_file", "part_comment", "part_tag"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Errorf("%s holds %d rows after the part was deleted (%v)", table, count, err)
		}
	}
}

func TestDeletingAPartNamesTheFoldersItsFilesAreIn(t *testing.T) {
	t.Parallel()

	held, files := fresh(t)
	ctx := context.Background()

	stays := aPart(t, held, "Stays")
	aVersion(t, held, files, stays.ID, "a.stl")

	if removed, err := held.DeletePart(ctx, stays.ID); err != nil || removed.Dir != "parts/1" ||
		len(removed.Dirs) != 1 {
		t.Errorf("deleting a part kept under one folder answered %+v, %v", removed, err)
	}

	moved := aPart(t, held, "Moved")
	aVersion(t, held, files, moved.ID, "b.stl")

	// The operator moves where new files go. The part's file stays where it
	// was recorded.
	held.Move(catalog.Place{Storage: "archive", Root: "catalogue"})

	removed, err := held.DeletePart(ctx, moved.ID)
	if err != nil {
		t.Fatalf("delete the part: %v", err)
	}

	want := []catalog.Dir{{Storage: "archive", Dir: "catalogue/2"}, {Storage: "local", Dir: "parts/2"}}
	if removed.Dir != "" || len(removed.Dirs) != 2 || removed.Dirs[0] != want[0] || removed.Dirs[1] != want[1] {
		t.Errorf("deleting a part after the settings moved answered %+v, and its file is in parts/2", removed)
	}
}

func TestDeletingAVersionComparesTheStorageOfAFile(t *testing.T) {
	t.Parallel()

	held, files := fresh(t)
	ctx := context.Background()
	part := aPart(t, held, "Bracket")

	first := aVersion(t, held, files, part.ID, "a.stl")

	// The same path in another storage is another file.
	held.Move(catalog.Place{Storage: "archive", Root: "parts"})
	files.Put("archive", first.Files[0].Path, 5, "")

	if _, err := held.AddVersion(ctx, part.ID, catalog.VersionDraft{
		Files: []catalog.FileDraft{{Path: first.Files[0].Path}},
	}, "user:alice"); err != nil {
		t.Fatalf("add the second version: %v", err)
	}

	removed, err := held.DeleteVersion(ctx, part.ID, first.Number)
	if err != nil {
		t.Fatalf("delete the first version: %v", err)
	}

	if len(removed.Files) != 1 || removed.Files[0].Storage != "local" {
		t.Errorf("deleting version 1 answered %+v, and its file in local is named by nothing now", removed.Files)
	}
}

func TestADescriptionAtItsLimitFitsItsRecord(t *testing.T) {
	t.Parallel()

	held, _ := fresh(t)

	// Each of these is escaped by a JSON encoder that guards HTML.
	for _, text := range []string{"<", ">", "&"} {
		description := strings.Repeat(text, catalog.MaxDescriptionBytes)

		if _, err := held.AddPart(context.Background(), catalog.PartDraft{
			Name: new("x"), Description: &description,
		}, "user:alice"); err != nil {
			t.Errorf("a description of %d %q answered %v", catalog.MaxDescriptionBytes, text, err)
		}
	}
}

func TestSearchFollowsTheChangesOfAPart(t *testing.T) {
	t.Parallel()

	held, files := fresh(t)
	ctx := context.Background()
	part := aPart(t, held, "Bracket")
	version := aVersion(t, held, files, part.ID, "Wheel-Hook.stl")

	if got := held.List(catalog.Query{Text: "wheel-hook"}).Matched; got != 1 {
		t.Errorf("a file name added with a version matched %d parts", got)
	}

	if _, err := held.ChangePart(
		ctx,
		part.ID,
		catalog.PartDraft{Description: new("Holds a BICYCLE")},
		"user:alice",
	); err != nil {
		t.Fatalf("change the description: %v", err)
	}

	if got := held.List(catalog.Query{Text: "bicycle"}).Matched; got != 1 {
		t.Errorf("a description written after the part matched %d parts", got)
	}

	if _, err := held.DeleteVersion(ctx, part.ID, version.Number); err != nil {
		t.Fatalf("delete the version: %v", err)
	}

	if got := held.List(catalog.Query{Text: "wheel-hook"}).Matched; got != 0 {
		t.Errorf("a file name of a deleted version still matched %d parts", got)
	}
}
