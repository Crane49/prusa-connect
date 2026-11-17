package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// What the old backlog asks of one call: the file reaches the machine and the
// print starts, in the order a person would do it by hand, and a file already
// on the machine costs no transfer at all.

// printResult is the answer the print route writes, read back the way a page
// reads it.
type printResult struct {
	Path     string          `json:"path"`
	Transfer *DeviceTransfer `json:"transfer"`
	Command  PrinterResult   `json:"command"`
}

// fleetThatPrints is the printer both halves of the route act on.
func fleetThatPrints() *fakeFleet {
	fleet := fleetWithFiles()
	fleet.result = PrinterResult{OK: true, CommandID: "01COMMAND"}

	return fleet
}

func TestPrintingFromAStorageTransfersTheFileAndThenStartsIt(t *testing.T) {
	t.Parallel()

	fleet := fleetThatPrints()

	rec := call(t, serverWithFleet(t, fleet).Handler(),
		http.MethodPost, "/api/v1/devices/pdeadbeef/print",
		`{"storage":"local","source":"models/part.bgcode","path":"usb/part.bgcode"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body printResult

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "usb/part.bgcode", body.Path)
	require.NotNil(t, body.Transfer, "a file from a storage is relayed, so the answer carries the transfer")
	require.Equal(t, "01TRANSFER", body.Transfer.TransferID)
	require.True(t, body.Command.OK)
	require.Equal(t, "01COMMAND", body.Command.CommandID)

	require.Len(t, fleet.sent, 1)
	require.Equal(t, DeviceUpload{
		Storage: "local", Source: "models/part.bgcode", Path: "usb/part.bgcode",
	}, fleet.sent[0])

	require.Len(t, fleet.received, 1)
	require.Equal(t, StartPrintVerb, fleet.received[0].Verb)
	require.Equal(t, map[string]string{StartPrintPathArgument: "usb/part.bgcode"}, fleet.received[0].Args)

	t.Logf("observed: %s", rec.Body.String())
}

func TestPrintingAFileTheMachineHoldsSendsNoTransfer(t *testing.T) {
	t.Parallel()

	fleet := fleetThatPrints()

	rec := call(t, serverWithFleet(t, fleet).Handler(),
		http.MethodPost, "/api/v1/devices/pdeadbeef/print", `{"path":"usb/part.bgcode"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body printResult

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Nil(t, body.Transfer, "nothing was relayed, so there is no transfer to report")
	require.Empty(t, fleet.sent)
	require.Len(t, fleet.received, 1)
	require.Equal(t, StartPrintVerb, fleet.received[0].Verb)
}

// A printer that says no is a 200 whose body says no, the way a command is.
// The transfer that already happened is reported beside it, because the file is
// on the machine whatever the printer made of the start.
func TestAPrinterRefusingTheStartStillReportsTheTransfer(t *testing.T) {
	t.Parallel()

	fleet := fleetThatPrints()
	fleet.result = PrinterResult{OK: false, MachineReason: "Device is busy"}

	rec := call(t, serverWithFleet(t, fleet).Handler(),
		http.MethodPost, "/api/v1/devices/pdeadbeef/print",
		`{"storage":"local","source":"models/part.bgcode","path":"usb/part.bgcode"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body printResult

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.False(t, body.Command.OK)
	require.Equal(t, "Device is busy", body.Command.MachineReason)
	require.NotNil(t, body.Transfer)
}

// Half a source is refused before anything is sent, so a typo cannot start a
// print of a file the caller did not name.
func TestHalfASourceIsRefusedBeforeAnythingIsSent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{name: "a storage with no source", body: `{"storage":"local","path":"usb/part.bgcode"}`},
		{name: "a source with no storage", body: `{"source":"models/part.bgcode","path":"usb/part.bgcode"}`},
		{name: "no path at all", body: `{"storage":"local","source":"models/part.bgcode","path":" "}`},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fleet := fleetThatPrints()

			rec := call(t, serverWithFleet(t, fleet).Handler(),
				http.MethodPost, "/api/v1/devices/pdeadbeef/print", tt.body)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
			require.Empty(t, fleet.sent)
			require.Empty(t, fleet.received)
		})
	}
}

// A printer this server has never been dialled by is refused on the transfer,
// so no command is sent after it.
func TestPrintingAtAnUnknownDeviceSendsNoCommand(t *testing.T) {
	t.Parallel()

	fleet := fleetThatPrints()

	rec := call(t, serverWithFleet(t, fleet).Handler(),
		http.MethodPost, "/api/v1/devices/pnothing/print",
		`{"storage":"local","source":"models/part.bgcode","path":"usb/part.bgcode"}`)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.Empty(t, fleet.received)
}
