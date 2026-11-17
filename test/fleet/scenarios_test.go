package fleet

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/internal/connector/xbuddy"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The two verbs the storage scenarios need by name. Both carry a path, and the
// difference between them is what the two faults are told apart by: a missing
// device refuses either, and a device with no space left refuses only the write.
// A model that stopped declaring one fails the scenario rather than skipping it.
const (
	startPrint   connectorapi.Capability = "core.command.start_print"
	createFolder connectorapi.Capability = "core.command.create_folder"
)

// The three argument-free verbs that need a job on the bed. A printer refuses
// each of them when there is nothing to pause, resume or stop, so a scenario
// sending them at an idle machine reads a refusal as a broken wire. They are
// named here so the verb sweep can put the printer in the state each one
// applies to, in this order.
var lifecycleVerbs = []connectorapi.Capability{
	"core.command.pause_print",
	"core.command.resume_print",
	"core.command.stop_print",
}

// sendInfo is a verb every profile declares and that leaves as a JSON command
// rather than as gcode, so a scenario naming it exercises the other half of the
// wire from the one an argument-free verb picked out of the data reaches.
const sendInfo connectorapi.Capability = "core.command.send_info"

// The three verbs every shipping profile declares that carry arguments of their
// own rather than a path. Each is named here because a scenario that picked one
// out of the data would stop exercising it the moment the data moved, and these
// are the two things a machine standing in front of somebody needs: the dialog
// on its display answered, and the firmware sitting on its stick installed.
const (
	dialogAction connectorapi.Capability = "core.command.dialog_action"
	upgrade      connectorapi.Capability = "core.command.upgrade"
	flash        connectorapi.Capability = "core.command.flash"
)

// firmwareImage is a path on the printer's own removable storage. It is where a
// firmware image really sits: the operator writes it to a stick, and UPGRADE
// names it.
const firmwareImage = "/usb/firmware.bbf"

// transferPath is where the transfer scenario lands its file: storage-prefixed
// and relative, the namespace connectorapi.Upload.Path is in.
const transferPath = "usb/matrix.bgcode"

// transferSize is how long the file the matrix sends is.
//
// It is longer than the 262,144 bytes one block request covers and not a
// multiple of the 1,024-byte frame this board takes, so the transfer spans more
// than one block request and ends on a partial chunk. Both are shapes an
// off-by-one in the inclusive end of a range produces a wrong file for, and the
// digest is what catches it.
const transferSize = 300_000

// transferBudget bounds one whole transfer, the printer's own reset and the
// reconnection after it included. It is virtual time, so it is generous on
// purpose: a transfer that wedges is a failure rather than a hang.
const transferBudget = 5 * time.Minute

// unmatchedFirmware is below every profile band's floor, so nothing covers it.
// It is the firmware a printer reports the day the vendor ships a lineup this
// build has never seen, which is the case earlier research says the
// profile data cannot currently detect on its own.
const unmatchedFirmware = "0.0.1"

// offlineGrace is how long a session under test outlives its printer. It is
// virtual time, so the value is chosen to be unmistakable in a log rather than
// to be cheap.
const offlineGrace = time.Minute

// commandBudget bounds one command. It is virtual time inside the bubble, so
// its only role is to make a wedge a failure instead of a hang.
const commandBudget = 30 * time.Second

// telemetryBudget is how long a scenario waits for the first state patch. The
// simulator's idle cadence is 4 Hz, so anything above a second is slack.
const telemetryBudget = 10 * time.Second

// slowPrinterDelay is longer than slowPrinterBudget, so the command under test
// cannot succeed and the deadline is what ends it.
const (
	slowPrinterDelay  = 5 * time.Minute
	slowPrinterBudget = 10 * time.Second
)

// scenario is one thing that can break, run against one cell.
type scenario struct {
	// name is the subtest name, and what a failure is reported under.
	name string
	// catches is the regression this scenario exists to find. It is logged on
	// every run so a reader of the output knows what was proved.
	catches string
	// run drives one cell.
	run func(t *testing.T, cell Cell)
}

// scenarios is the battery every cell runs. Adding one here adds it to every
// model and every firmware version at once, which is the point of the matrix.
//
//nolint:gochecknoglobals // the battery is data, read-only after init.
var scenarios = []scenario{
	{
		name:    "registers-and-becomes-a-session",
		catches: "a change to the frozen registration handshake or to what the host is told a printer is",
		run:     scenarioRegisters,
	},
	{
		name:    "resolves-the-capabilities-its-golden-names",
		catches: "a profile edit or a baseline change that moves what this printer is believed to support",
		run:     scenarioCapabilities,
	},
	{
		name:    "every-declared-verb-round-trips",
		catches: "a verb this model declares that no longer reaches the printer or no longer completes",
		run:     scenarioDeclaredVerbs,
	},
	{
		name:    "every-undeclared-verb-is-refused-before-the-driver",
		catches: "a capability check that stopped running, so a verb this model does not have reaches the wire",
		run:     scenarioUndeclaredVerbs,
	},
	{
		name:    "telemetry-becomes-state-patches",
		catches: "a telemetry key that stopped reaching the addressable state tree",
		run:     scenarioTelemetry,
	},
	{
		name:    "a-command-queued-while-offline-drains-on-reconnect",
		catches: "the queue-and-drain rule, which earlier research says is the one most likely to be lost",
		run:     scenarioOfflineDrain,
	},
	{
		name:    "a-storage-device-pulled-refuses-the-print",
		catches: "a USB stick removed mid-job being reported as a success",
		run:     scenarioStoragePulled,
	},
	{
		name:    "a-full-storage-device-refuses-the-write-and-serves-the-read",
		catches: "out of space reported as a success, which is where storage errors actually come from",
		run:     scenarioFullStorage,
	},
	{
		name:    "a-slow-printer-does-not-wedge-the-session",
		catches: "a command with no deadline of its own, so one unresponsive printer holds a caller forever",
		run:     scenarioSlowPrinter,
	},
	{
		name: "a-session-ends-after-the-offline-grace",
		catches: "a session that outlives an absence forever, so a printer switched off for good is " +
			"never reaped and its queue is never released",
		run: scenarioOfflineGrace,
	},
	{
		name:    "a-firmware-no-profile-covers-falls-back-to-the-baseline",
		catches: "an unmatched firmware being refused instead of served on a reduced set",
		run:     scenarioUnmatchedFirmware,
	},
	{
		name: "a-dialog-on-the-display-can-be-answered",
		catches: "a printer stopped on a dialog that no request can dismiss, because the verb its " +
			"profile declares does not reach the wire",
		run: scenarioDialogReply,
	},
	{
		name: "a-firmware-image-on-the-printer-can-be-installed",
		catches: "a firmware upgrade that has to be done at the machine, because neither verb the " +
			"profile declares for it reaches the wire",
		run: scenarioUpgrade,
	},
	{
		name: "a-file-reaches-the-printers-storage",
		catches: "a file that never lands on this model, or lands changed, because the transfer " +
			"exchange or the reset the printer performs after accepting broke for it",
		run: scenarioFileTransfer,
	},
}

// scenarioDialogReply answers a dialog the printer is showing.
//
// Both directions, because a permit test without its refusal is half a test: the
// reply the printer accepts, and a reply naming no button, which the host has to
// refuse before the driver is reached rather than send as a command the firmware
// cannot read.
//
// The dialog is raised first. A reply the machine takes needs a question on the
// display, and the three refusals below it are the firmware's own: no dialog,
// another dialog's id, and a button the display does not offer.
func scenarioDialogReply(t *testing.T, cell Cell) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		rig := newRig(t, cell)
		defer rig.stop(t)

		session := rig.connect(t)
		requireDeclared(t, cell, session, dialogAction, "dialog_id", "button")

		ctx, cancel := context.WithTimeout(rig.ctx, commandBudget)
		defer cancel()

		reply := func(name, id, button string) connectorapi.Result {
			t.Helper()

			result, err := session.Do(ctx, connectorapi.Command{
				Verb: dialogAction,
				Args: map[string]string{"dialog_id": id, "button": button},
			})
			if err != nil {
				t.Fatalf("%s: %s: %v", cell, name, err)
			}

			return result
		}

		refused := func(name string, result connectorapi.Result, reason string) {
			t.Helper()

			if result.OK {
				t.Errorf("%s: %s was accepted", cell, name)

				return
			}

			if !bytes.Contains(result.Raw, []byte(reason)) {
				t.Errorf("%s: %s was refused and does not say %s: %s", cell, name, reason, result.Raw)
			}
		}

		// Nothing is on the display, so there is no question to answer.
		refused("a reply with no dialog on the display", reply("no dialog", "1", "Yes"),
			simulator.ReasonNoDialog)

		if !rig.printer.RaiseDialog(simulator.Dialog{
			ID: 1, Code: "13501", Title: "Filament runout",
			Text: "Insert filament, then press Retry.", Buttons: []string{"Yes", "Cancel"},
		}) {
			t.Fatalf("%s: the dialog never reached the wire", cell)
		}

		refused("a reply naming another dialog", reply("wrong id", "2", "Yes"),
			simulator.ReasonInvalidDialogID)
		refused("a reply naming a button the display does not offer",
			reply("wrong button", "1", "Retry"), simulator.ReasonInvalidButton)

		answered := reply("the dialog on the display", "1", "Yes")
		if !answered.OK {
			t.Errorf("%s: the printer refused the dialog reply: %s", cell, answered.Raw)
		}

		// Answering closed it, so the same reply now has nothing to answer.
		refused("a second reply to a dialog that has gone", reply("closed", "1", "Yes"),
			simulator.ReasonNoDialog)

		before := rig.spy.calls.Load()

		if _, err := session.Do(ctx, connectorapi.Command{
			Verb: dialogAction, Args: map[string]string{"dialog_id": "1"},
		}); err == nil {
			t.Errorf("%s: a dialog reply naming no button was accepted", cell)
		}

		if after := rig.spy.calls.Load(); after != before {
			t.Errorf("%s: the driver was reached %d times for a dialog reply naming no button",
				cell, after-before)
		}

		t.Logf("observed: %s answered dialog 1 with Yes and refused four replies it could not answer", cell)
	})
}

// scenarioUpgrade installs a firmware image sitting on the printer's own
// storage, through both verbs the shipping matrix carries for it. Both reach the
// machine as M997 naming the image by its 8.3 name, the one form the firmware
// flashes on.
//
// The refusal half is the path rule: the image is named in the caller's
// storage-prefixed namespace and a path climbing out of it never reaches the
// machine, which is the one check the shipping validator does not make on these
// two verbs.
func scenarioUpgrade(t *testing.T, cell Cell) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		rig := newRig(t, cell, withFiles(simulator.File{Path: firmwareImage, Size: 1 << 20}))
		defer rig.stop(t)

		session := rig.connect(t)

		ctx, cancel := context.WithTimeout(rig.ctx, commandBudget)
		defer cancel()

		for _, verb := range []connectorapi.Capability{upgrade, flash} {
			requireDeclared(t, cell, session, verb, "path")

			started, err := session.Do(ctx, connectorapi.Command{
				Verb: verb, Args: map[string]string{"path": firmwareImage},
			})
			if err != nil {
				t.Fatalf("%s: %s from %s: %v", cell, verb, firmwareImage, err)
			}

			if !started.OK {
				t.Errorf("%s: the printer refused %s: %s", cell, verb, started.Raw)
			}

			// A flash settles once the frame leaves, so the printer reads it after.
			synctest.Wait()

			if flashed, asked := rig.printer.Flashed(); !asked || flashed != "/usb/FIRMWARE.BBF" {
				t.Errorf("%s: %s reached the printer as M997 %q (sent: %v), want /usb/FIRMWARE.BBF",
					cell, verb, flashed, asked)
			}

			before := rig.spy.calls.Load()

			if _, err = session.Do(ctx, connectorapi.Command{
				Verb: verb, Args: map[string]string{"path": "usb/../etc/passwd"},
			}); err == nil {
				t.Errorf("%s: %s accepted a path that climbs out of its storage", cell, verb)
			}

			if after := rig.spy.calls.Load(); after == before {
				t.Errorf("%s: %s with a climbing path never reached the driver, so the refusal came "+
					"from somewhere other than the path rule", cell, verb)
			}
		}

		t.Logf("observed: %s installed %s through both verbs and refused a climbing path",
			cell, firmwareImage)
	})
}

// scenarioFileTransfer sends one file to the printer and proves it arrived
// whole, on every model and every firmware version.
//
// Every shipping profile declares core.feature.file_upload, and a declaration a
// model is never asked to honour means nothing. The file goes through
// capability.Session.Send, the same host path the API surface takes, so what is
// proved is the whole route and not the driver alone.
//
// The printer resets its connection of its own accord after accepting, every
// time (simulator.ErrTransferReset), so every cell also drives the resume: the
// bytes counted before the drop, and one file across the two connections. The
// session count either side of the transfer is what says the reset happened
// rather than that this cell was quietly spared it. That the resumed transfer
// asks under a new file id is the simulator's own suite, which can see the id.
//
// Both refusals are here too, because a permit test without its refusal is half
// a test. Neither is reachable from the shipping data: every profile declares
// the capability and the coverage tier carries it, so no cell of the matrix
// lacks it, and the refusals are constructed the way
// TestABandWithACeilingRefusesTheFirmwareAboveIt constructs the ceiling the data
// never reaches.
func scenarioFileTransfer(t *testing.T, cell Cell) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		rig := newRig(t, cell)
		defer rig.stop(t)

		session := rig.connect(t)
		requireDeclared(t, cell, session, connectorapi.VerbFileUpload)

		file := transferFile()

		ctx, cancel := context.WithTimeout(rig.ctx, transferBudget)
		defer cancel()

		connections := rig.printer.Sessions()

		if err := session.Send(ctx, connectorapi.Upload{
			Path: transferPath, Content: heldContent{data: file.data},
		}); err != nil {
			t.Fatalf("%s: send %d bytes to %s: %v", cell, len(file.data), transferPath, err)
		}

		landed, fetched := rig.printer.LastTransfer()
		if !fetched {
			t.Fatalf("%s: the send returned and the printer fetched nothing", cell)
		}

		if !landed.Finished {
			t.Fatalf("%s: the send returned before the printer reported the transfer finished: %+v",
				cell, landed)
		}

		if landed.Received != int64(len(file.data)) {
			t.Errorf("%s: the printer received %d bytes, and the file is %d",
				cell, landed.Received, len(file.data))
		}

		if landed.Digest != file.digest {
			t.Errorf("%s: the printer received a different file: digest %s, want %s",
				cell, landed.Digest, file.digest)
		}

		if want := "/" + transferPath; landed.Path != want {
			t.Errorf("%s: the file landed at %q, want %q", cell, landed.Path, want)
		}

		if after := rig.printer.Sessions(); after <= connections {
			t.Errorf("%s: the printer held %d connections before the transfer and %d after, so the "+
				"reset it performs for having accepted one never happened here", cell, connections, after)
		}

		t.Logf("observed: %s took %d bytes to %s over %d connections, digest %s",
			cell, landed.Received, landed.Path, rig.printer.Sessions(), landed.Digest)

		refuseTransferOutOfItsStorage(ctx, t, cell, rig, session)
		refuseTransferToAPrinterWithoutTheVerb(ctx, t, cell, rig, session)
	})
}

// refuseTransferOutOfItsStorage is the path rule on the transfer surface. A
// destination that leaves the storage it names is the host's to refuse, and the
// driver is never reached: nothing below the host can be trusted to remember it.
func refuseTransferOutOfItsStorage(
	ctx context.Context, t *testing.T, cell Cell, rig *rig, session *capability.Session,
) {
	t.Helper()

	before := rig.spy.transfers.Load()

	err := session.Send(ctx, connectorapi.Upload{
		Path: "usb/../etc/passwd", Content: heldContent{data: []byte("x")},
	})
	if err == nil {
		t.Fatalf("%s: a file aimed out of the storage it names was accepted", cell)
	}

	// The code and not only the refusal, for the reason the other half checks
	// one: a printer refused for the wrong reason is a caller told to fix the
	// wrong thing.
	var refusal *fault.Fault
	if !errors.As(err, &refusal) {
		t.Fatalf("%s: the refusal is not a fault: %v", cell, err)
	}

	if got, want := refusal.Code(), connectorapi.PathNotAllowed.Code(); got != want {
		t.Errorf("%s: a file aimed out of its storage was refused with %s, want %s", cell, got, want)
	}

	reached := rig.spy.transfers.Load() - before
	if reached != 0 {
		t.Errorf("%s: the driver was reached %d times for a file aimed out of its storage",
			cell, reached)
	}

	t.Logf("observed: %s refused a file aimed out of its storage with %s, and the driver was "+
		"reached %d times", cell, refusal.Code(), reached)
}

// refuseTransferToAPrinterWithoutTheVerb is the cell the matrix does not carry:
// a printer whose resolved set does not declare that it can be sent a file.
//
// The set is narrowed on the live session rather than skipped over, because a
// scenario that skipped would report a pass for a check that never ran. It is
// last in the scenario, so nothing after it is asked of a session running a
// reduced set.
func refuseTransferToAPrinterWithoutTheVerb(
	ctx context.Context, t *testing.T, cell Cell, rig *rig, session *capability.Session,
) {
	t.Helper()

	reduced := session.Resolution()
	without := reduced.Capabilities.Clone()
	delete(without, connectorapi.VerbFileUpload)
	reduced.Capabilities = without
	reduced.Digest = without.Digest()
	session.Adopt(reduced)

	before := rig.spy.transfers.Load()

	err := session.Send(ctx, connectorapi.Upload{
		Path: transferPath, Content: heldContent{data: []byte("x")},
	})
	if err == nil {
		t.Fatalf("%s: a printer whose resolved set does not carry %s was sent a file",
			cell, connectorapi.VerbFileUpload)
	}

	var refusal *fault.Fault
	if !errors.As(err, &refusal) {
		t.Fatalf("%s: the refusal is not a fault: %v", cell, err)
	}

	if got, want := refusal.Code(), connectorapi.CapabilityNotSupported.Code(); got != want {
		t.Errorf("%s: a file offered to a printer without %s was refused with %s, want %s",
			cell, connectorapi.VerbFileUpload, got, want)
	}

	reached := rig.spy.transfers.Load() - before
	if reached != 0 {
		t.Errorf("%s: the driver was reached %d times for a printer that does not declare %s",
			cell, reached, connectorapi.VerbFileUpload)
	}

	t.Logf("observed: %s refused a file for a resolved set without %s with %s, and the driver was "+
		"reached %d times", cell, connectorapi.VerbFileUpload, refusal.Code(), reached)
}

// sample is the file the whole matrix sends, and the digest of it.
type sample struct {
	data   []byte
	digest string
}

// transferFile is that file, built once for every cell of the run.
//
// One fixture rather than one per cell: every cell sends the same bytes, so a
// digest that differs names the cell and not the file. The bytes are
// pseudorandom, because a transfer that reordered or repeated a block produces
// the same digest for a file of one repeated byte.
//
//nolint:gochecknoglobals // a once-value over a fixture, read-only after init.
var transferFile = sync.OnceValue(func() sample {
	source := rand.NewChaCha8([32]byte{11})
	data := make([]byte, transferSize)
	_, _ = source.Read(data)
	sum := sha256.Sum256(data)

	return sample{data: data, digest: hex.EncodeToString(sum[:])}
})

// heldContent is the fixture as a connectorapi.Content.
//
// It holds its bytes, which nothing on the send path may do: 300 KB shared by
// the whole matrix is a fixture, and that a transfer holds none of the file is
// TestATransferHoldsNoneOfTheFile in the driver's own suite.
type heldContent struct{ data []byte }

func (c heldContent) Size() int64 { return int64(len(c.data)) }

func (c heldContent) Open(_ context.Context, offset int64) (io.ReadCloser, error) {
	if offset >= int64(len(c.data)) {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}

	return io.NopCloser(bytes.NewReader(c.data[offset:])), nil
}

// requireDeclared fails when a verb the cell's own profile carries did not
// survive the merge with the driver's baseline, naming the key that went. A verb
// the data declares and no driver can send is the gap this reports, and it is a
// different failure from the command coming back refused.
func requireDeclared(
	t *testing.T, cell Cell, session *capability.Session, verb connectorapi.Capability, args ...string,
) {
	t.Helper()

	resolved, _ := session.Capabilities()

	keys := []connectorapi.Capability{verb}
	for _, name := range args {
		keys = append(keys, connectorapi.ArgumentKey(verb, name))
	}

	for _, key := range keys {
		if !resolved.Has(key) {
			t.Fatalf("%s: this model's profile declares %s and the resolved set does not carry it, "+
				"so the printer cannot be asked", cell, key)
		}
	}
}

// scenarioRegisters is the handshake and the identity the host is told.
func scenarioRegisters(t *testing.T, cell Cell) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		rig := newRig(t, cell)
		defer rig.stop(t)

		session := rig.connect(t)
		identity := session.Identity()

		if identity.Model != cell.Model {
			t.Errorf("the host was told the model is %q, want %q", identity.Model, cell.Model)
		}

		if want := connectorapi.NewFirmwareLabel(cell.Firmware); identity.Firmware != want {
			t.Errorf("the host was told a firmware that is not %q", cell.Firmware)
		}

		if identity.Serial != rig.printer.Serial() {
			t.Errorf("the host was told the serial is %q, want %q", identity.Serial, rig.printer.Serial())
		}

		if identity.Vendor != xbuddy.Vendor {
			t.Errorf("the host was told the vendor is %q, want %q", identity.Vendor, xbuddy.Vendor)
		}

		admitted, matched := rig.host.admissions()
		if len(admitted) != 1 {
			t.Fatalf("%d printers were offered to the host, want 1", len(admitted))
		}

		if !matched[0] {
			t.Errorf("no profile covered %s, and this cell is one the profile data claims", cell)
		}

		resolution := session.Resolution()
		if !resolution.ProfileMatched {
			t.Errorf("the resolution says no profile matched %s", cell)
		}

		if resolution.ProfileRange.From != cell.Band.From {
			t.Errorf("the profile that matched declares a floor of %q, want %q",
				resolution.ProfileRange.From, cell.Band.From)
		}

		t.Logf("observed: %s admitted on %d capabilities, digest %s, profile band from %s",
			cell, len(resolution.Capabilities), resolution.Digest, resolution.ProfileRange.From)
	})
}

// scenarioCapabilities holds the live resolution to the committed golden.
func scenarioCapabilities(t *testing.T, cell Cell) {
	t.Helper()

	committed, err := committedGoldens()
	if err != nil {
		t.Fatalf("read the goldens: %v", err)
	}

	want, present := committed[cell.String()]
	if !present {
		t.Fatalf("%s carries no entry for %s. Regenerate it with "+
			"`go test ./test/fleet -run TestTheCapabilityGoldensAreCurrent -update`", goldenFile, cell)
	}

	synctest.Test(t, func(t *testing.T) {
		rig := newRig(t, cell)
		defer rig.stop(t)

		got := summarise(rig.connect(t).Resolution())

		if got != want {
			t.Errorf("%s resolved to %+v over the wire, and %s says %+v", cell, got, goldenFile, want)
		}

		t.Logf("observed: %s resolved %d capabilities over the wire, digest %s",
			cell, got.Capabilities, got.Digest)
	})
}

// scenarioDeclaredVerbs sends every verb this cell declares that takes no
// arguments, and requires each to come back accepted.
//
// Every verb rather than a representative one: a wire rewrite that breaks one
// verb breaks it for every model that declares it, and picking a sample is how
// that ships.
//
// Three of the verbs need a print in progress, so the sweep runs in two parts:
// everything else at an idle printer, then a print started and paused, resumed
// and stopped in that order. Sending all of them at an idle machine asks it for
// a pause it has nothing to pause, and the answer to that is a refusal.
func scenarioDeclaredVerbs(t *testing.T, cell Cell) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		rig := newRig(t, cell)
		defer rig.stop(t)

		session := rig.connect(t)
		resolved, _ := session.Capabilities()
		verbs := argumentFreeVerbs(resolved)

		if len(verbs) == 0 {
			t.Fatalf("%s declares no verb without arguments, so this scenario sent nothing", cell)
		}

		ctx, cancel := context.WithTimeout(rig.ctx, commandBudget)
		defer cancel()

		accept := func(verb connectorapi.Capability, command connectorapi.Command) {
			result, err := session.Do(ctx, command)
			if err != nil {
				t.Errorf("%s: %s: %v", cell, verb, err)

				return
			}

			if !result.OK {
				t.Errorf("%s: %s came back refused: %s", cell, verb, result.Raw)
			}
		}

		sent := 0

		for _, verb := range verbs {
			if slices.Contains(lifecycleVerbs, verb) {
				continue
			}

			accept(verb, connectorapi.Command{Verb: verb})

			sent++
		}

		if !resolved.Has(startPrint) {
			t.Fatalf("%s does not declare %s, so the verbs that need a job cannot be sent", cell, startPrint)
		}

		accept(startPrint, connectorapi.Command{
			Verb: startPrint,
			Args: map[string]string{"path": "/usb/tangle.bgcode"},
		})

		for _, verb := range lifecycleVerbs {
			if !resolved.Has(verb) {
				t.Fatalf("%s does not declare %s, and every shipping profile does", cell, verb)
			}

			accept(verb, connectorapi.Command{Verb: verb})

			sent++
		}

		t.Logf("observed: %s answered %d declared verbs, every one accepted, %d of them over a job",
			cell, sent, len(lifecycleVerbs))
	})
}

// scenarioUndeclaredVerbs sends every verb the driver can put on the wire that
// this cell's profile does not declare, and requires the host to refuse each
// before the driver is reached.
func scenarioUndeclaredVerbs(t *testing.T, cell Cell) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		rig := newRig(t, cell)
		defer rig.stop(t)

		session := rig.connect(t)
		resolved, _ := session.Capabilities()
		verbs := verbsOutside(rig.driver.Baseline(), resolved)

		if len(verbs) == 0 {
			t.Fatalf("%s declares every verb the driver has, so this scenario sent nothing", cell)
		}

		before := rig.spy.calls.Load()

		ctx, cancel := context.WithTimeout(rig.ctx, commandBudget)
		defer cancel()

		for _, verb := range verbs {
			_, err := session.Do(ctx, connectorapi.Command{Verb: verb})
			if err == nil {
				t.Errorf("%s: accepted %s, which its profile does not declare", cell, verb)

				continue
			}

			var refusal *fault.Fault
			if !errors.As(err, &refusal) {
				t.Errorf("%s: %s was refused with something that is not a fault: %v", cell, verb, err)

				continue
			}

			if got, want := refusal.Code(), connectorapi.CapabilityNotSupported.Code(); got != want {
				t.Errorf("%s: %s was refused with %s, want %s", cell, verb, got, want)
			}
		}

		if after := rig.spy.calls.Load(); after != before {
			t.Errorf("%s: the driver was reached %d times for verbs the profile does not declare",
				cell, after-before)
		}

		t.Logf("observed: %s refused %d undeclared verbs and the driver was reached %d times",
			cell, len(verbs), rig.spy.calls.Load()-before)
	})
}

// scenarioTelemetry waits for the printer's own cadence to reach the state tree.
func scenarioTelemetry(t *testing.T, cell Cell) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		rig := newRig(t, cell)
		defer rig.stop(t)

		session := rig.connect(t)

		seen := map[string]json.RawMessage{}
		deadline := time.After(telemetryBudget)

		// Waiting for the whole idle key set rather than for a count. The keys
		// reach the tree one patch at a time in sorted order, so a test that
		// stopped at the first interesting one would pass with everything after
		// "s" missing.
		for !hasAll(seen, idleTelemetryPaths) {
			select {
			case patch, open := <-session.State():
				if !open {
					t.Fatalf("%s: the state channel closed before the printer reported its state", cell)
				}

				seen[patch.Path] = patch.Value
			case <-deadline:
				t.Fatalf("%s: %v did not all arrive in %s. %d paths did: %v",
					cell, idleTelemetryPaths, telemetryBudget, len(seen), seen)
			}
		}

		if got := string(seen["/state"]); got != `"IDLE"` {
			t.Errorf("%s: the printer reported state %s, want \"IDLE\"", cell, got)
		}

		t.Logf("observed: %s reported %d state paths, including /state = %s and /tools/0/nozzle_temp = %s",
			cell, len(seen), seen["/state"], seen["/tools/0/nozzle_temp"])
	})
}

// scenarioOfflineDrain is the queue-and-drain rule: a command offered to a
// printer that is away waits for it and completes when it returns.
func scenarioOfflineDrain(t *testing.T, cell Cell) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		rig := newRig(t, cell)
		defer rig.stop(t)

		session := rig.connect(t)
		resolved, _ := session.Capabilities()

		verb := firstArgumentFreeVerb(t, cell, resolved)

		rig.printer.GoOffline()
		synctest.Wait()

		ctx, cancel := context.WithTimeout(rig.ctx, commandBudget)
		defer cancel()

		answered := make(chan connectorapi.Result, 1)
		failed := make(chan error, 1)

		go func() {
			result, err := session.Do(ctx, connectorapi.Command{Verb: verb})
			if err != nil {
				failed <- err

				return
			}

			answered <- result
		}()

		synctest.Wait()

		select {
		case result := <-answered:
			t.Fatalf("%s: %s was answered while the printer was away: %+v", cell, verb, result)
		case err := <-failed:
			t.Fatalf("%s: %s failed while the printer was away instead of waiting: %v", cell, verb, err)
		default:
		}

		t.Logf("observed: %s is offline and %s is queued rather than failed", cell, verb)

		sessionsBefore := rig.printer.Sessions()

		rig.printer.GoOnline()
		synctest.Wait()

		select {
		case result := <-answered:
			if !result.OK {
				t.Errorf("%s: %s drained and came back refused: %s", cell, verb, result.Raw)
			}
		case err := <-failed:
			t.Fatalf("%s: %s failed after the printer came back: %v", cell, verb, err)
		case <-ctx.Done():
			t.Fatalf("%s: %s never drained after the printer came back", cell, verb)
		}

		if after := rig.printer.Sessions(); after <= sessionsBefore {
			t.Errorf("%s: the printer held %d sessions before the drain and %d after, so it never redialled",
				cell, sessionsBefore, after)
		}

		t.Logf("observed: %s redialled on session %d and the queued command completed",
			cell, rig.printer.Sessions())
	})
}

// scenarioStoragePulled is the USB stick removed while the server still holds a
// path on it. Out of space and media gone are reported at the write and nowhere
// else, so a success here is a data-loss defect rather than a cosmetic one.
func scenarioStoragePulled(t *testing.T, cell Cell) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		rig := newRig(t, cell)
		defer rig.stop(t)

		session := rig.connect(t)
		resolved, _ := session.Capabilities()

		if !resolved.Has(startPrint) {
			t.Fatalf("%s does not declare %s, so a path on a missing device cannot be sent", cell, startPrint)
		}

		ctx, cancel := context.WithTimeout(rig.ctx, commandBudget)
		defer cancel()

		command := connectorapi.Command{Verb: startPrint, Args: map[string]string{"path": "/usb/tangle.bgcode"}}

		healthy, err := session.Do(ctx, command)
		if err != nil {
			t.Fatalf("%s: a print from a healthy stick: %v", cell, err)
		}

		if !healthy.OK {
			t.Fatalf("%s: a print from a healthy stick was refused: %s", cell, healthy.Raw)
		}

		if !rig.printer.RemoveStorage("/usb") {
			t.Fatalf("%s: the printer reported no /usb to pull", cell)
		}

		pulled, err := session.Do(ctx, command)
		if err != nil {
			t.Fatalf("%s: a print from a stick that is gone: %v", cell, err)
		}

		if pulled.OK {
			t.Fatalf("%s: a print from a stick that is gone was reported as a success", cell)
		}

		if !bytes.Contains(pulled.Raw, []byte(simulator.ReasonMediaNotInserted)) {
			t.Errorf("%s: the refusal does not say why: %s", cell, pulled.Raw)
		}

		t.Logf("observed: %s refused the print with %s", cell, pulled.Raw)
	})
}

// scenarioFullStorage is the full disk. It is the asymmetry that makes out of
// space hard to find: the device still serves a read and refuses only the write,
// and a caller that only ever reads sees a healthy printer.
func scenarioFullStorage(t *testing.T, cell Cell) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		rig := newRig(t, cell)
		defer rig.stop(t)

		session := rig.connect(t)
		resolved, _ := session.Capabilities()

		for _, verb := range []connectorapi.Capability{startPrint, createFolder} {
			if !resolved.Has(verb) {
				t.Fatalf("%s does not declare %s, so the full-disk fault cannot be reached", cell, verb)
			}
		}

		ctx, cancel := context.WithTimeout(rig.ctx, commandBudget)
		defer cancel()

		if !rig.printer.SetFreeSpace("/usb", 0) {
			t.Fatalf("%s: the printer reported no /usb to fill", cell)
		}

		write, err := session.Do(ctx, connectorapi.Command{
			Verb: createFolder, Args: map[string]string{"path": "/usb/models"},
		})
		if err != nil {
			t.Fatalf("%s: a folder on a full stick: %v", cell, err)
		}

		if write.OK {
			t.Fatalf("%s: a folder was created on a stick with no space left", cell)
		}

		if !bytes.Contains(write.Raw, []byte(simulator.ReasonStorageFull)) {
			t.Errorf("%s: the refusal does not say the stick is full: %s", cell, write.Raw)
		}

		read, err := session.Do(ctx, connectorapi.Command{
			Verb: startPrint, Args: map[string]string{"path": "/usb/tangle.bgcode"},
		})
		if err != nil {
			t.Fatalf("%s: a print from a full stick: %v", cell, err)
		}

		if !read.OK {
			t.Errorf("%s: a full stick refused a read as well as a write: %s", cell, read.Raw)
		}

		t.Logf("observed: %s refused the write with %s and served the read", cell, write.Raw)
	})
}

// scenarioSlowPrinter makes a host-side deadline reachable. A command with no
// deadline of its own lets one unresponsive printer hold a caller forever, and
// the session has to survive the refusal.
func scenarioSlowPrinter(t *testing.T, cell Cell) {
	t.Helper()

	// One rig per frame kind, because a printer holding one command holds the
	// next one behind it: the two would have to share a deadline and the second
	// would pass whether its own frame kind was delayed or not.
	//
	// Both kinds, because nineteen of the fifty-seven verbs leave as gcode and
	// the rest as a JSON command, and a delay that
	// reached one and not the other would leave a third of the command surface
	// unable to make a host-side timeout reachable.
	for _, slow := range []connectorapi.Capability{firstArgumentFreeVerbOf(t, cell), sendInfo} {
		synctest.Test(t, func(t *testing.T) {
			rig := newRig(t, cell)
			defer rig.stop(t)

			session := rig.connect(t)

			rig.printer.SetResponseDelay(slowPrinterDelay)

			ctx, cancel := context.WithTimeout(rig.ctx, slowPrinterBudget)
			defer cancel()

			started := time.Now()

			if _, err := session.Do(ctx, connectorapi.Command{Verb: slow}); err == nil {
				t.Fatalf("%s: %s answered inside %s, and the printer is holding every command for %s",
					cell, slow, slowPrinterBudget, slowPrinterDelay)
			}

			if waited := time.Since(started); waited < slowPrinterBudget {
				t.Errorf("%s: %s gave up after %s, before its own deadline of %s",
					cell, slow, waited, slowPrinterBudget)
			}

			select {
			case _, open := <-session.State():
				if !open {
					t.Fatalf("%s: the session died because one command timed out", cell)
				}
			default:
			}

			t.Logf("observed: %s gave up on %s after %s and the session is still open",
				cell, slow, slowPrinterBudget)
		})
	}
}

// firstArgumentFreeVerbOf is the verb a cell's own capability set leads with,
// resolved without a session so a scenario can name it before building one.
func firstArgumentFreeVerbOf(t *testing.T, cell Cell) connectorapi.Capability {
	t.Helper()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("load the profiles: %v", err)
	}

	baseline := xbuddy.NewDriver(xbuddy.ModeNone, 1, 0, "", nil).Baseline()

	matched, ok := registry.Match(cell.Model, connectorapi.NewFirmwareLabel(cell.Firmware))
	if !ok {
		t.Fatalf("no profile covers %s", cell)
	}

	return firstArgumentFreeVerb(t, cell, baseline.Intersect(matched.Capabilities))
}

// scenarioOfflineGrace is the other half of the offline rule. A session outlives
// an absence so its queue does, and an operator who wants a printer reaped after
// a while says how long. Nothing else ends a session: the contract has no "the
// device went away but may come back", so an absence is a session with no socket
// under it.
func scenarioOfflineGrace(t *testing.T, cell Cell) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		rig := newRig(t, cell, withGrace(offlineGrace))
		defer rig.stop(t)

		session := rig.connect(t)

		rig.printer.GoOffline()
		synctest.Wait()

		select {
		case _, open := <-session.State():
			if !open {
				t.Fatalf("%s: the session ended the moment the printer went away, before its grace", cell)
			}
		default:
		}

		time.Sleep(2 * offlineGrace)
		synctest.Wait()

		for {
			if _, open := <-session.State(); !open {
				break
			}
		}

		t.Logf("observed: %s kept its session through the grace of %s and lost it after", cell, offlineGrace)
	})
}

// scenarioUnmatchedFirmware is the printer running a lineup this build has never
// seen. It is served on a reduced capability set, never refused: a refusal here
// is a printer nobody can reach until the profile data ships.
func scenarioUnmatchedFirmware(t *testing.T, cell Cell) {
	t.Helper()

	synctest.Test(t, func(t *testing.T) {
		unknown := Cell{Model: cell.Model, Firmware: unmatchedFirmware, Band: cell.Band, Source: "unmatched"}

		rig := newRig(t, unknown)
		defer rig.stop(t)

		session := rig.connect(t)
		resolution := session.Resolution()

		if resolution.ProfileMatched {
			t.Fatalf("%s reported firmware %s and a profile claimed to cover it",
				cell.Model, unmatchedFirmware)
		}

		if len(resolution.Capabilities) == 0 {
			t.Fatalf("%s on an unknown firmware resolved to nothing, so it is a printer nobody can command",
				cell.Model)
		}

		resolved, _ := session.Capabilities()
		verb := firstArgumentFreeVerb(t, unknown, resolved)

		ctx, cancel := context.WithTimeout(rig.ctx, commandBudget)
		defer cancel()

		result, err := session.Do(ctx, connectorapi.Command{Verb: verb})
		if err != nil {
			t.Fatalf("%s on an unknown firmware refused %s: %v", cell.Model, verb, err)
		}

		if !result.OK {
			t.Errorf("%s on an unknown firmware answered %s with a refusal: %s", cell.Model, verb, result.Raw)
		}

		t.Logf("observed: %s on firmware %s resolved %d capabilities from the driver baseline and answered %s",
			cell.Model, unmatchedFirmware, len(resolution.Capabilities), verb)
	})
}

// idleTelemetryPaths is what an idle printer of any model puts in the
// addressable state tree. The model-specific blocks are deliberately not here:
// earlier research records that neither the profile data nor the driver
// names them, so a test asserting one would be asserting an invention.
//
//nolint:gochecknoglobals // a fixture, read-only after init.
var idleTelemetryPaths = []string{
	"/state",
	"/tools/0/nozzle_temp",
	"/tools/0/nozzle_target_temp",
	"/temperature/bed",
	"/temperature/bed_target",
	"/axis/x",
	"/axis/y",
	"/axis/z",
	"/speed",
	"/flow",
	"/tools/0/material_package_instance/package/material/abbreviation",
}

// hasAll reports whether every path arrived.
func hasAll(seen map[string]json.RawMessage, want []string) bool {
	for _, path := range want {
		if _, present := seen[path]; !present {
			return false
		}
	}

	return true
}

// argumentFreeVerbs are the verbs in a set that declare no arguments, sorted.
//
// Only those: an argument's value is not in the profile data, so inventing one
// would be the test asserting against a number it made up.
func argumentFreeVerbs(set connectorapi.Set) []connectorapi.Capability {
	arguments := map[connectorapi.Capability]int{}

	var verbs []connectorapi.Capability

	for _, key := range set.Keys() {
		if _, isArgument := key.Argument(); isArgument {
			arguments[key.Verb()]++

			continue
		}

		// A key outside the command namespace answers "can you", never "do
		// it": core.feature.* is not invokable through the command surface, so
		// sending it would assert a refusal the vocabulary requires (ADR-20260826-ezr
		// rule 3).
		if !strings.HasPrefix(key.String(), connectorapi.CorePrefix+"command.") {
			continue
		}

		verbs = append(verbs, key)
	}

	free := make([]connectorapi.Capability, 0, len(verbs))

	for _, verb := range verbs {
		if arguments[verb] == 0 {
			free = append(free, verb)
		}
	}

	slices.Sort(free)

	return free
}

// verbsOutside are the verbs the first set declares and the second does not.
func verbsOutside(have, lacks connectorapi.Set) []connectorapi.Capability {
	var out []connectorapi.Capability

	for _, key := range have.Keys() {
		if _, isArgument := key.Argument(); isArgument {
			continue
		}

		if !lacks.Has(key) {
			out = append(out, key)
		}
	}

	slices.Sort(out)

	return out
}

// firstArgumentFreeVerb is the verb the single-command scenarios use. It fails
// rather than skips when a cell has none, because a cell that ran no command is
// a cell that proved nothing.
func firstArgumentFreeVerb(t *testing.T, cell Cell, set connectorapi.Set) connectorapi.Capability {
	t.Helper()

	free := argumentFreeVerbs(set)
	if len(free) == 0 {
		t.Fatalf("%s declares no verb without arguments", cell)
	}

	return free[0]
}

// summarise is one resolution in the shape the goldens carry.
func summarise(resolution capability.Resolution) golden {
	verbs := 0

	for _, key := range resolution.Capabilities.Keys() {
		if _, isArgument := key.Argument(); !isArgument {
			verbs++
		}
	}

	return golden{
		Digest:         resolution.Digest,
		Capabilities:   len(resolution.Capabilities),
		Verbs:          verbs,
		ProfileMatched: resolution.ProfileMatched,
	}
}

// committedGoldens reads the golden file once for the whole run.
//
//nolint:gochecknoglobals // a once-value over a file, read-only after init.
var committedGoldens = sync.OnceValues(func() (map[string]golden, error) {
	data, err := os.ReadFile(goldenFile)
	if err != nil {
		return nil, err
	}

	var file goldenFileContent
	if err = json.Unmarshal(data, &file); err != nil {
		return nil, err
	}

	return file.Cells, nil
})

// goldenFor resolves one cell the way the host does, without a wire. It is what
// the golden file is generated from, and scenarioCapabilities is what ties it
// to what a real session resolves.
func goldenFor(t *testing.T, cell Cell) golden {
	t.Helper()

	registry, err := profile.Builtin()
	if err != nil {
		t.Fatalf("load the profiles: %v", err)
	}

	baseline := xbuddy.NewDriver(xbuddy.ModeNone, 1, 0, "", nil).Baseline()

	identity := connectorapi.Identity{
		Model:    cell.Model,
		Firmware: connectorapi.NewFirmwareLabel(cell.Firmware),
		Serial:   "SIM-" + strings.ToUpper(cell.Model),
	}

	tier := registry.Coverage()
	if matched, ok := registry.Match(cell.Model, identity.Firmware); ok {
		tier = matched.Capabilities
	}

	// The probe is what the driver reports about the session it built, which is
	// its baseline narrowed to the tier the host admitted it on.
	probe := baseline.Intersect(tier)

	resolver := capability.NewResolver(registry, slog.New(slog.DiscardHandler))

	return summarise(resolver.Resolve(t.Context(), baseline, identity, probe, 1))
}
