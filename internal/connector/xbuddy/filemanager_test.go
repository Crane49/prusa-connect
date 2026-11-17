package xbuddy_test

import (
	"errors"
	"testing"
	"testing/synctest"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

func managerOf(t *testing.T, session connectorapi.Session) connectorapi.FileManager {
	t.Helper()

	manager, ok := connectorapi.Manages(session)
	if !ok {
		t.Fatal("an xBuddy session cannot change what is on the printer")
	}

	return manager
}

// The file-management half round-trips against the printer: a folder appears in
// the listing, a file in it goes, and the folder goes.
//
// It is driven through the generic contract rather than through Do, because the
// point of the contract is that a caller never names this dialect's commands.
func TestFileManagementRoundTripsAgainstThePrinter(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration, withPrinter(func(cfg *simulator.Config) {
			cfg.Files = []simulator.File{
				{Path: "/usb/keep.bgcode", Size: 12, Modified: 1},
				{Path: "/usb/scrap.bgcode", Size: 34, Modified: 1},
			}
		}))
		defer rig.stop(t)

		rig.connect(t)

		manager := managerOf(t, rig.spy.inner)
		files := filesOf(t, rig.spy.inner)

		if err := manager.CreateFolder(rig.ctx, "usb/models"); err != nil {
			t.Fatalf("create a folder: %v", err)
		}

		listing, err := files.List(rig.ctx, "usb")
		if err != nil {
			t.Fatalf("list usb: %v", err)
		}

		if !listingCarries(listing, "usb/models") {
			t.Errorf("the folder is not in the listing: %+v", listing.Entries)
		}

		t.Logf("observed: usb/models is in the printer's own listing")

		if err = manager.DeleteFile(rig.ctx, "usb/scrap.bgcode"); err != nil {
			t.Fatalf("delete a file: %v", err)
		}

		if listing, err = files.List(rig.ctx, "usb"); err != nil {
			t.Fatalf("list usb again: %v", err)
		}

		if listingCarries(listing, "usb/scrap.bgcode") {
			t.Error("the deleted file is still in the listing")
		}

		if !listingCarries(listing, "usb/keep.bgcode") {
			t.Error("deleting one file took another with it")
		}

		if err = manager.DeleteFolder(rig.ctx, "usb/models"); err != nil {
			t.Fatalf("delete a folder: %v", err)
		}

		if listing, err = files.List(rig.ctx, "usb"); err != nil {
			t.Fatalf("list usb a third time: %v", err)
		}

		if listingCarries(listing, "usb/models") {
			t.Error("the deleted folder is still in the listing")
		}

		t.Logf("observed: the file and the folder are gone and the untouched file is not")
	})
}

// A delete aimed at the wrong kind of entry is refused, and the refusal carries
// the printer's own word for why. That is the whole reason the two are separate
// calls rather than one with a flag.
func TestADeleteAimedAtTheWrongKindIsRefused(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration, withPrinter(func(cfg *simulator.Config) {
			cfg.Files = []simulator.File{
				{Path: "/usb/models", Folder: true, Modified: 1},
				{Path: "/usb/part.bgcode", Size: 12, Modified: 1},
			}
		}))
		defer rig.stop(t)

		rig.connect(t)
		manager := managerOf(t, rig.spy.inner)

		fileAtAFolder := manager.DeleteFile(rig.ctx, "usb/models")
		if fileAtAFolder == nil {
			t.Fatal("deleting a folder as a file was accepted")
		}

		if !errors.Is(fileAtAFolder, connectorapi.ErrFileChangeRefused) {
			t.Errorf("the refusal is not a file-change refusal: %v", fileAtAFolder)
		}

		t.Logf("observed: %v", fileAtAFolder)

		folderAtAFile := manager.DeleteFolder(rig.ctx, "usb/part.bgcode")
		if folderAtAFile == nil {
			t.Fatal("deleting a file as a folder was accepted")
		}

		t.Logf("observed: %v", folderAtAFile)
	})
}

// A folder whose name the storage already holds is refused rather than reported
// as created, because a caller that asked to create it wanted an empty one.
func TestCreatingAFolderThatExistsIsRefused(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration)
		defer rig.stop(t)

		rig.connect(t)
		manager := managerOf(t, rig.spy.inner)

		if err := manager.CreateFolder(rig.ctx, "usb/models"); err != nil {
			t.Fatalf("create a folder: %v", err)
		}

		err := manager.CreateFolder(rig.ctx, "usb/models")
		if err == nil {
			t.Fatal("creating a folder that already exists was accepted")
		}

		t.Logf("observed: %v", err)
	})
}

// A path that leaves the storage it names is refused for every management call
// and never reaches the printer, which is the rule every path-bearing verb takes.
func TestFileManagementRefusesAPathThatEscapes(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration)
		defer rig.stop(t)

		rig.connect(t)
		manager := managerOf(t, rig.spy.inner)

		escaping := "usb/../../etc/passwd"

		// A slice rather than subtests: t.Run is not allowed inside a synctest
		// bubble, and a map would make the order of the log lines vary.
		calls := []struct {
			name string
			call func() error
		}{
			{"create a folder", func() error { return manager.CreateFolder(rig.ctx, escaping) }},
			{"delete a file", func() error { return manager.DeleteFile(rig.ctx, escaping) }},
			{"delete a folder", func() error { return manager.DeleteFolder(rig.ctx, escaping) }},
		}

		for _, each := range calls {
			err := each.call()
			if err == nil {
				t.Errorf("%s accepted a path that leaves its storage", each.name)

				continue
			}

			t.Logf("observed: %s refused with %v", each.name, err)
		}
	})
}
