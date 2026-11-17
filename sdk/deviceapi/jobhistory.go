package deviceapi

import (
	"context"
	"fmt"
	"time"
)

// MaxCursorBytes is the longest cursor. The host creates the cursor, and a
// module sends it back unchanged.
const MaxCursorBytes = 256

// JobHistoryParams is the parameters of MethodJobHistory. ID is one device.
// An empty ID means every device the principal may observe. From and To bound
// the time the print ended, and a zero value sets no bound. Cursor is the
// cursor of the previous page, and empty for the first page.
type JobHistoryParams struct {
	// Principal is who the call is made for, and empty for the module's own
	// work.
	Principal string `json:"principal,omitempty"`

	ID string `json:"id,omitempty"`

	From time.Time `json:"from,omitzero"`
	To   time.Time `json:"to,omitzero"`

	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// Validate checks one history read against the limits of this contract. The
// client and the host both call it. An empty ID is valid.
func (p JobHistoryParams) Validate() error {
	if err := validPrincipal(p.Principal); err != nil {
		return err
	}

	if p.ID != "" {
		if err := validID(p.ID); err != nil {
			return err
		}
	}

	switch {
	case len(p.Cursor) > MaxCursorBytes:
		return fmt.Errorf("%w: the cursor is %d bytes and the limit is %d",
			ErrInvalidQuery, len(p.Cursor), MaxCursorBytes)
	case !p.From.IsZero() && !p.To.IsZero() && p.From.After(p.To):
		return fmt.Errorf("%w: the range starts at %s and ends at %s",
			ErrInvalidQuery, p.From, p.To)
	}

	return validWindow(p.Limit, 0)
}

// JobPage is the answer of MethodJobHistory: the prints that have ended,
// newest first. There is no total. Send Cursor back on the next call and stop
// when it is empty.
type JobPage struct {
	Items  []Job  `json:"items"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit"`
}

// JobHistory returns one page of the prints that have ended. It needs the
// observe permission on each device, and a page has only the prints of
// devices the principal may observe. A device the principal may not observe
// and an unknown device both report ErrNoDevice.
func (c *Client) JobHistory(ctx context.Context, params JobHistoryParams) (JobPage, error) {
	if err := params.Validate(); err != nil {
		return JobPage{}, fmt.Errorf("deviceapi: %s: %w", MethodJobHistory, err)
	}

	var page JobPage
	if err := c.call(ctx, MethodJobHistory, params, &page); err != nil {
		return JobPage{}, err
	}

	for index := range page.Items {
		if err := page.Items[index].validObjects(); err != nil {
			return JobPage{}, fmt.Errorf("deviceapi: %s: %w", MethodJobHistory, err)
		}
	}

	return page, nil
}
