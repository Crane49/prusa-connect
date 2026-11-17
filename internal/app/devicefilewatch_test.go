package app

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// holdSummerPrints holds a root with one long-named folder, and that folder
// with one file, as a Buddy printer lists them: the short name in Name.
func holdSummerPrints(plane *devices, id string) {
	holdListing(plane, id, "usb",
		httpapi.DeviceFile{Path: "usb/Summer prints", Name: "SUMMER~1", DisplayName: "Summer prints", Folder: true})
	holdListing(
		plane,
		id,
		"usb/Summer prints",
		httpapi.DeviceFile{
			Path:        "usb/Summer prints/old part.gcode",
			Name:        "OLDPAR~1.GCO",
			DisplayName: "old part.gcode",
		},
	)
}

// The printer names a file it reports by its short path. The row is kept
// under the long names this server lists, and the change is published.
func TestAFileThePrinterReportsIsHeldUnderItsLongName(t *testing.T) {
	t.Parallel()

	session := newListingSession("SIM-WATCH-ADD")
	plane, id, owner := arriveListing(t, session, session.patches)
	entry, ok := plane.entry(id)
	require.True(t, ok)

	holdSummerPrints(plane, id)
	seen := watchFolders(t, plane, owner)

	plane.printerChangedFile(entry, connectorapi.FileChange{File: connectorapi.File{
		Path: "usb/SUMMER~1/NEWPAR~1.BGC", Name: "NEWPAR~1.BGC", DisplayName: "new part.bgcode", Size: 7,
	}})

	listing, held := plane.listings.get(id, "usb/Summer prints")
	require.True(t, held)
	require.Equal(t,
		[]string{"usb/Summer prints/old part.gcode", "usb/Summer prints/new part.bgcode"}, itemPaths(listing))
	require.Equal(t, int64(7), listing.Items[1].Size)

	seen.requireLanded(t, "usb/Summer prints")
	require.Zero(t, session.lists.Load(), "a reported file made the printer list its folder")
}

// A removal named by the short path takes the row it names, and the listings
// held under it.
func TestARemovalThePrinterReportsTakesItsRow(t *testing.T) {
	t.Parallel()

	session := newListingSession("SIM-WATCH-REMOVE")
	plane, id, _ := arriveListing(t, session, session.patches)
	entry, ok := plane.entry(id)
	require.True(t, ok)

	holdSummerPrints(plane, id)

	plane.printerChangedFile(entry, connectorapi.FileChange{
		File: connectorapi.File{Path: "usb/SUMMER~1/OLDPAR~1.GCO"}, Removed: true,
	})

	listing, _ := plane.listings.get(id, "usb/Summer prints")
	require.Empty(t, listing.Items)

	plane.printerChangedFile(entry, connectorapi.FileChange{
		File: connectorapi.File{Path: "usb/SUMMER~1", Folder: true}, Removed: true,
	})

	root, _ := plane.listings.get(id, "usb")
	require.Empty(t, root.Items)

	_, held := plane.listings.get(id, "usb/Summer prints")
	require.False(t, held, "a removed folder's listing was kept")
}

// A thumbnail read answers with a FILE_INFO of a file already held, by its path
// alone and with the time a second off. It must change nothing, or every
// thumbnail read would publish files.changed.
func TestAFileReportedAsHeldChangesNothing(t *testing.T) {
	t.Parallel()

	session := newListingSession("SIM-WATCH-SAME")
	plane, id, _ := arriveListing(t, session, session.patches)
	entry, ok := plane.entry(id)
	require.True(t, ok)

	listed := time.Unix(1_700_000_000, 0).UTC()
	row := httpapi.DeviceFile{Path: "usb/Long name.gcode", Name: "LONGNA~1.GCO", Size: 5, Modified: &listed}
	holdListing(plane, id, "usb", row)

	plane.printerChangedFile(entry, connectorapi.FileChange{
		File: connectorapi.File{Path: "usb/LONGNA~1.GCO", Size: 5, Modified: listed.Unix() + 1},
	})

	held, ok := plane.listings.get(id, "usb")
	require.True(t, ok)
	require.Equal(t, []httpapi.DeviceFile{row}, held.Items)
}

// A change in a folder nobody holds is not kept: the folder is read when it
// is opened.
func TestAChangeInAFolderNotHeldIsNotKept(t *testing.T) {
	t.Parallel()

	session := newListingSession("SIM-WATCH-COLD")
	plane, id, _ := arriveListing(t, session, session.patches)
	entry, ok := plane.entry(id)
	require.True(t, ok)

	holdListing(plane, id, "usb")

	plane.printerChangedFile(entry, connectorapi.FileChange{
		File: connectorapi.File{Path: "usb/COLD/A.GCO", Name: "A.GCO"},
	})

	_, held := plane.listings.get(id, "usb/COLD")
	require.False(t, held)
	require.Zero(t, session.lists.Load())
}

// Several changes the firmware merged into one rescan read the held folder
// again, in the background, so a person's command goes first.
func TestARescanReadsTheHeldFolderInTheBackground(t *testing.T) {
	t.Parallel()

	session := newListingSession("SIM-WATCH-RESCAN")
	plane, id, _ := arriveListing(t, session, session.patches)
	entry, ok := plane.entry(id)
	require.True(t, ok)

	holdSummerPrints(plane, id)

	plane.printerChangedFile(entry, connectorapi.FileChange{
		File: connectorapi.File{Path: "usb/SUMMER~1", Folder: true}, Rescan: true,
	})

	require.Eventually(t, func() bool { return session.lists.Load() == 1 }, settle, time.Millisecond)
	require.Equal(t, []bool{true}, session.lanes())
}

// A refused delete_file leaves one row of one folder in doubt. Only that folder
// is forgotten, not every folder on the stick.
func TestAFailedFileVerbForgetsOnlyItsFolder(t *testing.T) {
	t.Parallel()

	session := newListingSession("SIM-FILE-VERB-FAILED")
	plane, id, _ := arriveListing(t, session, session.patches)
	entry, ok := plane.entry(id)
	require.True(t, ok)

	holdListing(plane, id, "usb", httpapi.DeviceFile{Path: "usb/x.gcode", Name: "x.gcode"})
	holdListing(plane, id, "usb/sub", httpapi.DeviceFile{Path: "usb/sub/y.gcode", Name: "y.gcode"})

	plane.fileChanged(entry, connectorapi.VerbDeleteFile, "usb/x.gcode", nil, errors.New("refused"))

	_, held := plane.listings.get(id, "usb/sub")
	require.True(t, held, "a failed delete of usb/x.gcode forgot usb/sub")

	_, held = plane.listings.get(id, "usb")
	require.False(t, held, "the folder of the failed delete was kept")
}
