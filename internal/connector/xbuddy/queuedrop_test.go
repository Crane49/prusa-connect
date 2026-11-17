package xbuddy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// queueBudget bounds every wait here. Nothing in this file waits for a clock:
// each case ends a session and reads the answer that ending produced, so a
// budget being reached is a command that was never settled at all.
const queueBudget = 5 * time.Second

// queuedIdentity is the machine the commands in this file are composed for.
var queuedIdentity = connectorapi.Identity{
	Vendor:   Vendor,
	Model:    "MK4",
	Serial:   "SN-QUEUE",
	UnitUID:  "fp-queue",
	Firmware: connectorapi.NewFirmwareLabel("6.5.0"),
}

// TestACommandStillWaitingWhenTheSessionEndsIsToldItWasNeverSent is the floor
// under every case below: a command the session never put on a wire settles the
// moment the session does, with a registered code, rather than waiting for its
// caller's own context to run out.
//
// The caller's context here is an hour, so a command that is not settled by the
// close is one this test waits an hour for and the budget catches. That is the
// behaviour being pinned: not the wording, the fact that anything is said.
func TestACommandStillWaitingWhenTheSessionEndsIsToldItWasNeverSent(t *testing.T) {
	t.Parallel()

	session := queuedSession(t)

	answered := offer(t, session, VerbBeep)

	// Nothing has attached, so the command is in the queue and the pump is
	// waiting for a connection that never comes.
	select {
	case settled := <-answered:
		t.Fatalf("a command for a printer that is away was settled before anything ended: %v", settled)
	case <-time.After(10 * time.Millisecond):
	}

	_ = session.Close()

	err := await(t, answered)
	t.Logf("observed: %v", err)

	if !fault.HasCode(err, fault.PrinterSessionEnded.Code()) {
		t.Errorf("the dropped command reported %v, want %s", err, fault.PrinterSessionEnded.Code())
	}

	if !errors.Is(err, ErrSessionClosed) {
		t.Error("the code replaced the sentinel rather than carrying it, so an in-process caller " +
			"that branched on ErrSessionClosed stopped seeing it")
	}
}

// TestEveryCommandWaitingWhenTheSessionEndsIsTold is the same rule for the
// whole queue rather than its head.
//
// A drain that settled one command and left the rest would pass the case above
// and leave every other caller waiting for its own context, which is the
// failure this whole file exists to remove.
func TestEveryCommandWaitingWhenTheSessionEndsIsTold(t *testing.T) {
	t.Parallel()

	const queued = 4

	session := queuedSession(t)

	waiting := make([]chan outcome, 0, queued)
	for range queued {
		waiting = append(waiting, offer(t, session, VerbBeep))
	}

	_ = session.Close()

	for i, answered := range waiting {
		err := await(t, answered)
		if !fault.HasCode(err, fault.PrinterSessionEnded.Code()) {
			t.Errorf("queued command %d reported %v, want %s", i, err, fault.PrinterSessionEnded.Code())
		}
	}

	t.Logf("observed: all %d commands waiting for the printer were answered by the close", queued)
}

// TestACommandDroppedBecauseThePrinterCameBackChangedSaysSoAndNamesBothFirmwares
// is the case the old backlog records: a printer that comes back reflashed ends the
// session its commands were queued on, because a capability set is fixed for a
// session's life.
//
// The caller learns three things it cannot learn from "the session closed":
// that nothing was sent, that the machine is there and is different, and which
// two firmware versions those are. The last is what a person at the bench acts
// on, because it says the command was composed before the flash.
func TestACommandDroppedBecauseThePrinterCameBackChangedSaysSoAndNamesBothFirmwares(t *testing.T) {
	t.Parallel()

	session := queuedSession(t)

	answered := offer(t, session, VerbBeep)

	reflashed := queuedIdentity
	reflashed.Firmware = connectorapi.NewFirmwareLabel("6.6.0")

	_ = session.CloseReplaced(reflashed)

	err := await(t, answered)
	t.Logf("observed: %v", err)

	if !fault.HasCode(err, connectorapi.PrinterChanged.Code()) {
		t.Fatalf("the dropped command reported %v, want %s", err, connectorapi.PrinterChanged.Code())
	}

	var raised *fault.Fault
	if !errors.As(err, &raised) {
		t.Fatalf("the code is not on a fault the transport can render: %v", err)
	}

	for key, want := range map[string]string{
		"verb":              VerbBeep.String(),
		"previous_firmware": "6.5.0",
		"firmware":          "6.6.0",
	} {
		value, carried := raised.Params().Get(key)
		if !carried {
			t.Errorf("the refusal carries no %s, so the caller cannot tell what changed", key)

			continue
		}

		if got := value.String(); got != want {
			t.Errorf("the refusal reports %s %q, want %q", key, got, want)
		}
	}

	if !raised.Retryable() {
		t.Error("the refusal is not retryable, and a command that was never sent is exactly the one " +
			"a caller may send again once it has read the new capability set")
	}
}

// TestACommandOnTheWireWhenTheConnectionGoesIsNeverReportedAsUnsent is the
// other half, and it is the half that is dangerous to get wrong.
//
// The frame is on the socket, so the printer may have carried the command out
// and gone quiet before the event that says so. Reporting that as "not sent"
// would invite a caller to send it again, and a second START_PRINT is a second
// print. It is a code of its own, and its retry hint is never.
func TestACommandOnTheWireWhenTheConnectionGoesIsNeverReportedAsUnsent(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	answered := printer.start(t)

	// Reading the frame is what proves the command left the host. A case that
	// skipped it could not tell an unsent command from a sent one.
	id := printer.takeFrame(t)
	t.Logf("observed: command %d is on the wire and unanswered", id)

	_ = printer.client.Close()

	_, err := printer.await(t, answered)
	t.Logf("observed: %v", err)

	if !fault.HasCode(err, connectorapi.CommandUnanswered.Code()) {
		t.Fatalf("the command in flight reported %v, want %s", err, connectorapi.CommandUnanswered.Code())
	}

	if fault.HasCode(err, fault.PrinterSessionEnded.Code()) ||
		fault.HasCode(err, connectorapi.PrinterChanged.Code()) {
		t.Error("a command that reached the printer was reported as one that never did")
	}

	var raised *fault.Fault
	if !errors.As(err, &raised) {
		t.Fatalf("the code is not on a fault the transport can render: %v", err)
	}

	if raised.Retryable() {
		t.Error("a command the printer may already have carried out is marked retryable, which is " +
			"how one print becomes two")
	}

	if !errors.Is(err, ErrPrinterWentAway) {
		t.Error("the code replaced the sentinel rather than carrying it")
	}
}

// TestARestartingCommandWhoseConnectionGoesSucceeded holds the one exception to
// the case above: a reset reboots the printer, which drops the socket before it
// can answer, so the drop is the success.
func TestARestartingCommandWhoseConnectionGoesSucceeded(t *testing.T) {
	t.Parallel()

	for _, verb := range []connectorapi.Capability{VerbResetPrinter, VerbReset} {
		t.Run(string(verb), func(t *testing.T) {
			t.Parallel()

			printer := standHostile(t)

			answered := printer.startVerb(t, verb)
			printer.takeFrame(t)

			_ = printer.client.Close()

			result, err := printer.await(t, answered)
			if err != nil {
				t.Fatalf("%s reported %v, want success", verb, err)
			}

			if !result.OK {
				t.Errorf("%s result is not OK: %+v", verb, result)
			}
		})
	}
}

// queuedSession is a session with a capability set and no connection, which is
// what a printer that is switched off looks like from above.
func queuedSession(t *testing.T) *Session {
	t.Helper()

	// SEND_INFO is withheld for the reason standHostile withholds it: the
	// session asks for it on every attach, and nothing here answers.
	caps := baseline().Clone()
	delete(caps, connectorapi.VerbSendInfo)

	session := newSession(settings{
		identity:   queuedIdentity,
		caps:       caps,
		storage:    defaultStorage,
		queueDepth: 8,
	})
	t.Cleanup(func() { _ = session.Close() })

	return session
}

// offer sends one command and hands back the channel its outcome arrives on.
//
// The context is an hour so that it settles nothing. What settles these
// commands is the session ending, and a shorter context would let a case pass
// against a driver that says nothing at all.
func offer(t *testing.T, session *Session, verb connectorapi.Capability) chan outcome {
	t.Helper()

	answered := make(chan outcome, 1)

	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Hour)
		defer cancel()

		result, err := session.Do(ctx, connectorapi.Command{Verb: verb})
		answered <- outcome{result: result, err: err}
	}()

	return answered
}

// await takes one command's outcome, and fails the case when nothing settles
// it. A command nobody settles is the whole failure this file pins.
func await(t *testing.T, answered chan outcome) error {
	t.Helper()

	select {
	case settled := <-answered:
		return settled.err
	case <-time.After(queueBudget):
		t.Fatalf("no answer reached the caller inside %s, so a command was dropped in silence", queueBudget)

		return errors.New("unreachable")
	}
}
