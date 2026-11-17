package httpapi

// The prints across the fleet: what every printer the caller may observe is
// printing and has printed, in one paged list. The device routes answer one
// printer at a time, so a page listing a farm would otherwise send one request
// per printer.

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

// jobsPath is the fleet's prints.
const jobsPath = "/api/v1/jobs"

// The two phases a page of prints can be narrowed to. A print is running while
// it has no end.
const (
	JobPhaseRunning = "running"
	JobPhaseEnded   = "ended"
)

// JobQuery is one request for a page of the fleet's prints.
type JobQuery struct {
	// DeviceID narrows the page to one device. Empty is every device the caller
	// may observe.
	DeviceID string

	// Phase narrows the page to the running prints or the ended ones. Empty is
	// both.
	Phase string

	// From and To bound when the print ended, and the zero value of either is
	// no bound on that end. Either one narrows the page to the prints that
	// have ended.
	From time.Time
	To   time.Time

	// Limit is how many rows to answer with. Zero is the server's default page
	// size, and a value above the cap is answered with the cap.
	Limit int

	// Cursor is what the previous page reported. It is minted by this server
	// and its form is not part of the contract.
	Cursor string
}

// JobPage is one page of the fleet's prints, newest start first. It has no
// total, for the reason DeviceJobPage has none.
type JobPage struct {
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Items []DeviceJob `json:"items" doc:"The prints on the devices the caller may observe, newest start first. A print with no ended is still running."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Cursor string `json:"cursor" doc:"Opaque text to send back as the cursor parameter for the next page. Empty when this page is the last one. Read nothing out of its form."`
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Limit int `json:"limit" doc:"The page size the server applied, which is the cap when a larger one was asked for."`
}

type listJobsInput struct {
	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	DeviceID string `query:"device_id" maxLength:"128" doc:"Only the prints of this device. A device the caller may not observe answers an empty page, exactly as one that has never printed does, so the answer never says whether a device exists. Omitted is every device the caller may observe." example:"9f2a4c18-6d3b-4e07-b5a1-8c0f2d7e4b93"`

	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Phase string `query:"phase" enum:"running,ended" doc:"Only the prints still on the bed, or only the prints that have ended. Omitted is both." example:"running"`

	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	From time.Time `query:"from" doc:"Only prints that ended at or after this instant, as RFC 3339. It narrows the page to the prints that have ended, so it cannot be sent with phase running. Omitted is no lower bound." example:"2026-09-01T00:00:00Z"`

	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	To time.Time `query:"to" doc:"Only prints that ended at or before this instant, as RFC 3339. It narrows the page to the prints that have ended, so it cannot be sent with phase running. Omitted is no upper bound." example:"2026-10-01T00:00:00Z"`

	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Limit int `query:"limit" minimum:"0" doc:"How many prints to return. Omitted or zero is the server's default page size, and a value above the cap is answered with the cap." example:"50"`

	//nolint:lll // the huma tag is one field's whole contract and does not wrap.
	Cursor string `query:"cursor" maxLength:"128" doc:"The cursor a previous page returned. It is minted by this server and its form is not part of this contract." example:"MTc1NjE5OTY0NTAwMDo5ZjJhNGMxOC02ZDNiLTRlMDctYjVhMS04YzBmMmQ3ZTRiOTM6NDE="`
}

type listJobsOutput struct {
	Body JobPage
}

func registerListJobs(api huma.API, fleet DeviceFleet) {
	register(api, huma.Operation{
		OperationID: "list-jobs",
		Method:      http.MethodGet,
		Path:        jobsPath,
		Summary:     "The prints across the fleet",
		Description: "A page of the prints on every device the caller may observe, running and ended, " +
			"newest start first. It is what a jobs page reads: one request for the whole farm, where " +
			"the device routes answer one printer at a time." + paragraphBreak +
			"`phase` narrows the page to the prints still on the bed or to the ones that have " +
			"ended, and `device_id` narrows it to one device. A print is running while it has no " +
			"`ended`." + paragraphBreak +
			"`from` and `to` narrow the page to the prints that ended inside a range of time, which " +
			"is what a report over the fleet reads. Either one alone bounds that end only. A " +
			"`from` after a `to` is refused, and so is either one with `phase` running, because a " +
			"running print has not ended." + paragraphBreak +
			"Paging follows the `cursor` member: send it back as the `cursor` parameter for the " +
			"next page, and stop when it comes back empty. The rows are read from the record " +
			"store, so they survive a restart of this server." + paragraphBreak +
			"It takes observe on each device, which is what the jobs read takes. The permission is " +
			"resolved inside the query, so a device the caller reaches nothing on adds no row and " +
			"moves no cursor, and naming it in `device_id` answers an empty page and not a refusal.",
		Tags:   []string{"devices"},
		Errors: []int{http.StatusBadRequest},
	}, func(ctx context.Context, in *listJobsInput) (*listJobsOutput, error) {
		if err := validJobRange(in.From, in.To); err != nil {
			return nil, err
		}

		if in.Phase == JobPhaseRunning && (!in.From.IsZero() || !in.To.IsZero()) {
			return nil, httpfault.RequestInvalid("query.phase", "ended, or omitted",
				"from and to bound when a print ended, and a running print has not ended")
		}

		page, err := fleet.ListJobs(ctx, principalFrom(ctx), JobQuery{
			DeviceID: in.DeviceID, Phase: in.Phase, From: in.From, To: in.To,
			Limit: in.Limit, Cursor: in.Cursor,
		})
		if err != nil {
			return nil, err
		}

		return &listJobsOutput{Body: page}, nil
	})
}
