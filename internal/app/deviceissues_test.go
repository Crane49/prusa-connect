package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/kernel/deviceissue"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// TestEveryIssueTheDevicePlaneRaisesIsInTheClosedSet is ADR-20260825-bhe rule 8 held at
// the one place a reason reaches a client. A token the plane invents is a badge
// no client can look up and no translator can translate, and the vocabulary's
// own gates cannot see it: they read the table, and this reads what is put on
// the wire beside it.
func TestEveryIssueTheDevicePlaneRaisesIsInTheClosedSet(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	// Four printers, between them carrying every state the plane can assess: a
	// dialog raised, a session that ended, a link down under a held session,
	// and one that is simply here.
	raiseDialog(t, plane, arriveSpeaking(t, plane, "SIM-DIALOG"), "Filament runout")

	ended := arriveSpeaking(t, plane, "SIM-GONE")
	closeAndAwaitDeparture(t, plane, ended)

	absent := arrivePresent(t, plane, "SIM-ABSENT")
	absent.setConnected(false)

	arrive(t, plane, "SIM-HERE")

	page, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{})
	require.NoError(t, err)
	require.Len(t, page.Items, 4)

	raised := 0

	for _, item := range page.Items {
		for _, issue := range item.Issues {
			raised++

			reason := deviceissue.Reason(issue.Reason)
			require.Truef(t, reason.Valid(),
				"%s carries the issue %q, which the closed set does not hold", item.ID, issue.Reason)

			require.Equalf(t, deviceissue.SeverityOf(reason).String(), issue.Severity,
				"%s carries %q at severity %q, and the vocabulary declares another",
				item.ID, issue.Reason, issue.Severity)
		}
	}

	require.NotZero(t, raised, "this gate read no issues at all")

	// The rollup names nothing the closed set does not, and it publishes a key
	// for every reason rather than only the ones that fired.
	require.Len(t, page.Issues.Counts, len(deviceissue.Reasons()))

	for reason := range page.Issues.Counts {
		require.Truef(t, deviceissue.Reason(reason).Valid(),
			"the rollup counts %q, which the closed set does not hold", reason)
	}

	t.Logf("observed: the listing raised %d issues across %d printers, rolled up as %v",
		raised, len(page.Items), page.Issues)
}

// TestAPrinterWaitingAtADialogNeedsAnOperator is the reason the whole set
// exists. A machine that has stopped and put a question on its screen looks
// exactly like an idle machine in every other member of the record, and this is
// what tells a person to walk to it.
func TestAPrinterWaitingAtADialogNeedsAnOperator(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	session := arriveSpeaking(t, plane, "SIM-DIALOG")

	before := issuesOf(t, plane, plane.idOf(session.identity))
	require.NotContains(t, reasonsOf(before), deviceissue.ReasonAwaitingOperator,
		"a printer with no dialog is reported as waiting for somebody")

	raiseDialog(t, plane, session, "Filament runout")

	raised := issuesOf(t, plane, plane.idOf(session.identity))
	require.Contains(t, reasonsOf(raised), deviceissue.ReasonAwaitingOperator)

	first := raised[0]
	require.Equal(t, deviceissue.ReasonAwaitingOperator.String(), first.Reason,
		"the reason a person acts on is not first in the list")
	require.Equal(t, deviceissue.SeverityError.String(), first.Severity)
	require.Equal(t, "Filament runout", first.Detail,
		"the machine's own words did not reach the issue")

	t.Logf("observed: the dialog reads as %+v", first)

	// The machine leaving the state clears the dialog, and the issue goes with
	// it. An issue that outlives its cause sends somebody to a printer that is
	// printing.
	clearDialog(t, plane, session)

	require.NotContains(t, reasonsOf(issuesOf(t, plane, plane.idOf(session.identity))),
		deviceissue.ReasonAwaitingOperator, "the issue outlived the dialog that raised it")
}

// TestADepartedPrinterCarriesOnlyTheDepartedIssue holds the one rule that keeps
// this list from lying. A printer that went away while a dialog was up would
// otherwise be reported as waiting for a person, and somebody would walk to a
// machine that is switched off.
func TestADepartedPrinterCarriesOnlyTheDepartedIssue(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	session := arriveSpeaking(t, plane, "SIM-GONE")

	raiseDialog(t, plane, session, "Filament runout")
	require.Contains(t, reasonsOf(issuesOf(t, plane, plane.idOf(session.identity))),
		deviceissue.ReasonAwaitingOperator)

	closeAndAwaitDeparture(t, plane, session)

	gone := issuesOf(t, plane, plane.idOf(session.identity))
	require.Equal(t, []deviceissue.Reason{deviceissue.ReasonDeparted}, reasonsOf(gone))
	require.Equal(t, departureSessionEnded, gone[0].Detail)
	require.Equal(t, deviceissue.SeverityWarning.String(), gone[0].Severity)

	t.Logf("observed: a departed printer reports %+v", gone)
}

// TestAPrinterWhoseLinkIsDownIsReportedAsDisconnected covers the other absence:
// the driver still holds the session, so a command queues rather than failing,
// and nothing is answering.
func TestAPrinterWhoseLinkIsDownIsReportedAsDisconnected(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	session := arrivePresent(t, plane, "SIM-ABSENT")

	require.NotContains(t, reasonsOf(issuesOf(t, plane, plane.idOf(session.identity))),
		deviceissue.ReasonDisconnected, "a printer that is answering reads as disconnected")

	session.setConnected(false)

	absent := issuesOf(t, plane, plane.idOf(session.identity))
	require.Contains(t, reasonsOf(absent), deviceissue.ReasonDisconnected)
	require.NotContains(t, reasonsOf(absent), deviceissue.ReasonDeparted,
		"a held session reads as a departure")

	t.Logf("observed: a printer with no connection under a held session reports %+v", absent)
}

// TestThePrinterWithNoProfileSaysSo is the informational end of the set. The
// printer works and offers fewer verbs than a covered one, which is a fact a
// person comparing two machines needs and never a failure.
func TestThePrinterWithNoProfileSaysSo(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	id := arrive(t, plane, "SIM-UNCOVERED")

	found := issuesOf(t, plane, id)
	require.Contains(t, reasonsOf(found), deviceissue.ReasonNoCapabilityProfile)

	for _, issue := range found {
		if issue.Reason == deviceissue.ReasonNoCapabilityProfile.String() {
			require.Equal(t, deviceissue.SeverityInfo.String(), issue.Severity)
		}
	}

	// It is last, because the list is ordered by severity and this is the least
	// of them. A client showing one badge shows the first entry.
	require.Equal(t, deviceissue.ReasonNoCapabilityProfile.String(), found[len(found)-1].Reason)
}

// TestTheIssueRollupCountsOnlyThePrintersTheCallerMaySee is an earlier design decision at the
// rollup. A count over the whole process would tell somebody who may see one
// printer that four hundred exist and that three of them are stopped, which is
// exactly the fact their grant withholds.
func TestTheIssueRollupCountsOnlyThePrintersTheCallerMaySee(t *testing.T) {
	t.Parallel()

	graph := newGraph(t)
	plane := planeOn(t, graph)

	mine := arriveSpeaking(t, plane, "SIM-MINE")
	yours := arriveSpeaking(t, plane, "SIM-YOURS")

	raiseDialog(t, plane, mine, "Filament runout")
	raiseDialog(t, plane, yours, "Bed levelling failed")

	require.NoError(t, graph.Write(t.Context(),
		tuple(t, authz.TypeDevice, plane.idOf(mine.identity), authz.RelOperator, watcher)))

	page, err := plane.List(t.Context(), watcher, httpapi.DeviceQuery{})
	require.NoError(t, err)

	require.Equal(t, 1, page.Total)
	require.Equal(t, 1, page.Issues.Affected,
		"the rollup counted a printer this caller may not see")
	require.Equal(t, 1, page.Issues.Counts[deviceissue.ReasonAwaitingOperator.String()])

	body, err := json.Marshal(page.Issues)
	require.NoError(t, err)
	t.Logf("observed: %s is told %s", watcher, body)

	// The whole population is two, and nothing the caller reads says so.
	require.NotContains(t, string(body), `"affected":2`)
}

// TestACallerGrantedNothingLearnsNothingFromTheRollup is the other half of
// an earlier design decision, and it is the half a count leaks through.
//
// The rollup is a number that moves when somebody else's printer breaks. A
// caller who holds nothing must not be able to poll it and watch it move: that
// reports how large the fleet is, how much of it is unwell, and roughly when
// each machine stopped, to somebody who may not see one printer.
//
// The same defect has been found twice on this branch, on a version token the
// preset listing handed out and on the one before it, so the case is written
// out rather than assumed from the walk.
func TestACallerGrantedNothingLearnsNothingFromTheRollup(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	quiet, err := plane.List(t.Context(), stranger, httpapi.DeviceQuery{})
	require.NoError(t, err)

	// Three printers, wrong in three different ways, so a rollup filled in from
	// any of the plane's own tables would move rather than staying at zero.
	raiseDialog(t, plane, arriveSpeaking(t, plane, "SIM-DIALOG"), "Filament runout")
	closeAndAwaitDeparture(t, plane, arriveSpeaking(t, plane, "SIM-GONE"))
	arrivePresent(t, plane, "SIM-ABSENT").setConnected(false)

	// The fleet owner watches all three go wrong, so the test knows the rollup
	// it is comparing against actually moved.
	owned, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{})
	require.NoError(t, err)
	require.Equal(t, 3, owned.Issues.Affected)
	require.Equal(t, 1, owned.Issues.Counts[deviceissue.ReasonDeparted.String()])

	after, err := plane.List(t.Context(), stranger, httpapi.DeviceQuery{})
	require.NoError(t, err)

	require.Equal(t, quiet, after, "the answer to a caller who holds nothing moved")
	require.Zero(t, after.Total)
	require.Zero(t, after.Issues.Affected)
	require.Empty(t, after.Items)

	for reason, count := range after.Issues.Counts {
		require.Zerof(t, count, "a caller who holds nothing is told %d printers carry %s", count, reason)
	}

	// Every reason is still a key, at zero. That is a fact about this build and
	// not about this deployment: the same keys are published by every server
	// running these bytes, whoever asks.
	require.Len(t, after.Issues.Counts, len(deviceissue.Reasons()))

	body, err := json.Marshal(after)
	require.NoError(t, err)
	t.Logf("observed: %s is told %s", stranger, body)
}

// TestTheIssueFilterNarrowsThePageAndNotTheRollup is what makes one request
// enough. A client asking for the printers that need somebody still learns how
// large the fleet behind them is, so the badge it draws does not read zero
// exactly when the filter is doing its job.
func TestTheIssueFilterNarrowsThePageAndNotTheRollup(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	const (
		fleet   = 12
		stopped = 3
	)

	for i := range fleet {
		session := arriveSpeaking(t, plane, fmt.Sprintf("SIM-%04d", i))
		if i < stopped {
			raiseDialog(t, plane, session, "Filament runout")
		}
	}

	all, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{})
	require.NoError(t, err)
	require.Equal(t, fleet, all.Total)

	// The rollup is over the fleet and not over the page, so a client that
	// turned to page two does not watch its badge change. The window here is
	// smaller than the number of printers that need somebody, so a rollup
	// counted from the items would come back short.
	windowed, err := plane.List(t.Context(), fleetOwner,
		httpapi.DeviceQuery{Page: httpapi.Page{Limit: 2}})
	require.NoError(t, err)
	require.Len(t, windowed.Items, 2)
	require.Equal(t, all.Issues, windowed.Issues,
		"the rollup counted the page rather than every printer the caller may see")

	only, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{WithIssues: true})
	require.NoError(t, err)

	require.Equal(t, stopped, only.Total, "the filter did not narrow the page")
	require.Len(t, only.Items, stopped)

	for _, item := range only.Items {
		require.NotEmpty(t, item.Issues, "the filtered page carries a printer with nothing wrong")
	}

	require.Equal(t, all.Issues, only.Issues,
		"the filter moved the rollup, so a client filtering to the problems loses the fleet behind them")
	require.Equal(t, stopped, only.Issues.Affected)

	t.Logf("observed: %d of %d printers need somebody, and the filtered page carries %d items",
		only.Issues.Affected, fleet, len(only.Items))

	// The filter composes with the window rather than replacing it: the offset
	// counts the printers that matched, not the printers that were walked.
	second, err := plane.List(t.Context(), fleetOwner,
		httpapi.DeviceQuery{Page: httpapi.Page{Limit: 1, Offset: 1}, WithIssues: true})
	require.NoError(t, err)
	require.Len(t, second.Items, 1)
	require.Equal(t, only.Items[1].ID, second.Items[0].ID)
}

// TestAPrinterWithNothingWrongCarriesAnEmptyArray keeps the member readable by
// a client that does not check for null. A profile-covered printer is the one
// case where the array is empty, and it has to be an array.
func TestAPrinterWithNothingWrongCarriesAnEmptyArray(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)
	arriveCovered(t, plane, "SIM-COVERED")

	page, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Empty(t, page.Items[0].Issues)
	require.Zero(t, page.Issues.Affected)

	body, err := json.Marshal(page.Items[0])
	require.NoError(t, err)
	require.Contains(t, string(body), `"issues":[]`, "an empty issue list reached the wire as null")

	t.Logf("observed: a covered printer with nothing wrong reports %s", body)
}

// reasonsOf is the reason of each issue, in the order they were reported.
func reasonsOf(issues []httpapi.DeviceIssue) []deviceissue.Reason {
	out := make([]deviceissue.Reason, 0, len(issues))
	for _, issue := range issues {
		out = append(out, deviceissue.Reason(issue.Reason))
	}

	return out
}

// issuesOf reads one printer's issues through the published surface, so a test
// asserts on what a client is told rather than on what the plane holds.
func issuesOf(t *testing.T, plane *devices, id string) []httpapi.DeviceIssue {
	t.Helper()

	found, err := plane.Get(t.Context(), fleetOwner, id)
	require.NoError(t, err)

	return found.Issues
}

// speakingSession is a session whose state channel a test writes to, so a
// dialog can be raised the way a driver raises one.
type speakingSession struct {
	countedSession
}

// arriveSpeaking puts one printer in the plane and keeps its state channel, so
// the test can report what the machine is doing.
//
// It arrives covered by a profile, which is what makes an assertion about one
// issue readable: a printer no profile covers carries the informational reason
// by construction, and every list a test compared would carry it too.
func arriveSpeaking(t *testing.T, plane *devices, serial string) *speakingSession {
	t.Helper()

	host := plane.Arrivals("test", nil, capability.NewResolver(coveringRegistry(), discard()), "")

	session := &speakingSession{countedSession{
		identity: connectorapi.Identity{
			Vendor:   "Prusa",
			Model:    "MK4",
			Serial:   serial,
			Firmware: connectorapi.NewFirmwareLabel("6.4.1"),
		},
		patches: make(chan connectorapi.StatePatch),
	}}

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { session.closeOnce() })

	return session
}

// closeOnce closes the state channel, and tolerates a test that already did.
func (s *speakingSession) closeOnce() {
	defer func() { _ = recover() }() // a test that ended the session already closed it

	close(s.patches)
}

// report sends one state patch and waits until the plane has applied it. The
// send alone proves nothing: the pump reads the channel and then records, so an
// assertion made straight after the send races the goroutine that applies it.
func (s *speakingSession) report(t *testing.T, plane *devices, path, value string) {
	t.Helper()

	s.patches <- connectorapi.StatePatch{Path: path, Value: json.RawMessage(value)}

	id := plane.idOf(s.identity)

	require.Eventually(t, func() bool {
		entry, live := plane.entry(id)
		if !live {
			return false
		}

		entry.mu.Lock()
		defer entry.mu.Unlock()

		held, carried := entry.state[path]
		if value == "null" {
			return !carried
		}

		return carried && string(held) == value
	}, settle, time.Millisecond, "the patch for %s never reached the state tree", path)
}

// raiseDialog puts a printer where a firmware puts it when it needs a person:
// an id that says a dialog is up, and the words that go with it.
func raiseDialog(t *testing.T, plane *devices, session *speakingSession, title string) {
	t.Helper()

	session.report(t, plane, deviceissue.DialogIDPath, `"42"`)
	session.report(t, plane, deviceissue.DialogTitlePath, fmt.Sprintf("%q", title))
}

// clearDialog is what a driver does when the machine leaves the state the
// dialog was raised in: the paths go, because a null value removes a node.
func clearDialog(t *testing.T, plane *devices, session *speakingSession) {
	t.Helper()

	session.report(t, plane, deviceissue.DialogTitlePath, "null")
	session.report(t, plane, deviceissue.DialogIDPath, "null")
}

// coveringRegistry holds a profile for the identity every speaking printer in
// this file reports, so one arrives with nothing wrong.
func coveringRegistry() profile.Registry {
	return profile.Registry{
		Schema: profile.CurrentSchema,
		Profiles: []profile.Profile{{
			Model:        "MK4",
			Firmware:     profile.Range{From: "6.4.0"},
			Capabilities: connectorapi.Set{},
		}},
	}
}

// arriveCovered is a printer with a profile behind it and nothing to say, which
// is the one arrival that carries no issue at all.
func arriveCovered(t *testing.T, plane *devices, serial string) string {
	t.Helper()

	return plane.idOf(arriveSpeaking(t, plane, serial).identity)
}

// closeAndAwaitDeparture ends the session and waits until the plane has filed
// the printer as departed.
func closeAndAwaitDeparture(t *testing.T, plane *devices, session *speakingSession) {
	t.Helper()

	session.closeOnce()

	id := plane.idOf(session.identity)

	require.Eventually(t, func() bool {
		entry, known := plane.known(id)

		return known && entry.departure() != nil
	}, settle, time.Millisecond, "the session ended and the printer never departed")
}

// TestNoFilterMovesTheRollup is ADR-20260827-xca rule 7: the badge reads the fleet,
// whatever the reader narrowed the page to.
func TestNoFilterMovesTheRollup(t *testing.T) {
	t.Parallel()

	plane := newPlane(t)

	for i := range 6 {
		session := arriveSpeaking(t, plane, fmt.Sprintf("SIM-ROLL-%d", i))
		if i < 2 {
			raiseDialog(t, plane, session, "Filament runout")
		}
	}

	all, err := plane.List(t.Context(), fleetOwner, httpapi.DeviceQuery{})
	require.NoError(t, err)
	require.Equal(t, 2, all.Issues.Affected)

	total := 0
	for _, count := range all.Issues.BySeverity {
		total += count
	}
	require.Equal(t, 2, total)
	require.Len(t, all.Issues.BySeverity, 3, "a severity at zero is absent rather than zero")

	for _, query := range []httpapi.DeviceQuery{
		{States: []string{"PRINTING"}},
		{Models: []string{"XL"}},
		{Search: "nothing-matches-this"},
		{Severities: []string{"info"}},
		{WithIssues: true},
		{Retired: "only"},
		{Retired: "include"},
	} {
		narrowed, listErr := plane.List(t.Context(), fleetOwner, query)
		require.NoError(t, listErr)
		require.Equalf(t, all.Issues, narrowed.Issues, "%+v moved the rollup", query)
	}
}
