package simulator

import (
	"testing"
)

func treePrinter(t *testing.T, files ...File) *Printer {
	t.Helper()

	printer, err := New(Config{Model: "MK4", BaseURL: "http://sim", Files: files})
	if err != nil {
		t.Fatalf("new printer: %v", err)
	}

	return printer
}

// The default tree is not empty. A simulator whose storage has nothing on it
// certifies a file listing green against a fixture that could never show a
// wrong answer, which is the failure this repository has already shipped three
// times.
func TestTheDefaultTreeHasSomethingOnIt(t *testing.T) {
	t.Parallel()

	printer := treePrinter(t)

	node, found := printer.fileNode("/usb")
	if !found {
		t.Fatal("the default storage has no root node")
	}

	if !node.Folder {
		t.Error("the storage root is not a folder")
	}

	children := printer.fileChildren("/usb")
	if len(children) == 0 {
		t.Fatal("the default storage is empty, so a listing test against it proves nothing")
	}
}

func TestOnlyTheDirectChildrenOfAPathAreListed(t *testing.T) {
	t.Parallel()

	printer := treePrinter(t,
		File{Path: "/usb/models", Folder: true},
		File{Path: "/usb/models/part.bgcode", Size: 4096, Modified: 1750000000},
		File{Path: "/usb/models/sub", Folder: true},
		File{Path: "/usb/models/sub/deep.bgcode", Size: 8192},
		File{Path: "/usb/top.bgcode", Size: 64},
	)

	root := printer.fileChildren("/usb")
	if len(root) != 2 {
		t.Fatalf("the storage root lists %d entries, want models and top.bgcode", len(root))
	}

	models := printer.fileChildren("/usb/models")
	if len(models) != 2 {
		t.Fatalf("/usb/models lists %d entries, want part.bgcode and sub", len(models))
	}

	for _, entry := range models {
		if entry.Path == "/usb/models/sub/deep.bgcode" {
			t.Error("a grandchild was listed as a direct child")
		}
	}
}

func TestAPathNothingHoldsIsNotFound(t *testing.T) {
	t.Parallel()

	printer := treePrinter(t, File{Path: "/usb/a.bgcode", Size: 1})

	if _, found := printer.fileNode("/usb/missing"); found {
		t.Error("a path nothing holds was found")
	}

	if _, found := printer.fileNode("/usb/a.bgcode"); !found {
		t.Error("a path the tree holds was not found")
	}
}

// A folder created over the wire is a folder the next listing reports, and a
// file deleted over the wire is gone from it. Without that, a simulator can
// only ever show one fixed answer.
func TestTheTreeChangesWhenTheWireChangesIt(t *testing.T) {
	t.Parallel()

	printer := treePrinter(t, File{Path: "/usb/a.bgcode", Size: 1})

	if !printer.createFolder("/usb/new") {
		t.Fatal("creating a folder that is not there reported a refusal")
	}

	if printer.createFolder("/usb/new") {
		t.Error("creating a folder that is already there reported success")
	}

	if _, found := printer.fileNode("/usb/new"); !found {
		t.Error("a folder that was created is not in the tree")
	}

	if !printer.removeEntry("/usb/a.bgcode", false) {
		t.Fatal("deleting a file that is there reported a refusal")
	}

	if _, found := printer.fileNode("/usb/a.bgcode"); found {
		t.Error("a file that was deleted is still in the tree")
	}

	if printer.removeEntry("/usb/a.bgcode", false) {
		t.Error("deleting a file that is gone reported success")
	}
}

// A folder delete takes what is inside it, and a file delete refuses a folder.
// A DELETE_FILE that removed a directory would be the wrong command silently
// doing the destructive one.
func TestDeletingAFolderTakesItsContentsAndAFileDeleteRefusesOne(t *testing.T) {
	t.Parallel()

	printer := treePrinter(t,
		File{Path: "/usb/dir", Folder: true},
		File{Path: "/usb/dir/inside.bgcode", Size: 1},
	)

	if printer.removeEntry("/usb/dir", false) {
		t.Error("DELETE_FILE removed a folder")
	}

	if !printer.removeEntry("/usb/dir", true) {
		t.Fatal("DELETE_FOLDER refused a folder that is there")
	}

	if _, found := printer.fileNode("/usb/dir/inside.bgcode"); found {
		t.Error("a file inside a deleted folder survived it")
	}
}
