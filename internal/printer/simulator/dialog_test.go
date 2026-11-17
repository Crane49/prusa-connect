package simulator_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// TestADialogOnTheDisplayIsRaisedRepeatedAndAnswered is the wire form of a
// question a machine is stopped on.
//
// The firmware says what a dialog asks once, in the STATE_CHANGED event that
// raises it, and then repeats only its id in telemetry until it closes. A host
// that missed the event therefore knows a dialog is waiting and not what it
// says, which is why the id and the description travel differently. Nothing
// says a dialog closed except the printer leaving the state it was raised in.
func TestADialogOnTheDisplayIsRaisedRepeatedAndAnswered(t *testing.T) {
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
			Model:      "MK4",
			BaseURL:    host.baseURL(),
			Dial:       host.dial,
			IdlePeriod: time.Second,
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

		reply := func(name string, kwargs map[string]any, event, reason string) {
			t.Helper()

			id, sendErr := host.send(printer.Fingerprint(), "DIALOG_ACTION", kwargs)
			if sendErr != nil {
				t.Fatalf("%s: send: %v", name, sendErr)
			}

			answer := awaitEvent(t, messages, event)

			if got := commandID(answer); got != id {
				t.Errorf("%s: answered under id %d, want %d", name, got, id)
			}

			if reason != "" && answer["reason"] != reason {
				t.Errorf("%s: reason is %v, want %s", name, answer["reason"], reason)
			}
		}

		reply("a reply with nothing on the display",
			map[string]any{"dialog_id": 1, "button": "Retry"},
			"REJECTED", simulator.ReasonNoDialog)

		if !printer.RaiseDialog(simulator.Dialog{
			ID: 42, Code: "13501", Title: "Filament runout",
			Text: "Insert filament, then press Retry.", Buttons: []string{"Retry", "Cancel"},
		}) {
			t.Fatal("the dialog never reached the wire")
		}

		raised := awaitEvent(t, messages, "STATE_CHANGED")

		if raised["state"] != "ATTENTION" {
			t.Errorf("the dialog was raised in state %v, want ATTENTION", raised["state"])
		}

		if id, _ := raised["dialog_id"].(float64); int64(id) != 42 {
			t.Errorf("the raising event names dialog %v, want 42", raised["dialog_id"])
		}

		said, _ := raised["data"].(map[string]any)
		for key, want := range map[string]any{
			"code": "13501", "title": "Filament runout",
			"text": "Insert filament, then press Retry.",
		} {
			if said[key] != want {
				t.Errorf("the raising event carries %s=%v, want %v", key, said[key], want)
			}
		}

		if buttons, _ := said["buttons"].([]any); len(buttons) != 2 {
			t.Errorf("the raising event offers %v, want two buttons", said["buttons"])
		}

		// The id is repeated for as long as the dialog is up, which is what a
		// host that missed the event above has to learn from.
		if frame := awaitTelemetry(t, messages); frame["dialog_id"] == nil {
			t.Errorf("telemetry under a raised dialog carries no dialog_id: %v", frame)
		}

		reply("a reply naming another dialog",
			map[string]any{"dialog_id": 41, "button": "Retry"},
			"REJECTED", simulator.ReasonInvalidDialogID)
		reply("a reply naming a button the display does not offer",
			map[string]any{"dialog_id": 42, "button": "Yes"},
			"REJECTED", simulator.ReasonInvalidButton)

		reply("the dialog on the display",
			map[string]any{"dialog_id": 42, "button": "Retry"}, "FINISHED", "")

		if held := printer.Dialog(); held != nil {
			t.Errorf("the answered dialog is still on the display: %+v", held)
		}

		if frame := awaitTelemetry(t, messages); frame["dialog_id"] != nil {
			t.Errorf("telemetry still carries dialog_id after the dialog was answered: %v", frame)
		}

		reply("a second reply to a dialog that has gone",
			map[string]any{"dialog_id": 42, "button": "Retry"},
			"REJECTED", simulator.ReasonNoDialog)

		t.Logf("observed: the dialog was raised, repeated in telemetry, answered, and refused four ways")

		cancel()
		<-done
		host.stop()
	})
}
