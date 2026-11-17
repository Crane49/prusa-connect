package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// oneMebibyte is the xBuddy frame limit, so a value this wide is one a printer
// on the shipping dialect can really send.
const oneMebibyte = 1 << 20

// wideValue is a JSON string a driver hands over, of about the given size. It
// is the shape a dialog title arrives in: one encoded string at one path.
func wideValue(size int) string {
	return `"` + strings.Repeat("A", size) + `"`
}

// TestAnOversizedTelemetryValueIsClippedInTheStateTree is ADR-20260829-jdq rules 2 and
// 3 over the state tree, which is the reader every subscriber reads.
//
// The value is a mebibyte because that is what the xBuddy frame limit admits,
// so this is a frame the shipping dialect accepts rather than an invented one.
func TestAnOversizedTelemetryValueIsClippedInTheStateTree(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	session := arriveSpeaking(t, plane, "SIM-WIDE-VALUE")

	entry, live := plane.entry(plane.idOf(session.identity))
	require.True(t, live)

	value := wideValue(oneMebibyte)
	entry.record(connectorapi.StatePatch{Path: "/dialog/title", Value: json.RawMessage(value)})

	held := entry.snapshot()["/dialog/title"]

	t.Logf("observed: the driver sent %d bytes at /dialog/title and the tree kept %d",
		len(value), len(held))

	require.NotEmpty(t, held, "the value was dropped rather than clipped")
	require.Less(t, len(held), len(value),
		"the state tree kept the whole of a value the printer chose")
	require.LessOrEqual(t, len(held), machinetext.MaxBytes+len(machinetext.Marker)+2)
	require.Contains(t, string(held), machinetext.Marker,
		"the value was cut and the reader cannot tell it was cut")
}

// TestAnOversizedMachineReasonIsClippedInTheCommandAnswer is the same two rules
// over the HTTP command answer, which every caller of the command surface and
// of the raw gcode surface reads.
func TestAnOversizedMachineReasonIsClippedInTheCommandAnswer(t *testing.T) {
	t.Parallel()

	reason := strings.Repeat("B", oneMebibyte)

	plane := newPlane(t)
	id := arriveTalking(t, plane, "SIM-WIDE-REASON", connectorapi.Result{
		OK: false, MachineReason: reason,
	})

	answer, err := plane.Command(t.Context(), "user:"+config.DefaultPrincipal, id,
		httpapi.PrinterCommand{Verb: nozzleVerb})
	require.NoError(t, err)

	t.Logf("observed: the printer refused with %d bytes and the answer carried %d",
		len(reason), len(answer.MachineReason))

	require.False(t, answer.OK)
	require.Less(t, len(answer.MachineReason), len(reason),
		"the caller was handed the whole of a refusal the printer chose")
	require.LessOrEqual(t, len(answer.MachineReason), machinetext.MaxBytes+len(machinetext.Marker))
	require.True(t, strings.HasSuffix(answer.MachineReason, machinetext.Marker),
		"the refusal was cut and the caller cannot tell it was cut")
}

// TestATreeOverItsBudgetRefusesANewPathAndStillUpdatesAnExistingOne is
// ADR-20260829-jdq rule 5. The cheap way to fill a tree is paths and not bytes: ten
// thousand paths of three bytes each cost a driver almost nothing to send.
//
// The second half is the half that matters. A driver that filled its own budget
// must still report the telemetry it was already reporting, or a printer locks
// itself out of /state by describing itself too fully once.
func TestATreeOverItsBudgetRefusesANewPathAndStillUpdatesAnExistingOne(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	session := arriveSpeaking(t, plane, "SIM-WIDE-TREE")

	entry, live := plane.entry(plane.idOf(session.identity))
	require.True(t, live)

	entry.record(connectorapi.StatePatch{Path: "/state", Value: json.RawMessage(`"IDLE"`)})

	// One value under the per-value bound, at a fresh path each time, until the
	// tree budget is spent. Nothing here is an update.
	filler := wideValue(machinetext.MaxBytes - 1)
	for i := range defaultTreeBudget/len(filler) + 2 {
		entry.record(connectorapi.StatePatch{
			Path:  "/filler/" + strconv.Itoa(i),
			Value: json.RawMessage(filler),
		})
	}

	clipped, refused := entry.textCounts()

	t.Logf("observed: the tree holds %d paths and %d bytes, refused %d paths and clipped %d values",
		len(entry.snapshot()), entry.treeBytes(), refused, clipped)

	require.Positive(t, refused, "a driver filled the tree past its budget and nothing refused a path")
	require.LessOrEqual(t, entry.treeBytes(), defaultTreeBudget+machinetext.MaxBytes,
		"the tree grew past the budget it was given")
	require.Zero(t, clipped, "nothing sent here was over the per-value bound")

	// The path that was already there still takes a new reading.
	entry.record(connectorapi.StatePatch{Path: "/state", Value: json.RawMessage(`"PRINTING"`)})
	require.Equal(t, `"PRINTING"`, string(entry.snapshot()["/state"]),
		"a full tree stopped a printer updating telemetry it was already reporting")
}

// TestAClippedValueIsCounted is ADR-20260829-jdq rule 6. The first measurement of this
// leak was made by writing a test, and an operator running the build had no
// reading that would have shown it.
func TestAClippedValueIsCounted(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	session := arriveSpeaking(t, plane, "SIM-COUNTED")

	entry, live := plane.entry(plane.idOf(session.identity))
	require.True(t, live)

	clipped, _ := entry.textCounts()
	require.Zero(t, clipped)

	// Sent down the driver's own channel rather than recorded directly, so the
	// count is one the pump made on the frame path.
	session.patches <- connectorapi.StatePatch{
		Path:  "/dialog/text",
		Value: json.RawMessage(wideValue(oneMebibyte)),
	}

	require.Eventually(t, func() bool {
		clipped, _ = entry.textCounts()

		return clipped > 0
	}, time.Second, time.Millisecond, "a value was clipped and nothing counted it")
}

// TestARemovalOfANodeTheTreeDoesNotHoldReachesNoSubscriber is the rule a
// driver relies on to remove a tool it cannot know is there: a removal that
// changes nothing is not kept, so it is not published either.
func TestARemovalOfANodeTheTreeDoesNotHoldReachesNoSubscriber(t *testing.T) {
	t.Parallel()

	entry := &device{state: map[string]json.RawMessage{}, budget: defaultTreeBudget}

	_, kept := entry.record(connectorapi.StatePatch{Path: "/tools/3/enabled", Value: json.RawMessage("null")})
	require.False(t, kept, "a removal of a node nobody wrote")

	_, kept = entry.record(connectorapi.StatePatch{Path: "/tools/3/enabled", Value: json.RawMessage(`false`)})
	require.True(t, kept)

	_, kept = entry.record(connectorapi.StatePatch{Path: "/tools/3/enabled", Value: json.RawMessage("null")})
	require.True(t, kept, "a removal of a node the tree holds")
	require.NotContains(t, entry.state, "/tools/3/enabled")
}
