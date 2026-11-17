package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// overdueSession is a driver that stopped waiting for its printer. Its Do
// carries context.DeadlineExceeded and the caller's own context is untouched,
// which is the shape a per-command deadline inside a driver has.
type overdueSession struct {
	countedSession
}

func (s *overdueSession) Capabilities() (connectorapi.Set, uint64) { return talkingBaseline(), 1 }

func (s *overdueSession) Do(context.Context, connectorapi.Command) (connectorapi.Result, error) {
	return connectorapi.Result{}, fmt.Errorf(
		"the printer did not answer inside the command deadline: %w", context.DeadlineExceeded)
}

// arriveOverdue puts a printer in the plane whose commands end on the driver's
// own deadline.
func arriveOverdue(t *testing.T, plane *devices, serial string) string {
	t.Helper()

	host := plane.Arrivals("test", talkingBaseline(),
		capability.NewResolver(profile.Registry{}, discard()), "")

	session := &overdueSession{
		countedSession: countedSession{
			identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: serial},
			patches:  make(chan connectorapi.StatePatch),
		},
	}

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { close(session.patches) })

	return plane.idOf(session.identity)
}

// A deadline the driver owns is recorded as a timeout, the same word a caller's
// own deadline gets.
//
// The two are one fact for whoever reads the record afterwards: the command was
// outstanding, nothing came back, and the wait was ended by a clock rather than
// by the printer. Recording the driver's as a plain failure would say the
// command was answered with a fault, which is what this side does not know.
func TestADeadlineTheDriverOwnsIsRecordedAsATimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		plane, bus := planeAndBus(t)
		id := arriveOverdue(t, plane, "SIM-OVERDUE")

		// The caller's context carries no deadline, so the word in the record
		// can only have come off the driver's error.
		_, err := plane.Command(t.Context(), "user:"+config.DefaultPrincipal, id,
			httpapi.PrinterCommand{Verb: nozzleVerb})
		require.Error(t, err)

		frames := bus.awaitCommands(t, 3)
		settled, ended := byType(frames)[event.TypeDeviceCommandSettled]
		require.True(t, ended, "a command that ran out of time published no outcome")

		var outcome event.CommandSettled
		require.NoError(t, json.Unmarshal(settled.Data, &outcome))

		require.Equal(t, event.CommandTimedOut, outcome.Status)

		// The printer may have carried it out, so the record says it does not
		// know rather than reporting a command that did not happen.
		require.True(t, outcome.OutcomeUnknown,
			"a command nothing answered was recorded as an observed outcome")

		t.Logf("observed: the command settled as %s, outcome known: %v",
			outcome.Status, !outcome.OutcomeUnknown)
	})
}

// The word each ending takes, held at the one function that decides it.
//
// A caller that went away and a deadline that expired are different answers to
// "why did this stop", and a driver deadline landing on top of a caller that
// went away is still the caller going away: that is what an operator is looking
// at.
func TestTheWordEachCommandEndingTakes(t *testing.T) {
	t.Parallel()

	overdue := fmt.Errorf("the driver waited long enough: %w", context.DeadlineExceeded)

	cancelled, stop := context.WithCancel(t.Context())
	stop()

	expired, release := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	t.Cleanup(release)

	notSent := errors.New("the driver could not put the command on the wire")

	cases := []struct {
		name    string
		ctx     context.Context //nolint:containedctx // the case is one call's arguments.
		result  connectorapi.Result
		err     error
		status  string
		unknown bool
	}{
		{
			name: "a printer that answered", ctx: t.Context(),
			result: connectorapi.Result{OK: true}, status: event.CommandSucceeded,
		},
		{
			name: "a printer that said no", ctx: t.Context(),
			result: connectorapi.Result{OK: false}, status: event.CommandRefused,
		},
		{
			name: "a driver that could not send it", ctx: t.Context(),
			err: notSent, status: event.CommandFailed,
		},
		{
			name: "a deadline the driver owns", ctx: t.Context(),
			err: overdue, status: event.CommandTimedOut, unknown: true,
		},
		{
			name: "a printer that lost its connection after the write", ctx: t.Context(),
			err:    connectorapi.CommandUnanswered.New("the printer disconnected before answering"),
			status: event.CommandFailed, unknown: true,
		},
		{
			name: "the caller's own deadline", ctx: expired,
			err: expired.Err(), status: event.CommandTimedOut, unknown: true,
		},
		{
			name: "a caller that went away", ctx: cancelled,
			err: cancelled.Err(), status: event.CommandAbandoned, unknown: true,
		},
		{
			name: "a caller that went away while the driver's deadline expired", ctx: cancelled,
			err: overdue, status: event.CommandAbandoned, unknown: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			status, unknown := settleStatus(testCase.ctx, testCase.result, testCase.err)
			require.Equal(t, testCase.status, status)
			require.Equal(t, testCase.unknown, unknown)
		})
	}
}
