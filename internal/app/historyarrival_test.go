package app

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// The arrival window: what is stored about a device in the moment it appears.
//
// A history provider is an ordinary bus subscriber, so what it sees of a device
// begins when the subscription is authorized to see that device, and admitting
// the device is what makes it visible. The measured failure this file gates is
// the gap between the two: on 2026-09-01 a command sent 0.10 s after a printer
// appeared in the listing answered 200, published all three of its frames and
// stored none of them, while the same command at 1.20 s stored all three.
//
// The rule the fix holds to is that the first thing a caller can do to a device
// is already inside the window. A fleet powering up is when this bites, and the
// fleet is four hundred printers.

// commandNow sends one command through the published API and returns the id the
// server minted for it.
//
//nolint:unparam // the verb is a parameter because a case that sends another one is one line away.
func commandNow(t *testing.T, running *process, device, verb string) string {
	t.Helper()

	sent := running.call(t, http.MethodPost,
		"/api/v1/devices/"+device+"/commands", `{"verb":"`+verb+`"}`)
	require.Equal(t, http.StatusOK, sent.status, sent.body)

	var result httpapi.PrinterResult
	require.NoError(t, json.Unmarshal([]byte(sent.body), &result))
	require.NotEmpty(t, result.CommandID, "the server minted no command id")

	return result.CommandID
}

// dialInImmediately starts one simulated printer and returns the moment its id
// is in the listing, without waiting for telemetry behind it. That is the
// earliest instant a client could send it a command, which is the instant this
// file is about.
func dialInImmediately(t *testing.T, running *process, serial string) string {
	t.Helper()

	printer, err := simulator.New(simulator.Config{
		Model:   "MK4",
		Serial:  serial,
		BaseURL: "http://" + running.addr,
	})
	require.NoError(t, err)

	ctx, stop := context.WithCancel(context.WithoutCancel(t.Context()))
	done := make(chan error, 1)

	go func() { done <- printer.RegisterAndRun(ctx) }()

	t.Cleanup(func() {
		stop()
		<-done
	})

	var id string

	require.Eventually(t, func() bool {
		for _, one := range printers(t, running) {
			if one.Serial == serial {
				id = one.ID

				return true
			}
		}

		return false
	}, settle, time.Millisecond, "the simulated printer never appeared in the listing")

	return id
}

// TestACommandSentTheInstantADeviceAppearsIsStoredWhole is the gate on the
// arrival window.
//
// It sends the command with no delay at all: the device is read out of the
// listing and commanded on the next line, which is stricter than the 0.10 s
// that measured zero rows. All three frames of the invocation have to reach the
// store, the audit one included, because the store keeps what it is shown and
// the class restriction is applied on the way back out and not on the way in.
//
//nolint:tparallel // the subtests are one core and one simulated printer each; running the engines side by side doubles the machine's work for no more evidence, and forEachHistoryEngine says so.
func TestACommandSentTheInstantADeviceAppearsIsStoredWhole(t *testing.T) {
	t.Parallel()

	forEachHistoryEngine(t, func(t *testing.T, e historyEngine) {
		dir := t.TempDir()
		running := storingPastOn(t, dir, e)

		device := dialInImmediately(t, running, "SIM-ARRIVAL")
		commandID := commandNow(t, running, device, "core.command.beep")

		store := e.Open(t, dir)
		mine := ` AND ` + e.JSONText("payload", "command_id") + ` = '` + commandID + `'`

		var stored int

		require.Eventually(t, func() bool {
			stored = e.count(t, store, `SELECT count(*) FROM history_fact WHERE 1 = 1`+mine)

			return stored >= 3
		}, settle, 100*time.Millisecond,
			"the store holds %d of the three frames of command %s", stored, commandID)

		t.Logf("observed: on %s, a command sent the instant the device appeared stored %d frames",
			e.Name, stored)

		for _, want := range []string{
			event.TypeDeviceCommandRequested,
			event.TypeDeviceCommandAuthorized,
			event.TypeDeviceCommandSettled,
		} {
			require.Positive(t, e.count(t, store,
				`SELECT count(*) FROM history_fact WHERE type = '`+want+`'`+mine),
				"%s of command %s was never stored", want, commandID)
		}

		// And the projection folded it, which is the row a command listing
		// reads.
		require.Equal(t, 1, e.count(t, store,
			`SELECT count(*) FROM history_command WHERE command_id = '`+commandID+
				`' AND requested_us IS NOT NULL AND settled_us IS NOT NULL`),
			"the folded command row is missing a half")
	})
}

// TestAFleetCommandedAsItArrivesStoresEveryInvocation is the same gate for the
// case that actually bites.
//
// Registering a printer moves the authorization graph a generation, so a site
// powering up moves it once per machine, and what the feed is handed of each
// newly visible printer is bounded per origin by the generation that printer's
// own grant began at. A bound taken from the graph as it stands after the last
// machine landed is past every arrival but that one, and a command sent to the
// first printer while the fifth is still registering is exactly the frame such
// a bound throws away.
//
//nolint:tparallel // the subtests are one core and one simulated printer each; running the engines side by side doubles the machine's work for no more evidence, and forEachHistoryEngine says so.
func TestAFleetCommandedAsItArrivesStoresEveryInvocation(t *testing.T) {
	t.Parallel()

	forEachHistoryEngine(t, func(t *testing.T, e historyEngine) {
		dir := t.TempDir()
		running := storingPastOn(t, dir, e)

		serials := []string{"SIM-AW-1", "SIM-AW-2", "SIM-AW-3", "SIM-AW-4", "SIM-AW-5"}
		sent := make(map[string]string, len(serials))

		for _, serial := range serials {
			device := dialInImmediately(t, running, serial)
			sent[device] = commandNow(t, running, device, "core.command.beep")
		}

		store := e.Open(t, dir)

		for device, commandID := range sent {
			var stored int

			require.Eventually(t, func() bool {
				stored = e.count(t, store,
					`SELECT count(*) FROM history_fact WHERE `+
						e.JSONText("payload", "command_id")+` = '`+commandID+`'`)

				return stored >= 3
			}, settle, 100*time.Millisecond,
				"the store holds %d of the three frames of command %s on %s",
				stored, commandID, device)
		}

		t.Logf("observed: on %s, %d printers were commanded as each appeared and every "+
			"invocation stored all three of its frames", e.Name, len(serials))
	})
}
