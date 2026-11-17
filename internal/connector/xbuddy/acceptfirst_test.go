package xbuddy_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// A printer on the Prusa Connect SDK, such as the HT90, answers every command
// with ACCEPTED before the event that carries the answer. The answer is the
// event after it.
func TestAnAcceptedBeforeTheAnswerDoesNotAnswerTheCommand(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration, withPrinter(func(cfg *simulator.Config) {
			cfg.Files = []simulator.File{{Path: "/usb/part.bgcode", Size: 4096}}
		}))
		defer rig.stop(t)

		session := rig.connect(t)
		time.Sleep(time.Second)
		synctest.Wait()

		rig.printer.SetAcceptFirst(true)

		listing, err := filesOf(t, rig.spy.inner).List(rig.ctx, "usb")
		if err != nil {
			t.Fatalf("a folder answered with ACCEPTED and then FILE_INFO was not listed: %v", err)
		}

		t.Logf("observed usb listing: %+v", listing)

		if len(listing.Entries) != 1 || listing.Entries[0].Path != "usb/part.bgcode" {
			t.Errorf("usb lists %+v, want usb/part.bgcode", listing.Entries)
		}

		result, err := session.Do(rig.ctx, connectorapi.Command{Verb: xbuddy.VerbSetPrinterReady})
		if err != nil {
			t.Fatalf("a command answered with ACCEPTED and then FINISHED failed: %v", err)
		}

		if !result.OK {
			t.Errorf("the command was refused: %s", result.Raw)
		}

		if got, want := rig.printer.State(), "READY"; got != want {
			t.Errorf("the printer is %q after the command, want %q", got, want)
		}
	})
}
