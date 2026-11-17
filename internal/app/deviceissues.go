package app

// What the host has found wrong with one printer. The reasons are the closed
// vocabulary in internal/kernel/deviceissue; this file is the only thing that
// decides when one holds.
//
// Every reading here is memory this process already holds for a printer it has
// been dialled by: the state tree the driver's patches built, whether a session
// is open, why it ended, and the two counters behind the link reading. Nothing
// here queries anything, so a fleet's worth of issues costs the statements the
// listing already cost.

import (
	"encoding/json"

	"github.com/prusa3d/prusa-connect/internal/kernel/deviceissue"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// issues is what the host has found wrong with this printer, in the
// vocabulary's own order, which puts the reason a person reads first first. It
// returns the link reading it judged, because the record beside it carries that
// reading and the two have to be the same one.
//
// link.observe rolls a window as it answers, so a second call for the same
// record is a second reading of one accumulator. The record could then say a
// printer is refusing frames beside a rate of zero. An issue is an index into
// its own record, so the two must agree.
//
// A printer whose session has ended carries exactly one issue: it has departed.
// The rest of the record is the last reading this server took, and reporting a
// dialog that was up an hour ago as work waiting for somebody would send a
// person to a machine that is switched off.
func (e *device) issues() ([]httpapi.DeviceIssue, httpapi.DeviceLink) {
	link := e.linkHealth()

	return e.issuesWith(link), link
}

// issuesWith is issues over a link reading taken already. The event that
// reports a change of issues judges the reading the link report took, because
// taking a second one would roll the window under it.
func (e *device) issuesWith(link httpapi.DeviceLink) []httpapi.DeviceIssue {
	if departed := e.departure(); departed != nil {
		return []httpapi.DeviceIssue{issueOf(deviceissue.ReasonDeparted, departed.Reason)}
	}

	found := make([]httpapi.DeviceIssue, 0, len(deviceissue.Reasons()))

	if title, raised := e.dialog(); raised {
		found = append(found, issueOf(deviceissue.ReasonAwaitingOperator, title))
	}

	if !e.connected() {
		found = append(found, issueOf(deviceissue.ReasonDisconnected, ""))
	}

	if link.RefusedFramesPerMinute > 0 {
		found = append(found, issueOf(deviceissue.ReasonFramesRefused, ""))
	}

	if link.DroppedTelemetryPerMinute > 0 {
		found = append(found, issueOf(deviceissue.ReasonTelemetryDropped, ""))
	}

	if !e.session.ProfileMatched() {
		found = append(found, issueOf(deviceissue.ReasonNoCapabilityProfile, ""))
	}

	return found
}

// dialog reports whether the printer has raised a dialog, and what it put on
// the screen.
//
// The id is what says a dialog is up, because a driver removes it when the
// dialog closes and the machine sends no "dialog closed" of its own. The words
// beside it are the machine's, and either of them may be missing: a dialog with
// no title still stops the machine.
func (e *device) dialog() (title string, raised bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, up := e.state[deviceissue.DialogIDPath]; !up {
		return "", false
	}

	for _, path := range []string{deviceissue.DialogTitlePath, deviceissue.DialogTextPath} {
		if words := plainText(e.state[path]); words != "" {
			return words, true
		}
	}

	return "", true
}

// plainText is a state tree value read as the string the printer encoded, and
// is empty for anything else. A driver passes values through as the machine
// spelled them, so a path that should hold a sentence may hold a number, an
// object or nothing at all.
func plainText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return ""
	}

	return text
}

// issueOf is one issue on the wire. The severity is read from the vocabulary
// and not named here, so the two cannot disagree.
func issueOf(reason deviceissue.Reason, detail string) httpapi.DeviceIssue {
	return httpapi.DeviceIssue{
		Reason:   reason.String(),
		Severity: deviceissue.SeverityOf(reason).String(),
		Detail:   detail,
	}
}

// emptyIssueCounts is the rollup's zero: a key for every reason this build
// knows. A client reading a key it expects must not have to tell a count of
// zero from a key the server left out.
func emptyIssueCounts() map[string]int {
	counts := make(map[string]int, len(deviceissue.Reasons()))
	for _, reason := range deviceissue.Reasons() {
		counts[reason.String()] = 0
	}

	return counts
}

// emptyIssueSummary is the rollup's zero, with a key for every severity too.
func emptyIssueSummary() httpapi.DeviceIssueSummary {
	return httpapi.DeviceIssueSummary{
		Counts:     emptyIssueCounts(),
		BySeverity: map[string]int{severityError: 0, severityWarning: 0, severityInfo: 0},
	}
}

// addToRollup counts one printer's issues into the summary: the printer once
// under Affected, each reason once, and each distinct severity once.
func addToRollup(summary *httpapi.DeviceIssueSummary, issues []httpapi.DeviceIssue) {
	if len(issues) > 0 {
		summary.Affected++
	}

	severities := map[string]bool{}

	for _, issue := range issues {
		summary.Counts[issue.Reason]++

		if !severities[issue.Severity] {
			severities[issue.Severity] = true
			summary.BySeverity[issue.Severity]++
		}
	}
}
