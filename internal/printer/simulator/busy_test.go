package simulator_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// TestHomingReportsBusyThenIdle is the word a long move puts on the wire.
//
// The firmware reports BUSY for as long as a move runs and then returns to the
// state the move started from: an MK4 asked to home and level its bed held BUSY
// for 80 to 100 seconds (BUG-012), which is the whole of
// to_str(DeviceState)'s BUSY in
// Prusa-Firmware-Buddy/src/state/printer_state.cpp:563. A simulator that
// answered G28 and stayed IDLE never produces the word a host has to handle,
// which is how a state the firmware reports for a minute and a half went
// unenumerated.
func TestHomingReportsBusyThenIdle(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		messages := make(chan observed, 64)
		sessions := make(chan string, 8)

		host := newHost(t)
		host.onMessage = func(fingerprint string, payload map[string]any) {
			messages <- observed{fingerprint: fingerprint, payload: payload}
		}
		host.onSession = func(fingerprint string) { sessions <- fingerprint }

		printer, err := simulator.New(simulator.Config{
			Model:        "MK4",
			BaseURL:      host.baseURL(),
			Dial:         host.dial,
			IdlePeriod:   time.Second,
			BusyDuration: 3 * time.Second,
		})
		if err != nil {
			t.Fatalf("new printer: %v", err)
		}

		ctx, cancel := context.WithCancel(t.Context())

		if registerErr := printer.Register(ctx); registerErr != nil {
			t.Fatalf("register: %v", registerErr)
		}

		done := make(chan error, 1)
		go func() { done <- printer.Run(ctx) }()

		<-sessions

		if got := printer.State(); got != "IDLE" {
			t.Fatalf("the printer starts in %s, want IDLE", got)
		}

		if _, sendErr := host.sendGCode(printer.Fingerprint(), "G28 X"); sendErr != nil {
			t.Fatalf("home: send: %v", sendErr)
		}

		for _, name := range []string{"ACCEPTED", "FINISHED"} {
			if answer := awaitEvent(t, messages, name); answer["state"] != "BUSY" {
				t.Errorf("the %s for a homing move carries state %v, want BUSY", name, answer["state"])
			}
		}

		if got := printer.State(); got != "BUSY" {
			t.Errorf("the printer reports %s while it homes, want BUSY", got)
		}

		// The move ends on the clock and not on a command, so the state is read
		// off telemetry until it moves. Four frames at one second cover a move
		// of three.
		const frames = 4

		busy := 0
		state := ""

		for range frames {
			state, _ = awaitTelemetry(t, messages)["state"].(string)
			if state == "BUSY" {
				busy++

				continue
			}

			break
		}

		if busy == 0 {
			t.Error("no telemetry frame reported BUSY, so a host reading only telemetry never sees the move")
		}

		if state != "IDLE" {
			t.Errorf("the printer reports %s once the move is over, want IDLE", state)
		}

		if got := printer.State(); got != "IDLE" {
			t.Errorf("the printer reports %s once the move is over, want IDLE", got)
		}

		// A line that is not a long move says nothing about the state, so the
		// machine answers it without leaving IDLE.
		if _, sendErr := host.sendGCode(printer.Fingerprint(), "M104 S215"); sendErr != nil {
			t.Fatalf("set the nozzle: send: %v", sendErr)
		}

		for _, name := range []string{"ACCEPTED", "FINISHED"} {
			if answer := awaitEvent(t, messages, name); answer["state"] != "IDLE" {
				t.Errorf("a short line moved the state to %v on %s, want IDLE", answer["state"], name)
			}
		}

		// Mesh bed leveling is one payload of two lines, G28 and then G29, and
		// the firmware is BUSY for the whole of it.
		if _, sendErr := host.sendGCode(printer.Fingerprint(), "G28\r\nG29"); sendErr != nil {
			t.Fatalf("level the bed: send: %v", sendErr)
		}

		if answer := awaitEvent(t, messages, "ACCEPTED"); answer["state"] != "BUSY" {
			t.Errorf("a payload that homes and then levels carries state %v, want BUSY", answer["state"])
		}

		t.Logf("observed: G28 held the machine at BUSY for %d telemetry frames and it returned to IDLE", busy)

		cancel()
		<-done
		host.stop()
	})
}
