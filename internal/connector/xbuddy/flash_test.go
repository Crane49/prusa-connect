package xbuddy_test

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// firmwareImage is a .bbf under the long name a person copies to the stick.
// The simulator shortens it the way FAT does, to COREON~1.BBF.
const firmwareImage = "/usb/COREONE_firmware_8.0.0.bbf"

func withFirmwareImage(cfg *simulator.Config) {
	cfg.Files = []simulator.File{
		{Path: "/usb/tangle.bgcode", Size: 1 << 20},
		{Path: firmwareImage, Size: 1 << 20},
	}
}

// A flash reaches the printer as M997 naming the image by its 8.3 short name.
// The firmware has no FLASH command and refuses one as unknown, and M997 reads
// only a short name, while every listing hands out the long one.
func TestAFlashReachesThePrinterAsM997WithTheShortName(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration, withPrinter(withFirmwareImage))
		defer rig.stop(t)

		session := rig.connect(t)

		time.Sleep(2 * time.Second)
		synctest.Wait()

		result, err := session.Do(rig.ctx, connectorapi.Command{
			Verb: xbuddy.VerbFlash, Args: map[string]string{"path": "usb/COREONE_firmware_8.0.0.bbf"},
		})
		if err != nil || !result.OK {
			t.Fatalf("the flash was not carried out: %+v, %v", result, err)
		}

		// A flash settles once the frame leaves, so the printer reads it after.
		synctest.Wait()

		flashed, asked := rig.printer.Flashed()
		if !asked {
			t.Fatal("the printer was sent no M997")
		}

		if flashed != "/usb/COREON~1.BBF" {
			t.Errorf("M997 named %q, and the firmware finds the image only as /usb/COREON~1.BBF", flashed)
		}
	})
}

// A flash is never sent to a printer that is printing. M997 resets the board
// at once, so the print would end with it.
func TestAFlashIsNotSentDuringAPrint(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration, withPrinter(withFirmwareImage))
		defer rig.stop(t)

		session := rig.connect(t)

		started, err := session.Do(rig.ctx, connectorapi.Command{
			Verb: xbuddy.VerbStartPrint, Args: map[string]string{"path": "tangle.bgcode"},
		})
		if err != nil || !started.OK {
			t.Fatalf("START_PRINT: %+v, %v", started, err)
		}

		time.Sleep(2 * time.Second)
		synctest.Wait()

		_, err = session.Do(rig.ctx, connectorapi.Command{
			Verb: xbuddy.VerbFlash, Args: map[string]string{"path": "usb/COREONE_firmware_8.0.0.bbf"},
		})

		refusal, ok := errors.AsType[*fault.Fault](err)
		if !ok || refusal.Code() != fault.PrinterVerbNotAllowedInState.Code() {
			t.Fatalf("a flash during a print answered %v, want %s", err, fault.PrinterVerbNotAllowedInState.Code())
		}

		synctest.Wait()

		if _, asked := rig.printer.Flashed(); asked {
			t.Error("the printer was sent M997 while it was printing")
		}
	})
}

// A printer that does not answer the question about the image's short name is
// not flashed, and the flash says it was not sent. The question's own
// COMMAND_UNANSWERED would read as "the printer may have been flashed".
func TestAnUnansweredShortNameIsAFlashNotSent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rig := start(t, "MK4", withAutoRegistration, withPrinter(withFirmwareImage))
		defer rig.stop(t)

		session := rig.connect(t)

		time.Sleep(2 * time.Second)
		synctest.Wait()

		// Longer than the driver's own command deadline, which is a minute.
		rig.printer.SetResponseDelay(2 * time.Minute)

		_, err := session.Do(rig.ctx, connectorapi.Command{
			Verb: xbuddy.VerbFlash, Args: map[string]string{"path": "usb/COREONE_firmware_8.0.0.bbf"},
		})

		refusal, ok := errors.AsType[*fault.Fault](err)
		if !ok || refusal.Code().String() != "core.printer/FLASH_NOT_SENT" {
			t.Fatalf("a flash whose short name went unanswered answered %v, want FLASH_NOT_SENT", err)
		}

		if fault.HasCode(err, connectorapi.CommandUnanswered.Code()) {
			t.Errorf("the flash carries COMMAND_UNANSWERED, so it reads as maybe flashed: %v", err)
		}

		rig.printer.SetResponseDelay(0)
		time.Sleep(4 * time.Minute)
		synctest.Wait()

		if _, asked := rig.printer.Flashed(); asked {
			t.Error("the printer was sent M997 although its short name was never learned")
		}
	})
}
