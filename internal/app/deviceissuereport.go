package app

import (
	"bytes"
	"context"
	"slices"
	"time"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// issueReport puts a change in what is wrong with one printer on the bus.
//
// The issues are worked out from what the pump already reads: the dialog
// subtree of the state tree, whether the link is up, and the link reading the
// link report took. So a change can only follow one of those, and the pump
// touches the report after each of them.
//
// A touch arms a short wait rather than publishing at once. A driver writes a
// dialog one path at a time, its id and then its title, and a frame per path
// would tell a module two things where the machine said one. The frame that
// leaves carries what the burst settled on, and only when it differs from the
// one before it.
type issueReport struct {
	plane  *devices
	entry  *device
	settle *time.Timer
	armed  bool

	// published is what a reader already holds. A printer arrives with the
	// issues the listing shows it with, so that is where it starts and the
	// arrival is not followed by a frame repeating it.
	issues []httpapi.DeviceIssue
	dialog *deviceapi.Dialog
}

// issueSettle is how long a burst of changes is folded for before the state it
// settled on is published. It is longer than a driver takes to write a dialog's
// paths and short enough that a person reads it as at once.
const issueSettle = 500 * time.Millisecond

// issueReportOf returns the reporter for one printer, holding what it has wrong
// over the link reading its arrival stated, and no timer until a touch.
func (d *devices) issueReportOf(entry *device, link httpapi.DeviceLink) *issueReport {
	settle := time.NewTimer(issueSettle)
	settle.Stop()

	return &issueReport{
		plane:  d,
		entry:  entry,
		settle: settle,
		issues: entry.issuesWith(link),
		dialog: entry.waitingDialog(),
	}
}

// touch says something the issues are read from may have moved. A touch while
// a wait is running is folded into it.
func (r *issueReport) touch() {
	if r.armed {
		return
	}

	r.settle.Reset(issueSettle)
	r.armed = true
}

// elapsed ends a wait and publishes the issues over link, if a reader does not
// hold them already.
func (r *issueReport) elapsed(link httpapi.DeviceLink) {
	r.armed = false

	issues, dialog := r.entry.issuesWith(link), r.entry.waitingDialog()
	if slices.Equal(issues, r.issues) && sameDialog(dialog, r.dialog) {
		return
	}

	r.issues, r.dialog = issues, dialog

	r.plane.publish(context.Background(), r.entry.id, event.TypeDeviceIssuesChanged,
		event.ClassState, "", issuesChangeOf(r.entry, issues, dialog))
}

func (r *issueReport) stop() { r.settle.Stop() }

// waitingDialog is the dialog the printer is waiting on now, read under the
// entry's lock, and nil for a printer that has departed, as its issue list
// names none either.
func (e *device) waitingDialog() *deviceapi.Dialog {
	if e.departure() != nil {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	return dialogInState(e.state)
}

// sameDialog reports whether two dialogs say the same thing.
func sameDialog(a, b *deviceapi.Dialog) bool {
	if a == nil || b == nil {
		return a == b
	}

	return bytes.Equal(a.ID, b.ID) && a.Code == b.Code && a.Title == b.Title && a.Text == b.Text &&
		slices.Equal(a.Buttons, b.Buttons)
}
