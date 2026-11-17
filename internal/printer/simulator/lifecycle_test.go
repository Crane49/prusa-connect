package simulator_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// TestAVerbThatNeedsAJobIsRefusedWhenThereIsNone holds the simulator to the six
// state rules a printer has.
//
// A machine with nothing on the bed has no print to pause, resume or stop, and
// a machine that is printing has one job and does not take a second. Both
// answers are a refusal, and the firmware answers each of the six with
// REJECTED. A simulator that accepts them teaches every test
// above it that a printer does, and the state it reports afterwards is one no
// machine reaches: an idle printer told to stop reported STOPPED.
func TestAVerbThatNeedsAJobIsRefusedWhenThereIsNone(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		messages := make(chan observed, 64)
		sessions := make(chan string, 8)

		host := newHost(t)
		host.onMessage = func(fingerprint string, payload map[string]any) {
			messages <- observed{fingerprint: fingerprint, payload: payload}
		}
		host.onSession = func(fingerprint string) { sessions <- fingerprint }

		// Both cadences are an hour, so the job advances only when a command
		// moves it and the case reads the state it set rather than one a
		// telemetry frame reached on its own.
		printer, err := simulator.New(simulator.Config{
			Model:          "MK4",
			BaseURL:        host.baseURL(),
			Dial:           host.dial,
			IdlePeriod:     time.Hour,
			PrintingPeriod: time.Hour,
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

		expect := func(name, command string, kwargs map[string]any, event, reason, state string) {
			t.Helper()

			id, sendErr := host.send(printer.Fingerprint(), command, kwargs)
			if sendErr != nil {
				t.Fatalf("%s: send: %v", name, sendErr)
			}

			answer := awaitEvent(t, messages, event)

			if got := commandID(answer); got != id {
				t.Errorf("%s: answered under id %d, want %d", name, got, id)
			}

			if answer["reason"] != reason && reason != "" {
				t.Errorf("%s: reason is %v, want %s", name, answer["reason"], reason)
			}

			if answer["state"] != state {
				t.Errorf("%s: the printer reports %v, want %s", name, answer["state"], state)
			}
		}

		job := map[string]any{"path": "/usb/tangle.bgcode"}

		// Nothing is printing, so the three verbs that act on a job have
		// nothing to act on.
		expect("a pause with nothing printing", "PAUSE_PRINT", nil,
			"REJECTED", simulator.ReasonInvalidState, "IDLE")
		expect("a resume with nothing paused", "RESUME_PRINT", nil,
			"REJECTED", simulator.ReasonInvalidState, "IDLE")
		expect("a stop with no job", "STOP_PRINT", nil,
			"REJECTED", simulator.ReasonInvalidState, "IDLE")

		expect("a print of a file the stick holds", "START_PRINT", job,
			"FINISHED", "", "PRINTING")

		// One job per machine. A second START_PRINT would otherwise replace the
		// file on the bed and report the replacement as a success.
		expect("a second print while one is running", "START_PRINT", job,
			"REJECTED", simulator.ReasonInvalidState, "PRINTING")
		expect("ready while a job is running", "SET_PRINTER_READY", nil,
			"REJECTED", simulator.ReasonInvalidState, "PRINTING")
		expect("idle while a job is running", "SET_IDLE", nil,
			"REJECTED", simulator.ReasonInvalidState, "PRINTING")

		expect("a pause over a running job", "PAUSE_PRINT", nil,
			"FINISHED", "", "PAUSED")
		expect("a second pause over a paused job", "PAUSE_PRINT", nil,
			"REJECTED", simulator.ReasonInvalidState, "PAUSED")
		expect("a resume over a paused job", "RESUME_PRINT", nil,
			"FINISHED", "", "PRINTING")

		expect("a stop over a running job", "STOP_PRINT", nil,
			"FINISHED", "", "STOPPED")
		expect("a stop over a job that has gone", "STOP_PRINT", nil,
			"REJECTED", simulator.ReasonInvalidState, "STOPPED")

		// The bed is free again, so the machine takes the verbs it refused
		// while a job was on it.
		expect("ready with the bed free", "SET_PRINTER_READY", nil,
			"FINISHED", "", "READY")
		expect("a print from ready", "START_PRINT", job,
			"FINISHED", "", "PRINTING")

		t.Logf("observed: the printer refused eight verbs the state does not allow and took six it does")

		cancel()
		<-done
		host.stop()
	})
}
