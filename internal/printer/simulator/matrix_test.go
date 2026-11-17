package simulator_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// The storage faults, over the wire, for every model the profiles cover.
//
// test/fleet drives the two the driver can reach, a full device and a device
// that is gone. Two more are only reachable here: a device mounted read-only,
// which nothing in the capability vocabulary can ask for, and a transfer at the
// uint32 ceiling, whose verb has no wire form in the driver. Out of space is
// reported at the write and nowhere else, so the level a fault can be asserted
// at is worth being exact about.
//
// The model list is read from the profile data, so a model added there is
// covered here without a line of Go changing.
func TestEveryModelRefusesTheStorageFaultsOnTheWire(t *testing.T) {
	t.Parallel()

	models, err := simulator.Models()
	if err != nil {
		t.Fatalf("load the models: %v", err)
	}

	if len(models) == 0 {
		t.Fatal("the simulator covers no models, so this suite drove nothing")
	}

	t.Logf("driving the storage faults across %d models: %v", len(models), models)

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				storageFaults(t, model)
			})
		})
	}
}

// storageFaults is one model's whole storage battery: a healthy device, a full
// one, a device that is not there, and a transfer past the uint32 ceiling.
func storageFaults(t *testing.T, model string) {
	t.Helper()

	messages := make(chan observed, 64)
	sessions := make(chan string, 8)

	host := newHost(t)
	host.onMessage = func(fingerprint string, payload map[string]any) {
		messages <- observed{fingerprint: fingerprint, payload: payload}
	}
	host.onSession = func(fingerprint string) { sessions <- fingerprint }

	printer, err := simulator.New(simulator.Config{
		Model:   model,
		BaseURL: host.baseURL(),
		Dial:    host.dial,
		// The idle cadence is turned off so the only frames on the wire are the
		// answers this test is reading.
		IdlePeriod: time.Hour,
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

	expect := func(name, command string, kwargs map[string]any, event, reason string) {
		t.Helper()

		id, sendErr := host.send(printer.Fingerprint(), command, kwargs)
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

	expect("a folder on a healthy stick", "CREATE_FOLDER",
		map[string]any{"path": "/usb/models"}, "FINISHED", "")

	if !printer.SetFreeSpace("/usb", 0) {
		t.Fatalf("%s reported no /usb to fill", model)
	}

	expect("a folder on a full stick", "CREATE_FOLDER",
		map[string]any{"path": "/usb/models"}, "REJECTED", simulator.ReasonStorageFull)

	// A full disk refuses a write and still serves a read, which is exactly the
	// asymmetry that makes out of space hard to find.
	expect("a print from a full stick", "START_PRINT",
		map[string]any{"path": "/usb/tangle.bgcode"}, "FINISHED", "")

	// A printer reads the file off its own storage, so a name the stick does
	// not hold is a print that cannot begin, on every model.
	expect("a print of a name the stick does not hold", "START_PRINT",
		map[string]any{"path": "/usb/nothing-here.bgcode"}, "REJECTED", simulator.ReasonFileNotFound)

	if !printer.RemoveStorage("/usb") {
		t.Fatalf("%s reported no /usb to pull", model)
	}

	expect("a print from a stick that is gone", "START_PRINT",
		map[string]any{"path": "/usb/tangle.bgcode"}, "REJECTED", simulator.ReasonMediaNotInserted)

	expect("a folder on a stick that is gone", "CREATE_FOLDER",
		map[string]any{"path": "/usb/models"}, "REJECTED", simulator.ReasonMediaNotInserted)

	// Above 4 GiB the size does not fit the uint32 the protocol carries, so the
	// transfer is refused rather than truncated.
	expect("a transfer at the uint32 ceiling", "START_INLINE_DOWNLOAD",
		map[string]any{
			"path": "/usb/huge.gcode", "hash": "abc", "team_id": 1, "orig_size": 1 << 32,
		},
		"REJECTED", simulator.ReasonTransferTooLarge)

	printer.InsertStorage(simulator.Storage{Mountpoint: "/usb", Type: "USB", ReadOnly: true, FreeSpace: 1 << 20})

	expect("a folder on a read-only stick", "CREATE_FOLDER",
		map[string]any{"path": "/usb/models"}, "REJECTED", simulator.ReasonStorageReadOnly)

	cancel()
	<-done
	host.stop()
}

// TestEveryModelHoldsACommandForExactlyTheResponseDelay is the slow-printer
// fault across the matrix, and it covers both frame kinds.
//
// Nineteen of the fifty-seven verbs leave as gcode rather than as a JSON
// command. A delay that only reached the JSON path
// would leave a third of the command surface unable to make a host-side timeout
// reachable, and the fault injector would look armed while being armed for two
// thirds of the wire.
func TestEveryModelHoldsACommandForExactlyTheResponseDelay(t *testing.T) {
	t.Parallel()

	models, err := simulator.Models()
	if err != nil {
		t.Fatalf("load the models: %v", err)
	}

	const delay = 30 * time.Second

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				messages := make(chan observed, 64)
				sessions := make(chan string, 8)

				host := newHost(t)
				host.onMessage = func(fingerprint string, payload map[string]any) {
					messages <- observed{fingerprint: fingerprint, payload: payload}
				}
				host.onSession = func(fingerprint string) { sessions <- fingerprint }

				printer, newErr := simulator.New(simulator.Config{
					Model: model, BaseURL: host.baseURL(), Dial: host.dial, IdlePeriod: time.Hour,
				})
				if newErr != nil {
					t.Fatalf("new printer: %v", newErr)
				}

				ctx, cancel := context.WithCancel(t.Context())

				if registerErr := printer.Register(ctx); registerErr != nil {
					t.Fatalf("register: %v", registerErr)
				}

				done := make(chan error, 1)
				go func() { done <- printer.Run(ctx) }()

				<-sessions

				printer.SetResponseDelay(delay)

				for _, frame := range []struct {
					name  string
					first string
					send  func() (uint32, error)
				}{
					{
						name:  "a json command",
						first: "FINISHED",
						send: func() (uint32, error) {
							return host.send(printer.Fingerprint(), "SET_PRINTER_READY", nil)
						},
					},
					{
						name:  "a gcode command",
						first: "ACCEPTED",
						send: func() (uint32, error) {
							return host.sendGCode(printer.Fingerprint(), "M18")
						},
					},
				} {
					started := time.Now()

					if _, sendErr := frame.send(); sendErr != nil {
						t.Fatalf("%s: send: %v", frame.name, sendErr)
					}

					awaitEvent(t, messages, frame.first)

					if waited := time.Since(started); waited != delay {
						t.Errorf("%s: answered after %s, want exactly %s", frame.name, waited, delay)
					}
				}

				t.Logf("observed: %s held both frame kinds for exactly %s", model, delay)

				cancel()
				<-done
				host.stop()
			})
		})
	}
}
