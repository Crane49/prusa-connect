package simulator

import "testing"

// FILE_INFO names the node by its 8.3 path and each child by its short name
// beside its long one. The simulator answers a question put in either spelling,
// as FAT does.
func TestFileInfoSpellsNamesTheWayTheFirmwareDoes(t *testing.T) {
	t.Parallel()

	printer := treePrinter(t,
		File{Path: "/usb/Summer prints", Folder: true},
		File{Path: "/usb/Summer prints/Tangle cube.bgcode", Size: 10},
		File{Path: "/usb/Summer prints/PART.GCO", Size: 10},
	)

	folder, found := printer.fileInfoData("/usb/SUMMER~1")
	if !found {
		t.Fatal("the folder was not found by its short name")
	}

	if folder["path"] != "/usb/SUMMER~1" || folder["display_name"] != "Summer prints" {
		t.Errorf("the folder reads as %v and %v", folder["path"], folder["display_name"])
	}

	children, _ := folder["children"].([]any)
	names := map[string]string{}

	for _, child := range children {
		node, _ := child.(map[string]any)
		if _, carries := node["path"]; carries {
			t.Errorf("a child carries a path, and the firmware sends none: %v", node)
		}

		long, _ := node["display_name"].(string)
		short, _ := node["name"].(string)
		names[long] = short
	}

	if names["Tangle cube.bgcode"] != "TANGLE~1.BGC" || names["PART.GCO"] != "PART.GCO" {
		t.Errorf("the children are named %v", names)
	}

	file, found := printer.fileInfoData("/usb/Summer prints/TANGLE~1.BGC")
	if !found {
		t.Fatal("the file was not found by a mixed spelling")
	}

	if file["path"] != "/usb/SUMMER~1/TANGLE~1.BGC" {
		t.Errorf("the file reads as %v, want its 8.3 path", file["path"])
	}
}

// A print started by the file's short name is the same print as one started by
// its long name, so the job keeps the long-name path.
func TestAPrintStartedByTheShortNameKeepsTheLongPath(t *testing.T) {
	t.Parallel()

	printer := treePrinter(t, File{Path: "/usb/Tangle cube.bgcode", Size: 10})

	path, found := printer.printable("/usb/TANGLE~1.BGC")
	if !found || path != "/usb/Tangle cube.bgcode" {
		t.Errorf("the short name resolves to %q, %v", path, found)
	}
}

// A name that already fits 8.3 is its own short name, so it takes no numbered
// slot a long name could collide with.
func TestAnEightThreeNameIsItsOwnShortName(t *testing.T) {
	t.Parallel()

	for long, short := range map[string]string{
		"part.gco":         "PART.GCO",
		"README":           "README",
		"part-long.bgcode": "PART-L~1.BGC",
		"my part.gco":      "MYPART~1.GCO",
	} {
		if got := shortSpelling(long); got != short {
			t.Errorf("%q is spelt %q, want %q", long, got, short)
		}
	}
}

// A transfer is refused for a name the storage holds in either spelling and in
// any case, since FAT finds the same entry by each of them.
func TestTheStorageHoldsAFileByEitherSpelling(t *testing.T) {
	t.Parallel()

	printer := treePrinter(t, File{Path: "/usb/Tangle cube.bgcode", Size: 10})

	printer.mu.Lock()
	defer printer.mu.Unlock()

	for _, path := range []string{"/usb/Tangle cube.bgcode", "/usb/tangle CUBE.bgcode", "/usb/TANGLE~1.BGC"} {
		if !printer.holdsLocked(path) {
			t.Errorf("the storage does not hold %q", path)
		}
	}

	if printer.holdsLocked("/usb/other.bgcode") {
		t.Error("the storage holds a name nothing was written under")
	}
}
