package xbuddy

import (
	"testing"
)

// The paths the two frames below move. They are read back off the state tree,
// so the test names the addresses a caller reads rather than the wire keys.
const (
	nozzlePath = "/tools/0/nozzle_temp"
	bedPath    = "/temperature/bed"
)

// A telemetry key a frame leaves out changes nothing on the state tree.
//
// connectorapi.StatePatch gives a null value one meaning: remove the node, so
// that a printer that stopped reporting a chamber is told apart from a chamber
// reading zero. A driver that turned every absent key into a null would spend
// that meaning on the ordinary case, because this dialect's frames are sparse
// and a printer reports what changed.
//
// The shipping connector records the same rule from the other side
// (legacy/connector/xbuddy/connection.go:820-860): reading absence as a cleared
// value made a dialog id flap on alternate frames, and queued a SEND_STATE_INFO
// on every flap.
//
// It runs against a real session over a real socket, because the property is
// about what leaves the session and not about the mapping table.
func TestATelemetryKeyAFrameLeavesOutClearsNothing(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"state":"PRINTING","temp_nozzle":215,"temp_bed":60}`)
	printer.awaitDialogTree(t, map[string]string{nozzlePath: "215", bedPath: "60"})

	written := printer.dialogPatchCount(bedPath)
	if written != 1 {
		t.Fatalf("the first frame produced %d patches at %s, want 1: this case measures what a "+
			"second frame does to a value the first one set", written, bedPath)
	}

	// The same printer, still printing, reporting only what moved. That is the
	// ordinary shape of a frame in this dialect and not an edge case.
	printer.text(t, `{"state":"PRINTING","temp_nozzle":216}`)

	// A third frame is the barrier. Patches reach one reader in order, so
	// waiting for this frame's nozzle proves every patch the second frame
	// produced has already been taken. Asserting straight after the second
	// frame would stop reading at its nozzle and never see what followed.
	printer.text(t, `{"state":"PRINTING","temp_nozzle":217}`)
	printer.awaitDialogTree(t, map[string]string{nozzlePath: "217"})

	if held := printer.dialogValue(bedPath); held != "60" {
		t.Errorf("%s reads %s after a frame that did not mention the bed, and it read 60 before. "+
			"An absent key is not a cleared one, and null is how a node is removed", bedPath, held)
	}

	if after := printer.dialogPatchCount(bedPath); after != written {
		t.Errorf("%d patches arrived at %s and one frame carried a bed temperature, so a frame that "+
			"said nothing about the bed wrote to it anyway", after, bedPath)
	}

	t.Logf("observed: %s stands at %s after %d patch, and %s moved to %s",
		bedPath, printer.dialogValue(bedPath), printer.dialogPatchCount(bedPath),
		nozzlePath, printer.dialogValue(nozzlePath))
}

// A flow of zero removes the flow. Buddy reads the flow off the tool it has
// selected and sends 0 when none is, so a 0 is no reading, and a page that took
// it for one drew a change of -100% beside a slider clamped at its floor
// (CL-229). A real flow after it comes back as it was sent.
func TestAFlowOfZeroIsNoReading(t *testing.T) {
	t.Parallel()

	const flowPath = "/flow"

	printer := standHostile(t)

	printer.text(t, `{"state":"IDLE","flow":100}`)
	printer.awaitDialogTree(t, map[string]string{flowPath: "100"})

	printer.text(t, `{"state":"IDLE","flow":0}`)
	printer.awaitDialogTree(t, map[string]string{flowPath: "null"})

	printer.text(t, `{"state":"IDLE","flow":95}`)
	printer.awaitDialogTree(t, map[string]string{flowPath: "95"})
}
