package simulator_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
)

// A message the host saw, flattened to what an assertion needs.
type observed struct {
	fingerprint string
	payload     map[string]any
}

// The whole handshake, end to end, over net.Pipe and inside a synctest bubble:
// POST /p/register for a code, GET /p/register through two 202 answers to a
// token, GET /p/ws with the three frozen headers, then one command in and one
// terminal event out under the same command id.
//
// Every wait in here is on a channel or a virtual timer, so the test costs
// microseconds and the two 202 polls cost nothing at all.
func TestAPrinterRegistersConnectsAndAnswersACommand(t *testing.T) {
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
			Model:   "MK4",
			BaseURL: host.baseURL(),
			Dial:    host.dial,
		})
		if err != nil {
			t.Fatalf("new printer: %v", err)
		}

		host.holdRegistration(printer.Fingerprint(), 2)

		ctx, cancel := context.WithCancel(t.Context())

		if registerErr := printer.Register(ctx); registerErr != nil {
			t.Fatalf("register: %v", registerErr)
		}

		if got, want := printer.Token(), "TOKEN-"+printer.Fingerprint(); got != want {
			t.Fatalf("token is %q, want %q", got, want)
		}

		done := make(chan error, 1)
		go func() { done <- printer.Run(ctx) }()

		if got := <-sessions; got != printer.Fingerprint() {
			t.Fatalf("connected as %q, want %q", got, printer.Fingerprint())
		}

		id, err := host.send(printer.Fingerprint(), "SEND_INFO", nil)
		if err != nil {
			t.Fatalf("send SEND_INFO: %v", err)
		}

		info := awaitEvent(t, messages, "INFO")

		if got := commandID(info); got != id {
			t.Fatalf("INFO carries command_id %d, want %d", got, id)
		}

		data, _ := info["data"].(map[string]any)
		if got, want := data["printer_type"], printer.Model().DeviceTypeID; got != want {
			t.Fatalf("INFO reports printer_type %v, want %v", got, want)
		}

		// Telemetry is a text frame with no event key, which is what makes the
		// server read it as telemetry rather than as an event.
		telemetry := awaitTelemetry(t, messages)
		if got, want := telemetry["state"], "IDLE"; got != want {
			t.Fatalf("telemetry state is %v, want %v", got, want)
		}

		cancel()

		if runErr := <-done; runErr != nil {
			t.Fatalf("run: %v", runErr)
		}

		host.stop()
	})
}

// The property earlier research says is most likely to be lost, because
// nothing else records it: a command sent while the printer is away is
// delivered when it comes back.
func TestACommandSentWhileOfflineIsDeliveredOnReconnect(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		messages := make(chan observed, 64)
		sessions := make(chan string, 8)

		// The teardown is deferred so that a failure reports itself. A Fatalf
		// with the printer still running is what testing/synctest reports as
		// a deadlock in place of the failure.
		host := newHost(t)
		defer host.stop()

		host.onMessage = func(fingerprint string, payload map[string]any) {
			messages <- observed{fingerprint: fingerprint, payload: payload}
		}
		host.onSession = func(fingerprint string) { sessions <- fingerprint }

		printer, err := simulator.New(simulator.Config{
			Model:      "MK4",
			BaseURL:    host.baseURL(),
			Dial:       host.dial,
			IdlePeriod: time.Hour, // telemetry would drown the assertion
		})
		if err != nil {
			t.Fatalf("new printer: %v", err)
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		if registerErr := printer.Register(ctx); registerErr != nil {
			t.Fatalf("register: %v", registerErr)
		}

		done := make(chan error, 1)
		go func() { done <- printer.Run(ctx) }()

		defer func() {
			cancel()
			<-done
		}()

		<-sessions

		// The host reports a session when it accepts the upgrade, and the
		// printer's dial returns after that. GoOffline before the printer has
		// taken the connection on closes nothing, the printer then refuses the
		// connection it dialled without counting it, and the session count at
		// the end is one short.
		for printer.Sessions() < 1 {
			<-printer.Adopted()
		}

		printer.GoOffline()
		synctest.Wait()

		if host.connected(printer.Fingerprint()) {
			t.Fatal("the host still holds a connection for a printer that went offline")
		}

		id, err := host.send(printer.Fingerprint(), "SET_PRINTER_READY", nil)
		if err != nil {
			t.Fatalf("send while offline: %v", err)
		}

		if got := host.queuedFor(printer.Fingerprint()); got != 1 {
			t.Fatalf("%d commands queued for an offline printer, want 1", got)
		}

		select {
		case message := <-messages:
			t.Fatalf("an offline printer answered: %v", message.payload)
		default:
		}

		printer.GoOnline()

		if got := <-sessions; got != printer.Fingerprint() {
			t.Fatalf("reconnected as %q, want %q", got, printer.Fingerprint())
		}

		finished := awaitEvent(t, messages, "FINISHED")
		if got := commandID(finished); got != id {
			t.Fatalf("drained command answered under id %d, want %d", got, id)
		}

		if got, want := printer.Sessions(), uint64(2); got != want {
			t.Fatalf("the printer established %d sessions, want %d", got, want)
		}

		if got, want := printer.State(), "READY"; got != want {
			t.Fatalf("state after the drained command is %q, want %q", got, want)
		}
	})
}

// A command the model does not declare is refused on the wire, with the id it
// arrived under, and the printer is still usable afterwards. The refusal has to
// be a REJECTED event rather than a dropped frame: a host waiting on a
// command_id that never comes reaches timeout, and timeout and refusal are
// different facts.
func TestAModelRefusesACommandItDoesNotDeclare(t *testing.T) {
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

		cases := []struct {
			name    string
			command string
			kwargs  map[string]any
			reason  string
		}{
			{
				name:    "a chamber verb only the CoreOne family has",
				command: "SET_CHAMBER_TARGET_TEMP",
				kwargs:  map[string]any{"chamber.target_temp": 40},
				reason:  simulator.ReasonUnsupportedCommand,
			},
			{
				name:    "a verb nothing has",
				command: "LAUNCH_THE_MISSILES",
				reason:  simulator.ReasonUnsupportedCommand,
			},
			{
				name:    "a declared verb without its required argument",
				command: "START_PRINT",
				reason:  simulator.ReasonMissingArgument,
			},
			{
				name:    "a declared verb with an argument of the wrong kind",
				command: "SET_NOZZLE_TEMPERATURE",
				kwargs:  map[string]any{"nozzle_temperature": "hot"},
				reason:  simulator.ReasonInvalidArgument,
			},
			{
				name:    "a declared verb with an argument it does not declare",
				command: "SET_NOZZLE_TEMPERATURE",
				kwargs:  map[string]any{"nozzle_temperature": 215, "chamber.target_temp": 40},
				reason:  simulator.ReasonUnknownArgument,
			},
		}

		for _, testCase := range cases {
			id, sendErr := host.send(printer.Fingerprint(), testCase.command, testCase.kwargs)
			if sendErr != nil {
				t.Fatalf("%s: send: %v", testCase.name, sendErr)
			}

			rejected := awaitEvent(t, messages, "REJECTED")

			if got := commandID(rejected); got != id {
				t.Errorf("%s: rejected under id %d, want %d", testCase.name, got, id)
			}

			if got := rejected["reason"]; got != testCase.reason {
				t.Errorf("%s: reason is %v, want %s", testCase.name, got, testCase.reason)
			}
		}

		// The printer is still on the wire and still answering, which is what
		// separates a refusal from a fault.
		id, err := host.send(printer.Fingerprint(), "SET_NOZZLE_TEMPERATURE",
			map[string]any{"nozzle_temperature": 215})
		if err != nil {
			t.Fatalf("send after the refusals: %v", err)
		}

		if got := commandID(awaitEvent(t, messages, "FINISHED")); got != id {
			t.Fatalf("the supported command did not complete under id %d", id)
		}

		cancel()
		<-done
		host.stop()
	})
}

// The storage faults, which is where storage errors actually come from: a full
// device and a stick somebody pulled.
func TestStorageFaultsAreRefusedOnTheWire(t *testing.T) {
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

		printer.SetFreeSpace("/usb", 0)
		expect("a folder on a full stick", "CREATE_FOLDER",
			map[string]any{"path": "/usb/models"}, "REJECTED", simulator.ReasonStorageFull)

		// A full disk refuses a write and still serves a read, which is exactly
		// the asymmetry that makes out-of-space hard to find.
		expect("a print from a full stick", "START_PRINT",
			map[string]any{"path": "/usb/tangle.bgcode"}, "FINISHED", "")

		// A printer reads the file off its own storage, so a name the stick
		// does not hold is a print that cannot begin. A folder is not a file.
		expect("a print of a name the stick does not hold", "START_PRINT",
			map[string]any{"path": "/usb/nothing-here.bgcode"}, "REJECTED", simulator.ReasonFileNotFound)
		expect("a print of a folder", "START_PRINT",
			map[string]any{"path": "/usb/models"}, "REJECTED", simulator.ReasonFileNotFound)

		printer.RemoveStorage("/usb")
		expect("a print from a stick that is gone", "START_PRINT",
			map[string]any{"path": "/usb/tangle.bgcode"}, "REJECTED", simulator.ReasonMediaNotInserted)

		id, err := host.send(printer.Fingerprint(), "SEND_INFO", nil)
		if err != nil {
			t.Fatalf("send SEND_INFO: %v", err)
		}

		info := awaitEvent(t, messages, "INFO")
		if got := commandID(info); got != id {
			t.Fatalf("INFO carries command_id %d, want %d", got, id)
		}

		data, _ := info["data"].(map[string]any)
		if storages, _ := data["storages"].([]any); len(storages) != 0 {
			t.Fatalf("INFO still lists %d storage devices after the stick was pulled", len(storages))
		}

		// Above 4 GiB the size does not fit the uint32 the protocol carries, so
		// the transfer is refused rather than truncated.
		expect("a transfer at the uint32 ceiling", "START_INLINE_DOWNLOAD",
			map[string]any{
				"path": "/usb/huge.gcode", "hash": "abc", "team_id": 1, "orig_size": 1 << 32,
			},
			"REJECTED", simulator.ReasonTransferTooLarge)

		cancel()
		<-done
		host.stop()
	})
}

// A slow printer, so a host-side timeout and a breaker are reachable. The delay
// is virtual: thirty seconds of it costs nothing and is exact.
func TestASlowPrinterHoldsTheCommandForExactlyTheDelay(t *testing.T) {
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

		const delay = 30 * time.Second

		printer.SetResponseDelay(delay)

		start := time.Now()

		if _, sendErr := host.send(printer.Fingerprint(), "SET_PRINTER_READY", nil); sendErr != nil {
			t.Fatalf("send: %v", sendErr)
		}

		awaitEvent(t, messages, "FINISHED")

		if elapsed := time.Since(start); elapsed != delay {
			t.Fatalf("the answer took %s, want exactly %s", elapsed, delay)
		}

		cancel()
		<-done
		host.stop()
	})
}

// A folder listing takes ListingDelay and a command sent behind it waits for
// it, as on Buddy firmware. A file's own details answer at once.
func TestAListingHoldsTheCommandSlotForTheListingDelay(t *testing.T) {
	t.Parallel()

	const delay = 13 * time.Second

	cases := []struct {
		name string
		path string
		want time.Duration
	}{
		{name: "a folder waits the delay", path: "/usb", want: delay},
		{name: "a file answers at once", path: "/usb/tangle.bgcode", want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
					IdlePeriod:   time.Hour,
					ListingDelay: delay,
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

				start := time.Now()

				if _, sendErr := host.send(printer.Fingerprint(), "SEND_FILE_INFO",
					map[string]any{"path": tc.path}); sendErr != nil {
					t.Fatalf("send SEND_FILE_INFO: %v", sendErr)
				}

				if _, sendErr := host.send(printer.Fingerprint(), "SEND_INFO", nil); sendErr != nil {
					t.Fatalf("send SEND_INFO: %v", sendErr)
				}

				awaitEvent(t, messages, "FILE_INFO")

				if elapsed := time.Since(start); elapsed != tc.want {
					t.Fatalf("FILE_INFO on %s took %s, want exactly %s", tc.path, elapsed, tc.want)
				}

				awaitEvent(t, messages, "INFO")

				if elapsed := time.Since(start); elapsed != tc.want {
					t.Fatalf("the command behind the listing took %s, want exactly %s", elapsed, tc.want)
				}

				cancel()
				<-done
				host.stop()
			})
		})
	}
}

// awaitEvent returns the next message carrying the named event, ignoring
// telemetry frames that arrive first.
func awaitEvent(t *testing.T, messages <-chan observed, name string) map[string]any {
	t.Helper()

	for message := range messages {
		if message.payload["event"] == name {
			return message.payload
		}

		// A JOB_INFO the machine volunteered is not an answer to anything. It
		// arrives after the terminal event of every verb that moved a print, so
		// a case that waits for the next command's answer would otherwise fail
		// on the frame the case before it caused.
		if message.payload["event"] == "JOB_INFO" && message.payload["command_id"] == nil {
			continue
		}

		if event, ok := message.payload["event"]; ok {
			t.Fatalf("waiting for %s, got %v", name, event)
		}
	}

	t.Fatalf("the printer closed without sending %s", name)

	return nil
}

func awaitTelemetry(t *testing.T, messages <-chan observed) map[string]any {
	t.Helper()

	for message := range messages {
		if _, isEvent := message.payload["event"]; !isEvent {
			return message.payload
		}
	}

	t.Fatal("the printer closed without sending telemetry")

	return nil
}

func commandID(payload map[string]any) uint32 {
	id, _ := payload["command_id"].(float64)

	return uint32(id)
}

// A printer that drops mid-print and comes back. The job is what has to survive
// it: a reconnection that silently restarts the print, or loses it, is the
// failure a test double cannot show and a simulator can.
func TestAPrinterGoesOfflineMidJobAndComesBack(t *testing.T) {
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
			Model:          "MK4",
			BaseURL:        host.baseURL(),
			Dial:           host.dial,
			IdlePeriod:     time.Hour,
			PrintingPeriod: time.Second,
			JobFrames:      6,
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

		started, err := host.send(printer.Fingerprint(), "START_PRINT",
			map[string]any{"path": "/usb/tangle.bgcode"})
		if err != nil {
			t.Fatalf("send START_PRINT: %v", err)
		}

		if got := commandID(awaitEvent(t, messages, "FINISHED")); got != started {
			t.Fatalf("START_PRINT answered under the wrong id")
		}

		var progress float64

		for range 2 {
			telemetry := awaitTelemetry(t, messages)
			if got, want := telemetry["state"], "PRINTING"; got != want {
				t.Fatalf("telemetry state is %v, want %v", got, want)
			}

			progress, _ = telemetry["progress"].(float64)
		}

		if progress <= 0 {
			t.Fatalf("the print reported %v%% after two frames, want progress", progress)
		}

		printer.GoOffline()
		synctest.Wait()

		paused, err := host.send(printer.Fingerprint(), "PAUSE_PRINT", nil)
		if err != nil {
			t.Fatalf("send PAUSE_PRINT: %v", err)
		}

		printer.GoOnline()
		<-sessions

		if got := commandID(awaitEvent(t, messages, "FINISHED")); got != paused {
			t.Fatalf("the drained PAUSE_PRINT answered under the wrong id")
		}

		if got, want := printer.State(), "PAUSED"; got != want {
			t.Fatalf("state is %q, want %q", got, want)
		}

		resumed := awaitTelemetry(t, messages)
		if got, want := resumed["state"], "PAUSED"; got != want {
			t.Fatalf("telemetry after the reconnection reports %v, want %v", got, want)
		}

		if got, _ := resumed["progress"].(float64); got != progress {
			t.Fatalf("the job restarted across the reconnection: progress went from %v to %v",
				progress, got)
		}

		if got, want := resumed["job_id"].(float64), float64(started); got != want {
			t.Fatalf("the job id changed across the reconnection: %v, want %v", got, want)
		}

		cancel()
		<-done
		host.stop()
	})
}

// The wire shape the shipping connector actually sends for a chamber verb.
//
// Twelve capability verbs are rewritten into a single SET_VALUE frame before
// they leave the server, and profiles.json does not record that, so the verb a
// SET_VALUE means is recovered from the argument set. The property that has to
// survive that recovery is the one the capability intersection exists for: a
// CoreOne has a chamber and an MK4 does not, and neither of them is named
// anywhere in the simulator's own code.
func TestASetValueFrameResolvesAgainstTheModelThatDeclaredTheVerb(t *testing.T) {
	t.Parallel()

	cases := []struct {
		model string
		event string
	}{
		{model: "CoreOne", event: "FINISHED"},
		{model: "CoreOneL", event: "FINISHED"},
		{model: "MK4", event: "REJECTED"},
		{model: "MINI", event: "REJECTED"},
		{model: "XL", event: "REJECTED"},
	}

	for _, testCase := range cases {
		t.Run(testCase.model, func(t *testing.T) {
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
					Model:      testCase.model,
					BaseURL:    host.baseURL(),
					Dial:       host.dial,
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

				id, err := host.send(printer.Fingerprint(), "SET_VALUE",
					map[string]any{"chamber.target_temp": 40})
				if err != nil {
					t.Fatalf("send SET_VALUE: %v", err)
				}

				answer := awaitEvent(t, messages, testCase.event)
				if got := commandID(answer); got != id {
					t.Fatalf("answered under id %d, want %d", got, id)
				}

				cancel()
				<-done
				host.stop()
			})
		})
	}
}
