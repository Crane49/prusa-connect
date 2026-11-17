package app

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The answer a caller gets when the printer refused.
//
// BUG-005: the POST answered 200 with {"ok":false,"command_id":"…"} and the
// stored row carried an empty machine reason and no fault code, so "the printer
// is busy, try again" and "this will never work" were the same answer. What
// separates them is a registered code, and the code now travels in the body of
// the 200 rather than turning the printer's own no into a problem document:
// this API answers a problem document for a refusal this server decided, and a
// refusal the machine decided is an answer.

// refusingSession is a driver session that refuses every command the way a
// dialect does: a result that is not OK, carrying the code for the refusal and
// no error.
type refusingSession struct {
	countedSession

	reason string
	// failed makes the session answer the way the dialect does for a FAILED
	// rather than a REJECTED: the printer took the command and could not carry
	// it out, which is a failure and leaves as an error.
	failed bool
}

func (s *refusingSession) Capabilities() (connectorapi.Set, uint64) { return talkingBaseline(), 1 }

func (s *refusingSession) Do(
	context.Context, connectorapi.Command,
) (connectorapi.Result, error) {
	if s.failed {
		return connectorapi.Result{OK: false},
			connectorapi.CommandFailedWithoutReason.
				New("the printer took the command, could not carry it out, and gave no reason for it").
				With(fault.StringParam("verb", nozzleVerb))
	}

	if s.reason == "" {
		return connectorapi.Result{
			OK:        false,
			FaultCode: connectorapi.CommandRefusedWithoutReason.Code().String(),
		}, nil
	}

	return connectorapi.Result{
		OK:            false,
		FaultCode:     connectorapi.CommandRefused.Code().String(),
		MachineReason: s.reason,
	}, nil
}

// arriveRefusing puts a printer in the plane that refuses every command, with
// the machine's own word for it where the case gives one.
func arriveRefusing(t *testing.T, plane *devices, serial, reason string) string {
	return arriveAnswering(t, plane, serial, reason, false)
}

// arriveFailing puts a printer in the plane that takes every command and
// cannot carry it out.
func arriveFailing(t *testing.T, plane *devices, serial string) string {
	return arriveAnswering(t, plane, serial, "", true)
}

func arriveAnswering(t *testing.T, plane *devices, serial, reason string, failed bool) string {
	t.Helper()

	host := plane.Arrivals("test", talkingBaseline(),
		capability.NewResolver(profile.Registry{}, discard()), "")

	session := &refusingSession{
		countedSession: countedSession{
			identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: serial},
			patches:  make(chan connectorapi.StatePatch),
		},
		reason: reason,
		failed: failed,
	}

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { close(session.patches) })

	return plane.idOf(session.identity)
}

// TestARefusalWithNoReasonAnswersACode is the gate on the HTTP half. A refusal
// the caller cannot read is not an answer, and 200 with ok false and nothing
// else was exactly that.
func TestARefusalWithNoReasonAnswersACode(t *testing.T) {
	t.Parallel()

	plane, _ := planeAndBus(t)
	id := arriveRefusing(t, plane, "SIM-UNEXPLAINED", "")

	answer, err := plane.Command(t.Context(), "user:"+config.DefaultPrincipal, id,
		httpapi.PrinterCommand{Verb: nozzleVerb})

	// The printer was reached and it answered, so this is an answer and not a
	// problem document. A 409 here would say this server declined to act.
	require.NoError(t, err, "the printer's own refusal was answered as a fault of this server's")

	require.False(t, answer.OK)
	require.Equal(t, connectorapi.CommandRefusedWithoutReason.Code().String(), answer.FaultCode,
		"the refusal carries no code, so a caller has nothing to branch on")

	// machine_reason is the printer's own words and the printer sent none. A
	// sentence written on this side would read as something the firmware said.
	require.Empty(t, answer.MachineReason,
		"a machine reason was invented for a printer that sent no word")

	require.NotEmpty(t, answer.CommandID,
		"a refused command carried no id, so the record it wrote is unaddressable")

	t.Logf("observed: the refusal answers 200 ok=false %s under %s",
		answer.FaultCode, answer.CommandID)
}

// TestARefusalWithAReasonAnswersACodeAndTheReason is the other refusal, and it
// is the one that was already a 200. It keeps that shape and gains the code, so
// a caller branches the same way for both.
func TestARefusalWithAReasonAnswersACodeAndTheReason(t *testing.T) {
	t.Parallel()

	plane, _ := planeAndBus(t)
	id := arriveRefusing(t, plane, "SIM-FILE-EXISTS", "FILE_EXISTS")

	answer, err := plane.Command(t.Context(), "user:"+config.DefaultPrincipal, id,
		httpapi.PrinterCommand{Verb: nozzleVerb})
	require.NoError(t, err)

	require.False(t, answer.OK)
	require.Equal(t, connectorapi.CommandRefused.Code().String(), answer.FaultCode)
	require.Equal(t, "FILE_EXISTS", answer.MachineReason,
		"the printer's own word for the refusal did not reach the caller")
	require.NotEmpty(t, answer.CommandID)

	t.Logf("observed: the refusal answers 200 ok=false %s / %q",
		answer.FaultCode, answer.MachineReason)
}

// TestARefusalIsStoredWithItsCode is the record half. The row a caller reads
// back has to separate the two refusals as well as the answer does.
func TestARefusalIsStoredWithItsCode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		serial string
		reason string
		code   string
	}{
		{
			name:   "no reason",
			serial: "SIM-UNEXPLAINED-ROW",
			code:   connectorapi.CommandRefusedWithoutReason.Code().String(),
		},
		{
			name:   "the printer's own word",
			serial: "SIM-FILE-EXISTS-ROW",
			reason: "FILE_EXISTS",
			code:   connectorapi.CommandRefused.Code().String(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			plane, bus := planeAndBus(t)
			id := arriveRefusing(t, plane, tc.serial, tc.reason)

			_, err := plane.Command(t.Context(), "user:"+config.DefaultPrincipal, id,
				httpapi.PrinterCommand{Verb: nozzleVerb})
			require.NoError(t, err)

			indexed := byType(bus.awaitCommands(t, 3))

			settled, ended := indexed[event.TypeDeviceCommandSettled]
			require.True(t, ended, "the refused command published no outcome")

			var outcome event.CommandSettled
			require.NoError(t, json.Unmarshal(settled.Data, &outcome))

			// The printer refused: it did not fail, and this host did not fail
			// either.
			require.Equal(t, event.CommandRefused, outcome.Status)
			require.False(t, outcome.OK)
			require.Equal(t, tc.code, outcome.FaultCode,
				"the stored row carries the wrong fault code for a refusal")
			require.Equal(t, tc.reason, outcome.MachineReason,
				"the stored row and the answer disagree about the machine's word")

			t.Logf("observed: the row reads %s / %s / %q",
				outcome.Status, outcome.FaultCode, outcome.MachineReason)
		})
	}
}

// TestAFailureIsNotStoredAsARefusal is the contrast, and it is the case the
// first fix folded into the refusal. A printer that took a command and could
// not carry it out did not decline it: the row says failed, the answer is a
// problem document rather than a 200 whose body says no, and the code is the
// failure's own.
func TestAFailureIsNotStoredAsARefusal(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus := planeAndBus(t)
		id := arriveFailing(t, plane, "SIM-FAILED-ROW")

		_, err := plane.Command(t.Context(), "user:"+config.DefaultPrincipal, id,
			httpapi.PrinterCommand{Verb: nozzleVerb})
		require.Error(t, err, "a command the printer could not carry out answered as if it had")
		require.True(t, fault.HasCode(err, connectorapi.CommandFailedWithoutReason.Code()),
			"the failure is carried as %v", err)

		indexed := byType(bus.awaitCommands(t, 3))

		settled, ended := indexed[event.TypeDeviceCommandSettled]
		require.True(t, ended, "the failed command published no outcome")

		var outcome event.CommandSettled
		require.NoError(t, json.Unmarshal(settled.Data, &outcome))

		require.Equal(t, event.CommandFailed, outcome.Status,
			"the printer tried and failed, and the row says the printer declined")
		require.Equal(t, connectorapi.CommandFailedWithoutReason.Code().String(), outcome.FaultCode)

		t.Logf("observed: the row reads %s / %s", outcome.Status, outcome.FaultCode)
	})
}
