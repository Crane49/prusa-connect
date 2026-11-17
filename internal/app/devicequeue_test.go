package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/simulator"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// readyVerb moves the printer's own state, so whether it ran is readable from
// the state tree and not only from the answer the caller got. A verb that
// changed nothing would leave "was it sent" an inference.
const readyVerb = "core.command.set_printer_ready"

// TestACommandQueuedForAPrinterThatComesBackReflashedIsRefusedAndSaysWhy is the
// the old backlog row, on the real composition root.
//
// A capability set is fixed for a session's life, so a printer that comes back
// reporting other firmware ends the session its commands were queued on. The
// drop is the epoch guard doing its job. What a driver must not do beside it is
// answer the caller core.internal/UNEXPECTED, which is a 500 with no params and
// no retry hint and says nothing about whether the printer ran the command.
//
// The assertion is on the bytes the API answered with. What the caller is owed
// is a code it can branch on, the two firmware versions, and a state tree that
// proves the machine never saw the command.
func TestACommandQueuedForAPrinterThatComesBackReflashedIsRefusedAndSaysWhy(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	first := runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-REFLASHED",
		Firmware: firmwareInsideTheProfile,
	}, "")

	listed := printers(t, running)
	require.Len(t, listed, 1)

	id := listed[0].ID

	takeOffTheWire(t, running, first, id)

	sent := queueCommand(t, running, id, readyVerb)

	// The printer comes back on firmware that resolves to another capability
	// set, which is what ends the session the command is queued on.
	runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-REFLASHED",
		Firmware: firmwareBeforeAnyProfile,
	}, first.printer.Token())

	got := awaitAnswer(t, sent)

	t.Logf("observed: the queued command answered %d %s", got.status, got.body)

	require.Equal(t, http.StatusConflict, got.status, got.body)

	problem := problemOf(t, got.body)
	require.Equal(t, connectorapi.PrinterChanged.Code().String(), problem.Code,
		"a command the server declined to send reported a failure it does not describe")
	require.True(t, problem.Retryable,
		"the command was never sent, so a caller that reads the new capability set may send it again")
	require.Equal(t, firmwareInsideTheProfile, problem.Params["previous_firmware"],
		"the refusal does not name the firmware the command was composed for")
	require.Equal(t, firmwareBeforeAnyProfile, problem.Params["firmware"],
		"the refusal does not name the firmware the printer came back on")
	require.Equal(t, readyVerb, problem.Params["verb"])

	// The other half of the same claim: the machine that came back never saw
	// it. set_printer_ready moves a printer to READY and this one is still idle.
	//
	// Sampled in this goroutine rather than through require.Never, which runs
	// the condition in one of its own: a sample still in flight when the case
	// ends reaches a server the cleanup has already stopped.
	for range 20 {
		require.NotEqual(t, "READY", stateOf(t, running, id),
			"the refusal said the command was not sent and the printer ran it")

		time.Sleep(50 * time.Millisecond)
	}
}

// TestACommandQueuedForAPrinterThatComesBackUnchangedIsCarriedOut is the other
// case, and it is the rule earlier research says is most likely to be lost:
// a session outlives an absence so its queue does.
//
// It is here beside the refusal because the two are one decision. A fix that
// answered every reconnection with a refusal would satisfy "say something" and
// break the queue-and-drain rule, and nothing but this case would notice.
func TestACommandQueuedForAPrinterThatComesBackUnchangedIsCarriedOut(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), "core:\n  log:\n    level: error\n")

	first := runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-SAMEFLASH",
		Firmware: firmwareInsideTheProfile,
	}, "")

	listed := printers(t, running)
	require.Len(t, listed, 1)

	id := listed[0].ID

	takeOffTheWire(t, running, first, id)

	sent := queueCommand(t, running, id, readyVerb)

	runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-SAMEFLASH",
		Firmware: firmwareInsideTheProfile,
	}, first.printer.Token())

	got := awaitAnswer(t, sent)

	t.Logf("observed: the queued command answered %d %s", got.status, got.body)

	require.Equal(t, http.StatusOK, got.status, got.body)
	require.Contains(t, got.body, `"ok":true`)

	require.Eventually(t, func() bool {
		return stateOf(t, running, id) == "READY"
	}, settle, 25*time.Millisecond,
		"the command was answered and the printer never moved, so nothing reached the machine")
}

// queueCommand sends one command for a printer that is away and hands back the
// channel the answer arrives on. The command waits in the driver's queue, which
// is what the two cases above end in different ways.
func queueCommand(t *testing.T, running *process, id, verb string) chan answer {
	t.Helper()

	sent := make(chan answer, 1)
	ctx := t.Context()

	// postCommand and not running.call: the request runs on a goroutine of its
	// own, and an assertion outside the test's goroutine is not one testify can
	// report.
	go func() { sent <- postCommand(ctx, running.addr, id, verb) }()

	// The command has to be waiting before the printer comes back, or the case
	// is a race and proves nothing about a queued command. An answer that
	// arrives while the printer is away is one this test is not measuring.
	require.Never(t, func() bool {
		return len(sent) > 0
	}, 500*time.Millisecond, 25*time.Millisecond,
		"the command was answered while the printer was away, so nothing was ever queued")

	return sent
}

// postCommand sends one command over HTTP and reports what came back. Anything
// that goes wrong on the way is reported as the body, because this runs off the
// test's goroutine and has nothing to fail with.
func postCommand(ctx context.Context, addr, id, verb string) answer {
	body := `{"verb":"` + verb + `","args":{}}`

	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+addr+"/api/v1/devices/"+id+"/commands", strings.NewReader(body))
	if err != nil {
		return answer{body: err.Error()}
	}

	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return answer{body: err.Error()}
	}

	defer func() { _ = response.Body.Close() }()

	read, err := io.ReadAll(response.Body)
	if err != nil {
		return answer{status: response.StatusCode, body: err.Error()}
	}

	return answer{status: response.StatusCode, body: string(read)}
}

// awaitAnswer takes the queued command's answer. A command that never settles is
// the failure this file exists to pin, so there is a budget on it.
func awaitAnswer(t *testing.T, sent chan answer) answer {
	t.Helper()

	select {
	case got := <-sent:
		return got
	case <-time.After(settle):
		t.Fatalf("the queued command was never answered inside %s, so it was dropped in silence", settle)

		return answer{}
	}
}

// stateOf reads the printer's own state out of the addressable state tree,
// which is where "did the machine run it" is readable.
func stateOf(t *testing.T, running *process, id string) string {
	t.Helper()

	var state string
	if raw, held := fetch(t, running, id).State["/state"]; held {
		require.NoError(t, json.Unmarshal(raw, &state))
	}

	return state
}

// problem is the RFC 9457 document this API answers a refusal with, read as a
// client reads it.
type problem struct {
	Code      string            `json:"code"`
	Params    map[string]string `json:"params"`
	Retryable bool              `json:"retryable"`
}

func problemOf(t *testing.T, body string) problem {
	t.Helper()

	var out problem
	require.NoError(t, json.Unmarshal([]byte(body), &out), "the refusal is not a problem document: %s", body)

	return out
}

// TestASaturatedPrinterAnswersARegisteredCodeAndNotAServerFault is CL-08 on the
// real composition root.
//
// A printer that is away holds its commands in a bounded queue, and a command
// offered past the bound is refused. A bare sentinel for that refusal reaches a
// caller as core.internal/UNEXPECTED: a 500 with no params, no retry hint and
// the wrong story, because the server did not fail and the command was never
// sent. Measured against a binary with the bare sentinel, 60 commands to an
// away printer answered 33 with 200 and 27 with that 500.
//
// The queue is one deep here so the case is three requests rather than sixty.
func TestASaturatedPrinterAnswersARegisteredCodeAndNotAServerFault(t *testing.T) {
	t.Parallel()

	running := start(t, t.TempDir(), strings.Join([]string{
		"core:",
		"  log:",
		"    level: error",
		"  connectors:",
		"    prusa-xbuddy:",
		"      type: xbuddy",
		"      config:",
		"        queue_depth: 1",
		"",
	}, "\n"))

	printer := runSimulator(t, running, simulator.Config{
		Model:    "MK4",
		Serial:   "SIM-SATURATED",
		Firmware: firmwareInsideTheProfile,
	}, "")

	listed := printers(t, running)
	require.Len(t, listed, 1)

	id := listed[0].ID

	takeOffTheWire(t, running, printer, id)

	// Every command is offered on a goroutine, because one that is queued does
	// not answer until the printer comes back: a case that posted in line would
	// block on the first and never reach the refusal. The queue is one deep, so
	// at most two are held, the one the pump took and the one in the slot, and
	// the rest are the refusal this case is about.
	const offered = 6

	answers := make(chan answer, offered)
	ctx := t.Context()

	for range offered {
		go func() { answers <- postCommand(ctx, running.addr, id, readyVerb) }()
	}

	var refused answer

	for range offered {
		select {
		case got := <-answers:
			if got.status != http.StatusOK {
				refused = got
			}
		case <-time.After(settle):
			// The rest are queued and waiting for a printer that never comes
			// back, which is the queue-and-drain rule working.
		}

		if refused.status != 0 {
			break
		}
	}

	require.NotZero(t, refused.status, "no command offered past the queue's depth was refused")

	t.Logf("observed: the overflowing command answered %d %s", refused.status, refused.body)

	require.Equal(t, http.StatusConflict, refused.status, refused.body)

	refusal := problemOf(t, refused.body)
	require.Equal(t, connectorapi.CommandQueueFull.Code().String(), refusal.Code,
		"a full command queue reached the caller as a fault the server does not describe")
	require.True(t, refusal.Retryable,
		"nothing was sent, so the caller may send it again when the queue drains")
	require.Equal(t, readyVerb, refusal.Params["verb"])
}
