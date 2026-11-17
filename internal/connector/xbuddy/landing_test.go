package xbuddy_test

import (
	"errors"
	"testing"
	"testing/synctest"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The two names BUG-014 was found with. Both shorten to the FAT 8.3 spelling
// BUG009~1.GCO, so a storage that holds the first has nowhere to put the
// second.
const (
	firstColliding  = "usb/bug009-probe.gcode"
	secondColliding = "usb/bug009-control.gcode"
	// distinctName shortens to ZZZ-CL~1.GCO, which nothing in that directory
	// holds, and it is the control the live run took (BUG-014, wave two).
	distinctName = "usb/zzz-clean-name.gcode"
)

// collidingSize is the length of the file the live run sent, kept because the
// fault reports it and a reader compares the two.
const collidingSize = 8490

// A transfer the printer reported finished, whose file is not on the storage
// under the name it was sent to, is not a transfer that finished.
//
// BUG-014, on a CORE One L at 6.8.1+16182: two files whose long names shorten
// to one 8.3 spelling were both answered `state: done`,
// `bytes_sent == bytes_total`, and the printer's own listing held only the
// first of them for ever. Nothing on the transfer's wire says so, which is why
// the driver reads the directory back.
func TestATransferWhoseFileNeverLandedIsNotRecordedAsDone(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration)
		defer rig.stop(t)

		rig.connect(t)

		uploads := uploadsOf(t, rig.spy.inner)
		data := sampleFile(collidingSize)

		if err := uploads.Send(rig.ctx, connectorapi.Upload{
			Path: firstColliding, Content: bytesContent{data: data},
		}); err != nil {
			t.Fatalf("send the first file: %v", err)
		}

		err := uploads.Send(rig.ctx, connectorapi.Upload{
			Path: secondColliding, Content: bytesContent{data: data},
		})
		if err == nil {
			t.Fatal("the second file was reported as sent and the printer holds no such file")
		}

		var refusal *fault.Fault
		if !errors.As(err, &refusal) {
			t.Fatalf("the failure is %v, and a caller reads a code", err)
		}

		if got, want := refusal.Code().String(), connectorapi.TransferNotOnStorage.Code().String(); got != want {
			t.Errorf("the failure carries %s, want %s", got, want)
		}

		t.Logf("observed: the second transfer failed with %s and details %v",
			refusal.Code(), refusal.Params())

		for name, want := range map[string]string{
			"path":          secondColliding,
			"expected_size": "8490",
			"found":         "absent",
		} {
			reported, carried := refusal.Params().Get(name)
			if !carried {
				t.Errorf("the failure carries no %s", name)

				continue
			}

			if got := reported.String(); got != want {
				t.Errorf("the failure reports %s = %q, want %q", name, got, want)
			}
		}

		// And it reports the printer's own state, which is the reason the
		// transfer is not done: one name, and it is the first one.
		listing, listErr := filesOf(t, rig.spy.inner).List(rig.ctx, "usb")
		if listErr != nil {
			t.Fatalf("list usb: %v", listErr)
		}

		if !listingCarries(listing, firstColliding) {
			t.Errorf("the first file is not in the printer's listing of usb: %+v", listing.Entries)
		}

		if listingCarries(listing, secondColliding) {
			t.Errorf("the second file is in the listing, so this case proves nothing: %+v",
				listing.Entries)
		}
	})
}

// The control, and it is the case the live run took beside the failing one: a
// second file whose short name nothing holds lands, and its transfer finishes
// with no fault and no warning.
func TestASecondTransferWithItsOwnShortNameFinishes(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration)
		defer rig.stop(t)

		rig.connect(t)

		uploads := uploadsOf(t, rig.spy.inner)
		data := sampleFile(collidingSize)

		if err := uploads.Send(rig.ctx, connectorapi.Upload{
			Path: firstColliding, Content: bytesContent{data: data},
		}); err != nil {
			t.Fatalf("send the first file: %v", err)
		}

		var last connectorapi.Progress

		if err := uploads.Send(rig.ctx, connectorapi.Upload{
			Path:     distinctName,
			Content:  bytesContent{data: data},
			Progress: func(report connectorapi.Progress) { last = report },
		}); err != nil {
			t.Fatalf("send a file whose short name is its own: %v", err)
		}

		if last.Warning != "" {
			t.Errorf("a confirmed transfer carries the warning %q", last.Warning)
		}

		listing, err := filesOf(t, rig.spy.inner).List(rig.ctx, "usb")
		if err != nil {
			t.Fatalf("list usb: %v", err)
		}

		for _, path := range []string{firstColliding, distinctName} {
			if !listingCarries(listing, path) {
				t.Errorf("%s is not in the printer's listing of usb: %+v", path, listing.Entries)
			}
		}

		t.Logf("observed: both files are in the printer's own listing of usb")
	})
}
