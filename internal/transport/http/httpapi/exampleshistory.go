package httpapi

import (
	"encoding/json"
	"time"
)

// exampleCommandElapsed is how long the example invocation took, which is what
// a beep on a machine on the same network costs.
const exampleCommandElapsed = 34 * time.Millisecond

// The history examples. They are built from the same printer, the same instant
// and the same presence payload the event stream example carries, so a reader
// who compares the two sees one frame and the row it became.

// exampleFactPage is one page of stored events.
func exampleFactPage() HistoryFactPage {
	return HistoryFactPage{
		Items: []HistoryFact{{
			At:       exampleTime,
			Origin:   "device:" + examplePrinterID,
			Type:     "com.prusa.connect.device.presence.changed",
			Sequence: 412,
			Epoch:    "01K3Q7W0",
			Payload:  json.RawMessage(`{"device_id":"` + examplePrinterID + `","driver":"xbuddy","connected":false}`),
		}},
		Cursor:     "",
		Incomplete: false,
	}
}

// exampleSamplePage is one page of stored readings, named by the state paths
// the same printer publishes telemetry under.
//
// The third row is a word rather than a number, because that is the half of the
// shape a reader has to see to write a client for it: two members, exactly one
// of them present per row.
func exampleSamplePage() HistorySamplePage {
	var (
		nozzle = 214.8
		bed    = 59.9
		state  = "PRINTING"
	)

	return HistorySamplePage{
		Items: []HistorySample{
			{At: exampleTime, Origin: "device:" + examplePrinterID, Metric: "/tools/0/nozzle_temp", Value: &nozzle},
			{At: exampleTime, Origin: "device:" + examplePrinterID, Metric: "/temperature/bed", Value: &bed},
			{At: exampleTime, Origin: "device:" + examplePrinterID, Metric: "/state", Text: &state},
		},
		Cursor:     "",
		Incomplete: false,
	}
}

// exampleCommand is one folded invocation: the beep a reader can send from the
// device page, as the record holds it once it has settled.
//
// The audit members are here because the example is what a caller holding
// view_audit reads. A caller without it reads the same row with those three
// absent, which the field documentation says and an example cannot show twice.
func exampleCommand() HistoryCommand {
	return HistoryCommand{
		ID:         exampleCommandID,
		Origin:     "device:" + examplePrinterID,
		Verb:       "core.command.beep",
		Requested:  exampleTime,
		Authorized: exampleTime,
		Settled:    exampleTime.Add(exampleCommandElapsed),
		Status:     "succeeded",
		OK:         true,
		ElapsedMS:  exampleCommandElapsed.Milliseconds(),
		AuthType:   "user",
		AuthID:     "operator",
	}
}

// exampleCommandPage is one page of one device's command record.
func exampleCommandPage() HistoryCommandPage {
	return HistoryCommandPage{Items: []HistoryCommand{exampleCommand()}, Cursor: ""}
}
