package xbuddy

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// A printer that cannot be asked what is in a directory has still taken the
// file, so its transfer finishes. What the driver could not establish rides out
// on the last report as a warning, because a host that showed this as a failure
// would be reporting a fault of the readback and not of the transfer.
func TestAPrinterThatCannotBeAskedForAListingFinishesWithAWarning(t *testing.T) {
	t.Parallel()

	session := &Session{}
	pending := newTestTransfer("usb/part.bgcode")

	var reported connectorapi.Progress

	pending.progress = func(report connectorapi.Progress) { reported = report }

	if err := session.confirmLanding(t.Context(), pending); err != nil {
		t.Fatalf("a transfer this driver cannot confirm is reported as failed: %v", err)
	}

	pending.report(pending.total)

	if reported.Warning == "" {
		t.Error("the host was told nothing about a transfer that was never confirmed")
	}

	t.Logf("observed: the last report carries the warning %q", reported.Warning)
}

// The destination a readback addresses. A path with no directory above it names
// no storage, and a readback of the root would confirm a file against a listing
// of the storages themselves.
func TestTheDirectoryAReadbackAsks(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		path      string
		directory string
		file      string
		addresses bool
	}{
		"a file on a storage":       {"usb/part.bgcode", "usb", "part.bgcode", true},
		"a file in a folder":        {"usb/models/part.gcode", "usb/models", "part.gcode", true},
		"the firmware's spelling":   {"/usb/part.bgcode", "usb", "part.bgcode", true},
		"a name with no storage":    {"part.bgcode", "", "", false},
		"a path that ends in a /":   {"usb/", "", "", false},
		"a path that is only a /":   {"/", "", "", false},
		"a path with nothing in it": {"", "", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			directory, file, addresses := splitDestination(tc.path)
			if addresses != tc.addresses {
				t.Fatalf("%q addresses a directory: %v, want %v", tc.path, addresses, tc.addresses)
			}

			if directory != tc.directory || file != tc.file {
				t.Errorf("%q reads back %q in %q, want %q in %q",
					tc.path, file, directory, tc.file, tc.directory)
			}
		})
	}
}

// What one listing says about the name a file was sent to. The match is
// case-insensitive because the storage is FAT, where two spellings of one name
// are one entry, and a folder of that name is not the file.
func TestWhatAListingSaysAboutASentName(t *testing.T) {
	t.Parallel()

	listing := connectorapi.Listing{
		Directory: connectorapi.File{Path: "usb", Folder: true},
		Entries: []connectorapi.File{
			{Path: "usb/BUG009~1.GCO", Name: "BUG009~1.GCO", DisplayName: "bug009-probe.gcode", Size: 8490},
			{Path: "usb/models", Name: "models", DisplayName: "models", Folder: true},
			{Path: "usb/short.gcode", Name: "SHORT.GCO", Size: 12},
			{Path: "usb/BUG009~2.GCO", Name: "BUG009~2.GCO", DisplayName: "landing.gcode",
				Size: 8490, ReadOnly: true},
		},
	}

	for name, tc := range map[string]struct {
		sent string
		want string
	}{
		"the long name it was sent under": {"bug009-probe.gcode", "8490"},
		"the same name in another case":   {"BUG009-PROBE.GCODE", "8490"},
		"a name the board shortened":      {"short.gcode", "12"},
		"a name no entry holds":           {"bug009-control.gcode", absentEntry},
		"a name a folder holds":           {"models", folderEntry},
		// A read-only entry at the right length is the payload inside a staging
		// directory the firmware renders as a file.
		"a name the board lists read-only": {"landing.gcode", stagingEntry},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := landingOf(listing, tc.sent).describe(); got != tc.want {
				t.Errorf("the readback of %q found %q, want %q", tc.sent, got, tc.want)
			}
		})
	}
}

// countingReader answers a readback with one fixed listing after a delay, and
// counts the readings taken. It is the printer a readback has to survive: one
// that answers, slowly, and never holds the name.
type countingReader struct {
	listing connectorapi.Listing
	delay   time.Duration
	// slow is the one path delay applies to, and every path when empty.
	slow   string
	err    error
	before func()
	reads  int
}

func (r *countingReader) read(ctx context.Context, path string) (connectorapi.Listing, error) {
	r.reads++

	if r.before != nil {
		r.before()
	}

	if r.delay > 0 && (r.slow == "" || r.slow == path) {
		select {
		case <-ctx.Done():
			return connectorapi.Listing{}, ctx.Err()
		case <-time.After(r.delay):
		}
	}

	if r.err != nil {
		return connectorapi.Listing{}, r.err
	}

	return r.listing, nil
}

// faultOf is the registered failure an error carries, and it fails the case for
// an error that carries none: a caller branches on the code.
func faultOf(t *testing.T, err error) *fault.Fault {
	t.Helper()

	var refusal *fault.Fault
	if !errors.As(err, &refusal) {
		t.Fatalf("the failure is %v, and a caller reads a code", err)
	}

	return refusal
}

func codeOfFault(t *testing.T, err error) string {
	t.Helper()

	return faultOf(t, err).Code().String()
}

// emptyUSB is a directory that holds none of the names these cases send to.
func emptyUSB() connectorapi.Listing {
	return connectorapi.Listing{Directory: connectorapi.File{Path: "usb", Folder: true}}
}

// A budget that ends before the printer has answered as often as a file is
// called absent establishes nothing, so the transfer keeps the printer's own
// word for it and carries a warning.
//
// The readback takes three listings before it calls a file absent. One slow
// listing is one reading, and failing a transfer on it would report
// TRANSFER_NOT_ON_STORAGE for a board that was still writing the file.
func TestAListingThatOnlyAnswersAsTheBudgetEndsLeavesAWarning(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		printer := &countingReader{listing: emptyUSB(), delay: landingBudget - landingGap/2, slow: "usb"}
		session := &Session{}
		pending := newTestTransfer("usb/part.bgcode")
		pending.total = 8490

		var reported connectorapi.Progress

		pending.progress = func(report connectorapi.Progress) { reported = report }

		err := session.judgeLanding(t.Context(), pending, "usb", "part.bgcode", printer.read)
		if err != nil {
			t.Fatalf("one slow listing failed the transfer: %v", err)
		}

		pending.report(pending.total)

		if reported.Warning == "" {
			t.Error("the host was told nothing about a transfer the readback could not establish")
		}

		if printer.reads != 2 {
			t.Errorf(
				"the readback took %d listings, want 2 inside the budget, the file ask and one folder listing",
				printer.reads,
			)
		}

		t.Logf("observed: %d reading inside the budget, warning %q", printer.reads, reported.Warning)
	})
}

// A printer that answers promptly and never holds the name is the conclusive
// reading, and that is the one that fails the transfer. BUG-014's own case.
func TestAPromptListingThatNeverHoldsTheNameFailsTheTransfer(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		printer := &countingReader{listing: emptyUSB()}
		session := &Session{}
		pending := newTestTransfer("usb/part.bgcode")
		pending.total = 8490

		err := session.judgeLanding(t.Context(), pending, "usb", "part.bgcode", printer.read)
		if err == nil {
			t.Fatal("a file no listing ever held was reported as landed")
		}

		if got, want := codeOfFault(t, err), connectorapi.TransferNotOnStorage.Code().String(); got != want {
			t.Errorf("the failure carries %s, want %s", got, want)
		}

		if printer.reads != landingAttempts+1 {
			t.Errorf(
				"the readback took %d listings, want the file ask and %d folder listings",
				printer.reads,
				landingAttempts,
			)
		}
	})
}

// A stop pressed inside the readback window ends the listing, and that is the
// caller's doing. Answering it done with a warning records a printer that was
// never asked as a printer that could not be asked, and the row settles as a
// success nobody wanted.
func TestAStopInsideTheReadbackIsTheCallersAndNotThePrintersDoing(t *testing.T) {
	t.Parallel()

	ctx, stop := context.WithCancel(t.Context())
	printer := &countingReader{listing: emptyUSB(), before: stop, err: context.Canceled}
	session := &Session{}
	pending := newTestTransfer("usb/part.bgcode")
	pending.total = 8490

	var reported connectorapi.Progress

	pending.progress = func(report connectorapi.Progress) { reported = report }

	err := session.judgeLanding(ctx, pending, "usb", "part.bgcode", printer.read)
	if err == nil {
		t.Fatal("a transfer the caller stopped was reported as finished")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("the failure is %v, and the caller reads a cancellation", err)
	}

	pending.report(pending.total)

	if reported.Warning != "" {
		t.Errorf("a transfer the caller stopped blames the printer: %q", reported.Warning)
	}

	t.Logf("observed: %v", err)
}

// A name the firmware lists read-only at the sent length is the payload inside
// its staging directory. The firmware moves it into place once nothing holds it
// open, and a printer with a screen holds every file it receives open in the
// print preview. The file is there, so the transfer finishes, and the warning
// says why the file cannot be deleted or replaced yet.
func TestAStagingEntryFinishesWithAWarning(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		printer := &countingReader{listing: connectorapi.Listing{
			Directory: connectorapi.File{Path: "usb", Folder: true},
			Entries: []connectorapi.File{{
				Path: "usb/BUG009~2.GCO", Name: "BUG009~2.GCO",
				DisplayName: "bug009-control.gcode", Size: 8490, ReadOnly: true,
			}},
		}}
		session := &Session{}
		pending := newTestTransfer("usb/bug009-control.gcode")
		pending.total = 8490

		var reported connectorapi.Progress

		pending.progress = func(report connectorapi.Progress) { reported = report }

		err := session.judgeLanding(t.Context(), pending, "usb", "bug009-control.gcode", printer.read)
		if err != nil {
			t.Fatalf("a file the printer has not moved into place yet failed the transfer: %v", err)
		}

		pending.report(pending.total)

		if reported.Warning != stagedWarning {
			t.Errorf("the last report warns %q, want %q", reported.Warning, stagedWarning)
		}

		if printer.reads != landingAttempts+1 {
			t.Errorf(
				"the readback took %d listings, want the file ask and %d folder listings",
				printer.reads,
				landingAttempts,
			)
		}
	})
}

// A read-only entry of another length is not the whole file, whatever the
// printer said about the transfer, so it fails as one that did not land.
func TestAStagingEntryOfAnotherLengthFailsTheTransfer(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		printer := &countingReader{listing: connectorapi.Listing{
			Directory: connectorapi.File{Path: "usb", Folder: true},
			Entries: []connectorapi.File{{
				Path: "usb/BUG009~2.GCO", Name: "BUG009~2.GCO",
				DisplayName: "bug009-control.gcode", Size: 4096, ReadOnly: true,
			}},
		}}
		session := &Session{}
		pending := newTestTransfer("usb/bug009-control.gcode")
		pending.total = 8490

		err := session.judgeLanding(t.Context(), pending, "usb", "bug009-control.gcode", printer.read)
		if err == nil {
			t.Fatal("a read-only entry shorter than the file was reported as landed")
		}

		found, carried := faultOf(t, err).Params().Get("found")
		if !carried {
			t.Fatal("the failure carries no found")
		}

		if got := found.String(); got != stagingEntry {
			t.Errorf("the failure reports found = %q, want %q", got, stagingEntry)
		}
	})
}

// A budget that ends on a staged reading still knows why the file is locked, so
// the transfer carries the staged warning and not the one for a printer that
// could not be asked.
func TestABudgetThatEndsOnAStagingEntryCarriesTheStagedWarning(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		printer := &countingReader{
			listing: connectorapi.Listing{
				Directory: connectorapi.File{Path: "usb", Folder: true},
				Entries: []connectorapi.File{{
					Path: "usb/part.bgcode", Name: "PART.BGC", DisplayName: "part.bgcode",
					Size: 8490, ReadOnly: true,
				}},
			},
			delay: landingBudget - landingGap/2,
			slow:  "usb",
		}
		session := &Session{}
		pending := newTestTransfer("usb/part.bgcode")
		pending.total = 8490

		var reported connectorapi.Progress

		pending.progress = func(report connectorapi.Progress) { reported = report }

		if err := session.judgeLanding(t.Context(), pending, "usb", "part.bgcode", printer.read); err != nil {
			t.Fatalf("a staged file failed the transfer: %v", err)
		}

		pending.report(pending.total)

		if reported.Warning != stagedWarning {
			t.Errorf("the last report warns %q, want %q", reported.Warning, stagedWarning)
		}

		if printer.reads != 2 {
			t.Errorf(
				"the readback took %d listings, want 2 inside the budget, the file ask and one folder listing",
				printer.reads,
			)
		}
	})
}

// pathReader is a printer that answers per asked path and records what it was
// asked, in order.
type pathReader struct {
	answers map[string]connectorapi.Listing
	errs    map[string]error
	asked   []string
}

func (r *pathReader) read(_ context.Context, path string) (connectorapi.Listing, error) {
	r.asked = append(r.asked, path)

	if err := r.errs[path]; err != nil {
		return connectorapi.Listing{}, err
	}

	return r.answers[path], nil
}

// The printer's answer about the file itself is fast where a folder readback is
// not, so a file it names as sent, writable and of the sent length is landed
// without listing the folder.
func TestAFileThePrinterNamesAsSentLandsWithoutAFolderReadback(t *testing.T) {
	t.Parallel()

	printer := &pathReader{answers: map[string]connectorapi.Listing{
		"usb/a.gcode": {Directory: connectorapi.File{Path: "usb/a.gcode", DisplayName: "a.gcode", Size: 42}},
	}}
	pending := newTestTransfer("usb/a.gcode")
	pending.total = 42

	if err := (&Session{}).judgeLanding(t.Context(), pending, "usb", "a.gcode", printer.read); err != nil {
		t.Fatalf("a file the printer named as sent failed the transfer: %v", err)
	}

	if want := []string{"usb/a.gcode"}; !slices.Equal(printer.asked, want) {
		t.Errorf("the printer was asked %v, want %v", printer.asked, want)
	}
}

// rooted() rewrites the answer's Path to the path that was asked, so a Path
// match proves nothing. An answer whose own name is another one's is not this
// file, and the folder decides.
func TestAFileLevelAnswerNamingAnotherLongNameFallsBackToTheFolder(t *testing.T) {
	t.Parallel()

	printer := &pathReader{answers: map[string]connectorapi.Listing{
		"usb/b-long.gcode": {Directory: connectorapi.File{
			Path: "usb/b-long.gcode", DisplayName: "a-long.gcode", Size: 42,
		}},
		"usb": emptyUSB(),
	}}
	pending := newTestTransfer("usb/b-long.gcode")
	pending.total = 42

	err := (&Session{}).judgeLanding(t.Context(), pending, "usb", "b-long.gcode", printer.read)
	if !fault.HasCode(err, connectorapi.TransferNotOnStorage.Code()) {
		t.Fatalf("a file the folder does not hold landed: %v", err)
	}

	if len(printer.asked) < 2 || printer.asked[0] != "usb/b-long.gcode" || printer.asked[1] != "usb" {
		t.Errorf("the printer was asked %v, want the file and then the folder", printer.asked)
	}
}

// A read-only answer is a payload still in staging, which only the folder
// readback turns into the staged warning.
func TestAStagedFileFallsBackToTheFolderReadback(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		printer := &pathReader{answers: map[string]connectorapi.Listing{
			"usb/a.gcode": {Directory: connectorapi.File{
				Path: "usb/a.gcode", DisplayName: "a.gcode", Size: 42, ReadOnly: true,
			}},
			"usb": {
				Directory: connectorapi.File{Path: "usb", Folder: true},
				Entries:   []connectorapi.File{{Path: "usb/a.gcode", DisplayName: "a.gcode", Size: 42, ReadOnly: true}},
			},
		}}
		pending := newTestTransfer("usb/a.gcode")
		pending.total = 42

		var reported connectorapi.Progress

		pending.progress = func(report connectorapi.Progress) { reported = report }

		if err := (&Session{}).judgeLanding(t.Context(), pending, "usb", "a.gcode", printer.read); err != nil {
			t.Fatalf("a staged file failed the transfer: %v", err)
		}

		pending.report(pending.total)

		if reported.Warning != stagedWarning {
			t.Errorf("the last report warns %q, want %q", reported.Warning, stagedWarning)
		}
	})
}

func TestAFileAskThatFailsFallsBackToTheFolder(t *testing.T) {
	t.Parallel()

	printer := &pathReader{
		errs: map[string]error{"usb/a.gcode": errors.New("no answer")},
		answers: map[string]connectorapi.Listing{
			"usb": {
				Directory: connectorapi.File{Path: "usb", Folder: true},
				Entries:   []connectorapi.File{{Path: "usb/a.gcode", DisplayName: "a.gcode", Size: 42}},
			},
		},
	}
	pending := newTestTransfer("usb/a.gcode")
	pending.total = 42

	if err := (&Session{}).judgeLanding(t.Context(), pending, "usb", "a.gcode", printer.read); err != nil {
		t.Fatalf("a file the folder holds failed the transfer: %v", err)
	}

	if want := []string{"usb/a.gcode", "usb"}; !slices.Equal(printer.asked, want) {
		t.Errorf("the printer was asked %v, want %v", printer.asked, want)
	}
}

// A file ask that runs out of its budget, the MK4S case behind a 15 s listing
// on the wire, finishes with a warning and no folder readback: the folder
// would not answer in time either, and its listing would hold the printer.
func TestAFileAskThatRunsOutOfTimeReadsNoFolder(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var asked []string

		read := func(ctx context.Context, path string) (connectorapi.Listing, error) {
			asked = append(asked, path)
			<-ctx.Done()

			return connectorapi.Listing{}, ctx.Err()
		}
		pending := newTestTransfer("usb/a.gcode")
		pending.total = 42

		var reported connectorapi.Progress

		pending.progress = func(report connectorapi.Progress) { reported = report }

		if err := (&Session{}).judgeLanding(t.Context(), pending, "usb", "a.gcode", read); err != nil {
			t.Fatalf("an unanswered file ask failed the transfer: %v", err)
		}

		pending.report(pending.total)

		if want := []string{"usb/a.gcode"}; !slices.Equal(asked, want) {
			t.Errorf("the printer was asked %v, want %v", asked, want)
		}

		if reported.Warning == "" {
			t.Error("an unconfirmed landing carries no warning")
		}
	})
}
