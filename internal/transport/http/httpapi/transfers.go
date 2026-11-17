package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// runningTransfersPath lists the transfers still running across every printer.
// It is not under a device: a page that shows every file on its way would
// otherwise read each printer in turn.
const runningTransfersPath = "/api/v1/transfers"

// RunningTransfer is one transfer still running, with the printer it goes to
// and what the caller may do about it.
type RunningTransfer struct {
	DeviceTransfer

	DeviceID    string `json:"device_id"            doc:"The printer the file is going to."`
	PrinterName string `json:"printer_name"         doc:"The printer's name as the printer listing shows it."`
	Mine        bool   `json:"mine"                 doc:"True when the caller's own request began the transfer, from any browser or client."`
	StartedBy   string `json:"started_by,omitempty" doc:"The principal whose request began the transfer, such as user:ada. Present only for a caller holding view_audit on the printer, because who asked is audit."`
	CanStop     bool   `json:"can_stop"             doc:"True when the caller holds operate on the printer, which is what the stop route takes."`
}

type listRunningTransfersOutput struct {
	Body struct {
		Items []RunningTransfer `json:"items" doc:"Every transfer still running on a printer the caller may observe, newest first."`
	}
}

// registerRunningTransfers publishes the transfers running on every printer
// the caller may read.
func registerRunningTransfers(api huma.API, fleet DeviceFleet) {
	register(api, huma.Operation{
		OperationID: "list-running-transfers",
		Method:      http.MethodGet,
		Path:        runningTransfersPath,
		Summary:     "The files on their way to any printer",
		Description: "Every transfer that is queued or sending, on every printer the caller may observe, " +
			"newest first, whoever began it. A printer the caller cannot observe adds nothing. " +
			"The same transfers are on the event stream as they move, so a caller keeps the list " +
			"current by subscribing to the three device.transfer types and reads this again only " +
			"for a transfer it has not seen.",
		Tags: []string{"devices"},
	}, func(ctx context.Context, _ *struct{}) (*listRunningTransfersOutput, error) {
		items, err := fleet.RunningTransfers(ctx, principalFrom(ctx))
		if err != nil {
			return nil, err
		}

		out := &listRunningTransfersOutput{}
		out.Body.Items = items

		return out, nil
	})
}
