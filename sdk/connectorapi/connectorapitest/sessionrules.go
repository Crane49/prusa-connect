package connectorapitest

import (
	"context"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// These cases test the session as a whole and not one call: the capabilities
// the printer reported, a command sent while nothing reads the state channel,
// and the connection totals a session reports.

// checkProbe tests the capabilities a printer reported. The probe may remove a
// capability from the driver's baseline. It may never add one.
//
// connectorapi.ReportsProbe is optional. A session that does not implement it
// passes, and the log says what that means: the API lists every command of the
// driver's baseline and the model's profile for each printer of this driver,
// including commands one printer does not support.
func checkProbe(t *testing.T, subject Subject, session connectorapi.Session) {
	t.Helper()

	reporter, reports := session.(connectorapi.ReportsProbe)
	if !reports {
		t.Log("this session does not implement connectorapi.ReportsProbe, so the host lists every " +
			"command of the driver's baseline and the model's profile, including commands this " +
			"printer does not support")

		return
	}

	probe := reporter.Probe()
	if probe == nil {
		t.Log("this session reported no capabilities for this printer, so the host uses the profile alone")

		return
	}

	baseline := subject.Driver.Baseline()

	for key, declared := range probe {
		ceiling, inBaseline := baseline[key]
		if !inBaseline {
			t.Errorf("the probe lists %s, and this driver's baseline does not have it. A probe may "+
				"remove a capability and may never add one: the driver has no code for a "+
				"capability outside its baseline", key)

			continue
		}

		for _, err := range widenedErrors(key, ceiling, declared) {
			t.Error(err)
		}
	}

	resolved, _ := session.Capabilities()
	for key := range resolved {
		if _, claimed := probe[key]; !claimed {
			t.Errorf("this session offers %s and the probe does not carry it, so it offers a verb "+
				"the machine did not say it has", key)
		}
	}
}

// commandWhileUnread bounds the wait for a command sent with nothing reading
// the state channel. A driver that blocks its reader on a full patch queue
// answers nothing, and the case has to say that rather than hang the binary.
const commandWhileUnread = 30 * time.Second

// checkStateDoesNotBlockCommands sends a command while nothing reads State.
//
// Session.State requires that what fills the channel never waits for the host.
// A driver that lets a full patch queue block the goroutine reading the
// connection stops answering commands as soon as the host falls behind, and a
// host holding several devices falls behind regularly.
//
// What it does not demonstrate: the suite cannot make a machine report faster
// than the channel drains, so a driver whose queue never filled during the case
// passes it. The bound and the drop rule are held by the driver's own tests.
func checkStateDoesNotBlockCommands(t *testing.T, subject Subject, session connectorapi.Session) {
	t.Helper()

	if subject.NoSafeVerb {
		t.Skip("Subject.NoSafeVerb is set: there is no verb the suite may send while the state " +
			"channel goes unread")
	}

	cmd, ok := safeCommand(t, subject, session)
	if !ok {
		return
	}

	asked, cancel := context.WithTimeout(t.Context(), commandWhileUnread)
	defer cancel()

	answered := make(chan error, 1)

	go func() {
		_, err := session.Do(asked, cmd)
		answered <- err
	}()

	select {
	case err := <-answered:
		if err != nil {
			t.Errorf("Do(%s) with nothing reading the state channel: %v", cmd.Verb, err)
		}
	case <-asked.Done():
		t.Errorf("Do(%s) did not answer within %s while nothing read the state channel. A driver "+
			"that lets a full patch queue block the goroutine reading its connection stops "+
			"answering commands whenever the host falls behind; see connectorapi.Session.State",
			cmd.Verb, commandWhileUnread)
	}
}

// checkLinkCounters reads the totals a session reports about its connection.
//
// Session.State requires a driver to count the patches it drops. The interface
// that reports the count is optional, because a session that never drops one
// still has nothing to say; a session that implements it is held to totals that
// only grow, so a host reading two of them apart can subtract.
func checkLinkCounters(t *testing.T, session connectorapi.Session) {
	t.Helper()

	counters, counts := session.(connectorapi.CountsLink)
	if !counts {
		t.Log("this session does not implement connectorapi.CountsLink, so the host reports it as " +
			"counting nothing: a dropped patch and no dropped patch read the same to an operator")

		return
	}

	firstDropped, firstRefused := counters.Dropped(), counters.Refused()
	secondDropped, secondRefused := counters.Dropped(), counters.Refused()

	if secondDropped < firstDropped {
		t.Errorf("Dropped went backwards, from %d to %d. A host subtracts two readings to get a "+
			"rate, and a total that falls reads as a rate of nothing", firstDropped, secondDropped)
	}

	if secondRefused < firstRefused {
		t.Errorf("Refused went backwards, from %d to %d", firstRefused, secondRefused)
	}
}
