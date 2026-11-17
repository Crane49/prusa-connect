package simulator

import (
	"testing"
)

// The 8.3 spelling this simulator gives a long name, and the collision it
// produces. It is a model of a measured board and not a transcription of the
// firmware: what it has to get right is that bug009-probe.gcode and
// bug009-control.gcode are one entry (BUG-014).
func TestTheShortNameOfALongName(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		long  string
		short string
	}{
		"the name BUG-014 was found with": {"bug009-probe.gcode", "BUG009~1.GCO"},
		"the name that collides with it":  {"bug009-control.gcode", "BUG009~1.GCO"},
		"the control name of that run":    {"zzz-clean-name.gcode", "ZZZ-CL~1.GCO"},
		"a name already inside 8.3":       {"part.gco", "PART~1.GCO"},
		"a name with no extension":        {"readme", "README~1"},
		"a name with a second dot":        {"a.b.bgcode", "AB~1.BGC"},
		"a name with a space in it":       {"my part.gcode", "MYPART~1.GCO"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := shortName(tc.long); got != tc.short {
				t.Errorf("%q shortens to %q, want %q", tc.long, got, tc.short)
			}
		})
	}
}

// A file whose short name a sibling already holds is written nowhere, and that
// is the whole of what the printer in BUG-014 did: it reported the transfer
// finished and listed only the first of the two names.
func TestAFileWhoseShortNameIsTakenIsPlantedNowhere(t *testing.T) {
	t.Parallel()

	printer := treePrinter(t, File{Path: "/usb/bug009-probe.gcode", Size: 8490})

	printer.mu.Lock()
	printer.plant(File{Path: "/usb/bug009-control.gcode", Size: 8490})
	printer.mu.Unlock()

	if _, found := printer.fileNode("/usb/bug009-control.gcode"); found {
		t.Error("the second of two names that shorten alike is on the storage")
	}

	if _, found := printer.fileNode("/usb/bug009-probe.gcode"); !found {
		t.Error("the first of the two names left the storage")
	}

	// And a name of its own lands, so the model refuses a collision rather than
	// a second file.
	printer.mu.Lock()
	printer.plant(File{Path: "/usb/zzz-clean-name.gcode", Size: 8490})
	printer.mu.Unlock()

	if _, found := printer.fileNode("/usb/zzz-clean-name.gcode"); !found {
		t.Error("a file whose short name nothing holds was not written")
	}

	// A directory of its own is a directory of its own: the collision is
	// between siblings and never across two folders.
	printer.mu.Lock()
	printer.plant(File{Path: "/usb/examples/bug009-control.gcode", Size: 8490})
	printer.mu.Unlock()

	if _, found := printer.fileNode("/usb/examples/bug009-control.gcode"); !found {
		t.Error("a name taken in one folder refused the same name in another")
	}
}

// A collision is not a silent no-op. The bug file records two things beside the
// missing file: the transfer reported itself finished, and the survivor of the
// pair became undeletable, answering "File is busy" over twenty seconds and
// three retries (BUG-014, observed). A simulator that dropped the file and said
// nothing would hide the half of the bug a caller meets next.
func TestACollidingLandingLeavesTheSurvivorUndeletable(t *testing.T) {
	t.Parallel()

	printer := treePrinter(t, File{Path: "/usb/bug009-probe.gcode", Size: 8490})

	printer.mu.Lock()
	landed := printer.plant(File{Path: "/usb/bug009-control.gcode", Size: 8490})
	printer.mu.Unlock()

	if landed {
		t.Error("the second of two names that shorten alike reported a landing")
	}

	if printer.removeEntry("/usb/bug009-probe.gcode", false) {
		t.Error("the survivor of the collision was deleted, and the observed board would not let go of it")
	}

	// The listing still reports it as an ordinary file. The board's refusal is a
	// lock in its filesystem and not the read_only flag, which it raises for a
	// payload still inside a staging directory and for nothing else.
	node, found := printer.fileNode("/usb/bug009-probe.gcode")
	if !found {
		t.Fatal("the survivor left the storage")
	}

	if node.ReadOnly {
		t.Error("the survivor is listed read-only, which is the firmware's word for a staging directory")
	}

	// A file whose short name nothing holds lands and deletes as it always did.
	printer.mu.Lock()
	clean := printer.plant(File{Path: "/usb/zzz-clean-name.gcode", Size: 8490})
	printer.mu.Unlock()

	if !clean {
		t.Error("a file with a short name of its own reported no landing")
	}

	if !printer.removeEntry("/usb/zzz-clean-name.gcode", false) {
		t.Error("a file no collision touched is undeletable")
	}
}
