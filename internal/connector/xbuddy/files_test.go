package xbuddy_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gorilla/websocket"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi/connectorapitest"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// filesOf is the session's filesystem half, which every case here needs.
func filesOf(t *testing.T, session connectorapi.Session) connectorapi.Filesystem {
	t.Helper()

	files, ok := connectorapi.Files(session)
	if !ok {
		t.Fatal("an xBuddy session cannot report what is on the printer")
	}

	return files
}

// A printer says nothing about its storages until it is asked, so the session
// asks when a connection attaches. Without that the host knows one mountpoint,
// the configured default, and cannot list a second stick at all.
func TestAConnectedPrinterIsAskedWhatStoragesItHas(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration, withPrinter(func(cfg *simulator.Config) {
			cfg.Storages = []simulator.Storage{
				{Mountpoint: "/usb", Type: "USB", FreeSpace: 1 << 30},
				{Mountpoint: "/local", Type: "LOCAL", FreeSpace: 1 << 24, ReadOnly: true},
			}
		}))
		defer rig.stop(t)

		rig.connect(t)
		time.Sleep(time.Second)
		synctest.Wait()

		storages, err := filesOf(t, rig.spy.inner).Storages(rig.ctx)
		if err != nil {
			t.Fatalf("Storages: %v", err)
		}

		t.Logf("observed storages: %+v", storages)

		if len(storages) != 2 {
			t.Fatalf("the session reports %d storages, and the printer has 2", len(storages))
		}

		if storages[0].Mountpoint != "/usb" || storages[0].Name != "usb" || storages[0].Type != "USB" {
			t.Errorf("the first storage reads %+v, want /usb named usb of type USB", storages[0])
		}

		if !storages[0].FreeSpaceKnown || storages[0].FreeSpace != 1<<30 {
			t.Errorf("the first storage reports %d bytes free, known=%v, want 1073741824 known",
				storages[0].FreeSpace, storages[0].FreeSpaceKnown)
		}

		if !storages[1].ReadOnly {
			t.Error("the read-only storage does not report itself read-only")
		}
	})
}

// The root has no node on the firmware side, so it is answered from an INFO
// rather than a FILE_INFO.
func TestListingTheRootReportsTheStorages(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration)
		defer rig.stop(t)

		rig.connect(t)
		time.Sleep(time.Second)
		synctest.Wait()

		listing, err := filesOf(t, rig.spy.inner).List(rig.ctx, "")
		if err != nil {
			t.Fatalf("List the root: %v", err)
		}

		t.Logf("observed root listing: %+v", listing)

		if !listing.Directory.Folder {
			t.Error("the root is not reported as a folder")
		}

		if len(listing.Entries) != 1 || listing.Entries[0].Path != "usb" {
			t.Fatalf("the root lists %+v, and the printer has one storage named usb", listing.Entries)
		}

		if !listing.Entries[0].Folder {
			t.Error("a storage is not reported as a folder")
		}
	})
}

// A printer can miss reporting a stick put in or pulled out, so the root asks
// again rather than listing what the printer said when it connected.
// The wake-up fires for an answer that moved the list and not for one that
// repeated it.
func TestListingTheRootAsksThePrinterWhatStoragesItHasNow(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration)
		defer rig.stop(t)

		rig.connect(t)
		time.Sleep(time.Second)
		synctest.Wait()

		report, ok := rig.spy.inner.(capability.StorageReport)
		if !ok {
			t.Fatal("an xBuddy session cannot say whether its printer has answered about its storages")
		}

		if !report.StoragesReported() {
			t.Fatal("the printer answered SEND_INFO on connecting and the session does not say so")
		}

		drain(report.StoragesChanged())

		if !rig.printer.RemoveStorage("/usb") {
			t.Fatal("the simulated printer had no stick to pull out")
		}

		listing, err := filesOf(t, rig.spy.inner).List(rig.ctx, "")
		if err != nil {
			t.Fatalf("List the root: %v", err)
		}

		if len(listing.Entries) != 0 {
			t.Fatalf("the root lists %+v after the stick was pulled out", listing.Entries)
		}

		if !drain(report.StoragesChanged()) {
			t.Error("the stick was pulled out and nothing woke a reader")
		}

		if _, err = filesOf(t, rig.spy.inner).List(rig.ctx, ""); err != nil {
			t.Fatalf("List the root again: %v", err)
		}

		if drain(report.StoragesChanged()) {
			t.Error("an answer repeating the held list woke a reader")
		}
	})
}

// A root listing asks the printer again, and a person is waiting on the page
// that asked. A printer slow to answer is listed from what it last said within
// the ask's own bound, not after the caller's whole deadline.
func TestARootListingOfASlowPrinterAnswersWithinItsBound(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration)
		defer rig.stop(t)

		rig.connect(t)
		time.Sleep(time.Second)
		synctest.Wait()

		rig.printer.SetResponseDelay(10 * time.Minute)

		began := time.Now()

		listing, err := filesOf(t, rig.spy.inner).List(rig.ctx, "")
		if err != nil {
			t.Fatalf("List the root: %v", err)
		}

		took := time.Since(began)
		t.Logf("observed: the root of a slow printer took %v and lists %d entries", took, len(listing.Entries))

		if took > 10*time.Second {
			t.Errorf("the root of a slow printer took %v", took)
		}

		if len(listing.Entries) != 1 {
			t.Errorf("the root lists %+v, want the stick the printer last reported", listing.Entries)
		}
	})
}

// A printer that is away has no link to ask over. A root listing then answers
// from what it last said and queues nothing, so a page reloaded while the
// printer is gone does not fill the queue a person's own command needs.
func TestARootListingWhileThePrinterIsAwayQueuesNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration)
		defer rig.stop(t)

		rig.connect(t)
		time.Sleep(time.Second)
		synctest.Wait()

		rig.printer.GoOffline()
		time.Sleep(time.Second)
		synctest.Wait()

		files := filesOf(t, rig.spy.inner)

		for range 40 {
			asked, cancel := context.WithTimeout(rig.ctx, time.Minute)
			began := time.Now()

			listing, err := files.List(asked, "")

			cancel()

			if err != nil {
				t.Fatalf("List the root while the printer is away: %v", err)
			}

			if took := time.Since(began); took > time.Second {
				t.Fatalf("the root of a printer that is away took %v", took)
			}

			if len(listing.Entries) != 1 {
				t.Fatalf("the root lists %+v, want the stick the printer last reported", listing.Entries)
			}
		}

		before := rig.printer.Asked("SEND_INFO")

		rig.printer.GoOnline()
		time.Sleep(5 * time.Second)
		synctest.Wait()

		after := rig.printer.Asked("SEND_INFO")
		t.Logf("observed: SEND_INFO asked %d times once the printer came back", after-before)

		if after-before != 1 {
			t.Errorf(
				"the printer was asked SEND_INFO %d times on coming back, want only the attach's own",
				after-before,
			)
		}
	})
}

// Two pages open on one printer ask together. One SEND_INFO answers both,
// because the printer runs one command at a time and a second ask would only
// wait behind the first to hear the same answer.
func TestRootListingsAskingTogetherSendOneInfo(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration)
		defer rig.stop(t)

		rig.connect(t)
		time.Sleep(time.Second)
		synctest.Wait()

		rig.printer.SetResponseDelay(time.Second)

		before := rig.printer.Asked("SEND_INFO")
		files := filesOf(t, rig.spy.inner)

		var wg sync.WaitGroup
		for range 5 {
			wg.Go(func() {
				if _, err := files.List(rig.ctx, ""); err != nil {
					t.Errorf("List the root: %v", err)
				}
			})
		}

		wg.Wait()

		asked := rig.printer.Asked("SEND_INFO") - before
		t.Logf("observed: five root listings at once sent SEND_INFO %d times", asked)

		if asked != 1 {
			t.Errorf("five root listings at once sent SEND_INFO %d times, want 1", asked)
		}
	})
}

// drain reports whether a wake-up was waiting, and takes it.
func drain(changed <-chan struct{}) bool {
	select {
	case <-changed:
		return true
	default:
		return false
	}
}

// The listing a caller reads is what the printer answered SEND_FILE_INFO with,
// in the storage-prefixed relative namespace the rest of the product addresses
// a file in.
func TestADirectoryIsListedFromWhatThePrinterReports(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration, withPrinter(func(cfg *simulator.Config) {
			cfg.Files = []simulator.File{
				{Path: "/usb/models", Folder: true, Modified: 1750000000},
				{Path: "/usb/models/part.bgcode", Size: 4096, Modified: 1750000001},
				{Path: "/usb/models/sub", Folder: true},
				{Path: "/usb/models/sub/deep.bgcode", Size: 8192},
			}
		}))
		defer rig.stop(t)

		rig.connect(t)
		time.Sleep(time.Second)
		synctest.Wait()

		files := filesOf(t, rig.spy.inner)

		root, err := files.List(rig.ctx, "usb")
		if err != nil {
			t.Fatalf("List usb: %v", err)
		}

		t.Logf("observed usb listing: %+v", root)

		if len(root.Entries) != 1 || root.Entries[0].Path != "usb/models" {
			t.Fatalf("usb lists %+v, want one folder usb/models", root.Entries)
		}

		models, err := files.List(rig.ctx, "usb/models")
		if err != nil {
			t.Fatalf("List usb/models: %v", err)
		}

		t.Logf("observed usb/models listing: %+v", models)

		if models.Directory.Path != "usb/models" || !models.Directory.Folder {
			t.Errorf("the directory reads %+v, want usb/models as a folder", models.Directory)
		}

		if len(models.Entries) != 2 {
			t.Fatalf("usb/models lists %d entries, want part.bgcode and sub", len(models.Entries))
		}

		file := models.Entries[0]
		if file.Path != "usb/models/part.bgcode" || file.Size != 4096 || file.Modified != 1750000001 {
			t.Errorf("the file reads %+v, want usb/models/part.bgcode of 4096 bytes", file)
		}

		if file.Folder {
			t.Error("a file was reported as a folder")
		}

		if !models.Entries[1].Folder {
			t.Error("a folder was not reported as a folder")
		}
	})
}

// The firmware names the node it answers about by its 8.3 path and a child by
// its short name beside its long one. A listing still reads in long names,
// under the path it was asked for.
func TestAFolderWithALongNameIsListedUnderThePathItWasAskedFor(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration, withPrinter(func(cfg *simulator.Config) {
			cfg.Files = []simulator.File{
				{Path: "/usb/Summer prints", Folder: true},
				{Path: "/usb/Summer prints/Tangle cube.bgcode", Size: 4096},
			}
		}))
		defer rig.stop(t)

		rig.connect(t)
		time.Sleep(time.Second)
		synctest.Wait()

		folder, err := filesOf(t, rig.spy.inner).List(rig.ctx, "usb/Summer prints")
		if err != nil {
			t.Fatalf("List usb/Summer prints: %v", err)
		}

		t.Logf("observed: %+v", folder)

		if folder.Directory.Path != "usb/Summer prints" {
			t.Errorf("the folder reads as %q, want the path it was asked for", folder.Directory.Path)
		}

		if len(folder.Entries) != 1 {
			t.Fatalf("the folder lists %d entries, want 1", len(folder.Entries))
		}

		entry := folder.Entries[0]
		if entry.Path != "usb/Summer prints/Tangle cube.bgcode" {
			t.Errorf("the file reads as %q, want it under the long-name folder", entry.Path)
		}

		if entry.Name != "TANGLE~1.BGC" || entry.DisplayName != "Tangle cube.bgcode" {
			t.Errorf("the file's names are %q and %q, want the short one and the long one",
				entry.Name, entry.DisplayName)
		}
	})
}

// A send into a long-named folder is judged by a listing of that folder, which
// must read under the long name for the file to be found there.
func TestAFileSentIntoALongNamedFolderLands(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration, withPrinter(func(cfg *simulator.Config) {
			cfg.Files = []simulator.File{{Path: "/usb/Summer prints", Folder: true}}
		}))
		defer rig.stop(t)

		rig.connect(t)

		const path = "usb/Summer prints/Tangle cube.bgcode"

		if err := uploadsOf(t, rig.spy.inner).Send(rig.ctx, connectorapi.Upload{
			Path: path, Content: bytesContent{data: sampleFile(10_000)},
		}); err != nil {
			t.Fatalf("send into the long-named folder: %v", err)
		}

		listing, err := filesOf(t, rig.spy.inner).List(rig.ctx, "usb/Summer prints")
		if err != nil {
			t.Fatalf("list the folder: %v", err)
		}

		if !listingCarries(listing, path) {
			t.Errorf("the file is not listed under the long name: %+v", listing.Entries)
		}
	})
}

// A path that leaves the storage it names is refused before the printer sees
// it, with the code a caller can act on rather than an error nothing reads.
func TestAListedPathThatLeavesItsStorageIsRefused(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration)
		defer rig.stop(t)

		rig.connect(t)
		time.Sleep(time.Second)
		synctest.Wait()

		files := filesOf(t, rig.spy.inner)
		before := rig.spy.calls.Load()

		for _, path := range []string{"usb/../../etc/passwd", "../etc/passwd", "usb/..", ".."} {
			_, err := files.List(rig.ctx, path)
			if err == nil {
				t.Errorf("the traversing path %q was listed", path)

				continue
			}

			var refusal *fault.Fault
			if !errors.As(err, &refusal) || refusal.Code() != connectorapi.PathNotAllowed.Code() {
				t.Errorf("listing %q reported %v, want %s", path, err, connectorapi.PathNotAllowed.Code())
			}
		}

		if got := rig.spy.calls.Load(); got != before {
			t.Errorf("%d commands reached the driver for paths that leave their storage, want none",
				got-before)
		}
	})
}

// A directory the printer will not report is a refusal the caller can tell from
// a listing that is simply empty.
//
// It carries a code as well as the sentinel, because an error with no code
// reaches a caller as core.internal/UNEXPECTED, a 500 with a redacted detail.
// A printer in ERROR refuses every SEND_FILE_INFO, so its Files tab depends on
// this refusal reading as the printer's and not as this server failing.
func TestAPathThePrinterDoesNotHoldIsRefusedRatherThanEmpty(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration)
		defer rig.stop(t)

		rig.connect(t)
		time.Sleep(time.Second)
		synctest.Wait()

		listing, err := filesOf(t, rig.spy.inner).List(rig.ctx, "usb/nothing-here")
		if err == nil {
			t.Fatalf("a path the printer does not hold was listed as %+v", listing)
		}

		if !errors.Is(err, connectorapi.ErrListingRefused) {
			t.Errorf("the refusal reads %v, want one carrying ErrListingRefused", err)
		}

		var refusal *fault.Fault
		if !errors.As(err, &refusal) {
			t.Fatalf("the refusal reads %v, want one carrying a fault", err)
		}

		if refusal.Code() != connectorapi.ListingRefused.Code() {
			t.Errorf("the refusal carries %s, want %s",
				refusal.Code(), connectorapi.ListingRefused.Code())
		}

		if refusal.Status() != http.StatusConflict {
			t.Errorf("the refusal answers %d, want %d", refusal.Status(), http.StatusConflict)
		}

		named, carried := refusal.Params().Get("path")
		if !carried || named.String() != "usb/nothing-here" {
			t.Errorf("the refusal names path %q, want the path that was asked for", named.String())
		}

		t.Logf("observed refusal: %v", err)
	})
}

// The INFO reply carries a command id on the firmware this port was written
// against, and the shipping connector reads the storages out of an INFO
// whatever it carries. A session that only read events matching a command in
// flight would lose the storages on any firmware that volunteers one.
func TestAnInfoEventCarryingNoCommandIDStillTeachesTheSessionItsStorages(t *testing.T) {
	t.Parallel()

	driver := xbuddy.NewDriver(xbuddy.ModeNone, 4, 0, "", nil)

	host := connectorapitest.NewHost(driver.Baseline())

	handler, err := driver.Handler(host)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	conn := dialSocket(t, server.URL, "PRINTER-INFO")
	t.Cleanup(func() { _ = conn.Close() })

	session, err := host.AwaitSession(t.Context())
	if err != nil {
		t.Fatalf("AwaitSession: %v", err)
	}

	// A session is four goroutines, and closing the server and the socket ends
	// none of them: the session outlives a connection on purpose, so only the
	// host releases it.
	t.Cleanup(func() { _ = session.Close() })

	files := filesOf(t, session)

	before, err := files.Storages(t.Context())
	if err != nil {
		t.Fatalf("Storages: %v", err)
	}

	if len(before) != 0 {
		t.Fatalf("the session reports %+v before the printer said anything", before)
	}

	unsolicited := `{"event":"INFO","state":"IDLE","data":{"storages":[` +
		`{"mountpoint":"/local","type":"LOCAL","free_space":512,"read_only":true}]}}`
	if err = conn.WriteMessage(websocket.TextMessage, []byte(unsolicited)); err != nil {
		t.Fatalf("write the INFO: %v", err)
	}

	after := awaitStorages(t, files)

	t.Logf("observed storages after an INFO with no command id: %+v", after)

	if len(after) != 1 || after[0].Mountpoint != "/local" || !after[0].ReadOnly {
		t.Fatalf("the session reports %+v, want one read-only /local", after)
	}

	if !after[0].FreeSpaceKnown || after[0].FreeSpace != 512 {
		t.Errorf("the storage reports %d bytes free, known=%v, want 512 known",
			after[0].FreeSpace, after[0].FreeSpaceKnown)
	}
}

// awaitStorages polls until the reader goroutine has taken the frame. This case
// runs on a real socket rather than in a synctest bubble, because a raw frame
// with no command id is not something a simulated printer sends.
func awaitStorages(t *testing.T, files connectorapi.Filesystem) []connectorapi.Storage {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for {
		storages, err := files.Storages(t.Context())
		if err != nil {
			t.Fatalf("Storages: %v", err)
		}

		if len(storages) > 0 {
			return storages
		}

		if time.Now().After(deadline) {
			t.Fatal("the session never read the storages out of an INFO carrying no command id")
		}

		time.Sleep(time.Millisecond)
	}
}

// A storage other than the default is listed by its own name, as the HT90 lists
// "local". The path was resolved against the default storage, so "local" went
// to the printer as /usb/local, which it has not got.
func TestAStorageOtherThanTheDefaultIsListed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration, withPrinter(func(cfg *simulator.Config) {
			cfg.Storages = []simulator.Storage{
				{Mountpoint: "/usb", Type: "USB", FreeSpace: 1 << 30},
				{Mountpoint: "/local", Type: "LOCAL", FreeSpace: 1 << 24},
			}
			cfg.Files = []simulator.File{{Path: "/local/part.gcode", Size: 4096}}
		}))
		defer rig.stop(t)

		rig.connect(t)
		time.Sleep(time.Second)
		synctest.Wait()

		listing, err := filesOf(t, rig.spy.inner).List(rig.ctx, "local")
		if err != nil {
			t.Fatalf("the second storage was not listed: %v", err)
		}

		if len(listing.Entries) != 1 || listing.Entries[0].Path != "local/part.gcode" {
			t.Errorf("local lists %+v, want local/part.gcode", listing.Entries)
		}
	})
}
