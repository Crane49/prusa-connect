package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/deviceissue"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// A module reads a device's issues and the dialog it waits on from the record
// the contract publishes, in the shape the listing carries them.
func TestAModuleReadsWhatIsWrongAndTheDialog(t *testing.T) {
	t.Parallel()

	listed := httpapi.Device{
		ID: "printer-1",
		State: httpapi.DeviceState{
			deviceissue.DialogIDPath:      json.RawMessage(`42`),
			deviceissue.DialogCodePath:    json.RawMessage(`12201`),
			deviceissue.DialogTitlePath:   json.RawMessage(`"Filament runout"`),
			deviceissue.DialogTextPath:    json.RawMessage(`"Load filament to continue."`),
			deviceissue.DialogButtonsPath: json.RawMessage(`["Load","Unload"]`),
		},
		Issues: []httpapi.DeviceIssue{issueOf(deviceissue.ReasonAwaitingOperator, "Filament runout")},
	}

	read := moduleDevice(listed)

	require.Equal(t, []deviceapi.Issue{{
		Reason: "awaiting_operator", Severity: "error", Detail: "Filament runout",
	}}, read.Issues)
	require.NotNil(t, read.Dialog)
	require.JSONEq(t, `42`, string(read.Dialog.ID), "the id is not the one dialog_action takes back")
	require.Equal(t, "12201", read.Dialog.Code, "a numeric firmware code did not read as text")
	require.Equal(t, "Filament runout", read.Dialog.Title)
	require.Equal(t, "Load filament to continue.", read.Dialog.Text)
	require.Equal(t, []string{"Load", "Unload"}, read.Dialog.Buttons)

	// A printer that has gone away still holds the last tree it reported, and
	// a dialog in it is not work waiting for anybody.
	listed.Departed = &httpapi.DeviceDeparture{Reason: "closed"}
	require.Nil(t, moduleDevice(listed).Dialog, "a departed printer reads as waiting at a dialog")

	require.Nil(t, moduleDevice(httpapi.Device{ID: "quiet"}).Dialog)
	require.Empty(t, moduleDevice(httpapi.Device{ID: "quiet"}).Issues)
}

// A dialog raised one path at a time reaches the bus as one frame carrying
// what it settled on, and clearing it as one more, so a module that sends a
// message per frame sends one per change.
func TestADialogIsOneIssuesFrameAndItsEndIsAnother(t *testing.T) {
	t.Parallel()

	plane, bus, _ := planeBusAndGraph(t)
	session := arriveSpeaking(t, plane, "SIM-ISSUES")
	id := plane.idOf(session.identity)

	session.report(t, plane, deviceissue.DialogIDPath, `7`)
	session.report(t, plane, deviceissue.DialogTitlePath, `"Filament runout"`)
	session.report(t, plane, deviceissue.DialogButtonsPath, `["Continue"]`)

	raised := bus.awaitType(t, event.TypeDeviceIssuesChanged, 1)
	t.Logf("observed: %s", raised[0].Data)

	var change struct {
		Printer string                `json:"device_id"`
		Issues  []httpapi.DeviceIssue `json:"issues"`
		Dialog  *deviceapi.Dialog     `json:"dialog"`
	}

	require.NoError(t, json.Unmarshal(raised[0].Data, &change))
	require.Equal(t, id, change.Printer)
	require.Equal(t, []httpapi.DeviceIssue{issueOf(deviceissue.ReasonAwaitingOperator, "Filament runout")},
		change.Issues, "the frame does not carry the dialog's title as the machine said it")
	require.NotNil(t, change.Dialog)
	require.Equal(t, []string{"Continue"}, change.Dialog.Buttons)

	session.report(t, plane, deviceissue.DialogTitlePath, "null")
	session.report(t, plane, deviceissue.DialogButtonsPath, "null")
	session.report(t, plane, deviceissue.DialogIDPath, "null")

	cleared := bus.awaitType(t, event.TypeDeviceIssuesChanged, 2)

	var after struct {
		Issues []httpapi.DeviceIssue `json:"issues"`
		Dialog *deviceapi.Dialog     `json:"dialog"`
	}

	require.NoError(t, json.Unmarshal(cleared[1].Data, &after))
	require.Empty(t, after.Issues, "the dialog closed and the frame still names an issue")
	require.Nil(t, after.Dialog, "the dialog closed and the frame still carries it")

	// Nothing moved after that, so nothing else is published.
	time.Sleep(2 * issueSettle)
	require.Len(t, bus.ofType(event.TypeDeviceIssuesChanged), 2,
		"a dialog raised and cleared published more than two frames")
}

// ofType returns the frames of one type seen so far, in arrival order.
func (r *busReader) ofType(eventType string) []event.Envelope {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []event.Envelope

	for _, envelope := range r.frames {
		if envelope.Type == eventType {
			out = append(out, envelope)
		}
	}

	return out
}

// awaitType waits for at least want frames of one type and returns them.
func (r *busReader) awaitType(t *testing.T, eventType string, want int) []event.Envelope {
	t.Helper()

	require.Eventually(t, func() bool { return len(r.ofType(eventType)) >= want },
		5*time.Second, 10*time.Millisecond, "fewer than %d %s frames arrived", want, eventType)

	return r.ofType(eventType)
}
