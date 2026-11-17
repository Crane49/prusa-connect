package xbuddy

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// The firmware answers a G frame with ACCEPTED, goes on running the G-code, and
// refuses every command with no reason until it sends FINISHED
// (Prusa-Firmware-Buddy src/connect/planner.cpp).
//
// The caller of Home has its answer on ACCEPTED. What waits is the next
// command, and it goes out when the printer says G28 ended.
func TestTheNextCommandWaitsForTheRunningGCodeToFinish(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)
	frames := readFrames(t, printer.client)

	home := printer.offer(t, connectorapi.Command{Verb: VerbHome})
	homeID := takeID(t, frames)
	printer.event(t, "ACCEPTED", homeID)

	if result, err := printer.await(t, home); err != nil || !result.OK {
		t.Fatalf("home settled with %+v and %v, and an ACCEPTED is the answer its caller waits for", result, err)
	}

	beep := printer.start(t)

	select {
	case frame := <-frames:
		t.Fatalf("the session sent %q while the printer was still homing, and the firmware refuses "+
			"that with no reason", frame)
	case <-time.After(quietWindow):
	}

	printer.event(t, "FINISHED", homeID)

	beepID := takeID(t, frames)
	printer.event(t, "ACCEPTED", beepID)

	if result, err := printer.await(t, beep); err != nil || !result.OK {
		t.Fatalf("the beep settled with %+v and %v once homing had finished", result, err)
	}

	// FINISHED ends the beep as well, so the next command is not held behind it.
	printer.event(t, "FINISHED", beepID)

	third := printer.start(t)
	thirdID := takeID(t, frames)
	printer.event(t, "FINISHED", thirdID)

	if _, err := printer.await(t, third); err != nil {
		t.Fatalf("a command after a finished beep failed: %v", err)
	}
}

// A JSON command is finished by its answer, so nothing waits behind it.
func TestAJSONCommandHoldsNothingBehindIt(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)
	frames := readFrames(t, printer.client)

	// Buddy firmware answers a pause with FINISHED alone; a printer on the
	// Prusa Connect SDK sends ACCEPTED first, which answers nothing.
	pause := printer.offer(t, connectorapi.Command{Verb: VerbPausePrint})
	pauseID := takeID(t, frames)
	printer.event(t, "ACCEPTED", pauseID)
	printer.event(t, "FINISHED", pauseID)

	if _, err := printer.await(t, pause); err != nil {
		t.Fatalf("pause failed: %v", err)
	}

	beep := printer.start(t)
	printer.event(t, "FINISHED", takeID(t, frames))

	if _, err := printer.await(t, beep); err != nil {
		t.Fatalf("a beep after a finished pause failed: %v", err)
	}
}

// A command whose caller gave up while it waited behind a running G-code is
// never written to the printer.
func TestACommandGivenUpInTheQueueIsNotSent(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)
	frames := readFrames(t, printer.client)

	home := printer.offer(t, connectorapi.Command{Verb: VerbHome})
	homeID := takeID(t, frames)
	printer.event(t, "ACCEPTED", homeID)

	if _, err := printer.await(t, home); err != nil {
		t.Fatalf("home failed: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	abandoned := make(chan error, 1)

	go func() {
		_, err := printer.session.Do(ctx, connectorapi.Command{Verb: VerbBeep})
		abandoned <- err
	}()

	expectNoFrame(t, frames, "while the printer was still homing")
	cancel()

	if err := <-abandoned; !errors.Is(err, context.Canceled) {
		t.Fatalf("the abandoned beep returned %v, want context.Canceled", err)
	}

	printer.event(t, "FINISHED", homeID)
	expectNoFrame(t, frames, "for a beep whose caller had already gone")

	next := printer.start(t)
	printer.event(t, "FINISHED", takeID(t, frames))

	if _, err := printer.await(t, next); err != nil {
		t.Fatalf("the command after the abandoned one failed: %v", err)
	}
}

// A printer that never says the G-code ended holds the next command for the
// command deadline and no longer.
func TestTheHoldEndsAtTheCommandDeadline(t *testing.T) {
	t.Parallel()

	const deadline = time.Second

	printer := standHostileWith(t, nil, deadline)
	frames := readFrames(t, printer.client)

	home := printer.offer(t, connectorapi.Command{Verb: VerbHome})
	homeID := takeID(t, frames)
	accepted := time.Now()
	printer.event(t, "ACCEPTED", homeID)

	if _, err := printer.await(t, home); err != nil {
		t.Fatalf("home failed: %v", err)
	}

	beep := printer.start(t)
	beepID := takeID(t, frames)

	if waited := time.Since(accepted); waited < deadline {
		t.Fatalf("the beep went out %v after ACCEPTED, inside the %v deadline", waited, deadline)
	}

	printer.event(t, "FINISHED", beepID)

	if _, err := printer.await(t, beep); err != nil {
		t.Fatalf("the beep sent after the hold failed: %v", err)
	}
}

// A G-code command whose caller gave up before ACCEPTED is still running on
// the printer once it is accepted, so the next command waits for it all the
// same.
func TestAnAbandonedGCodeStillHoldsTheQueue(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)
	frames := readFrames(t, printer.client)

	ctx, cancel := context.WithCancel(t.Context())
	abandoned := make(chan error, 1)

	go func() {
		_, err := printer.session.Do(ctx, connectorapi.Command{Verb: VerbHome})
		abandoned <- err
	}()

	homeID := takeID(t, frames)
	cancel()

	if err := <-abandoned; !errors.Is(err, context.Canceled) {
		t.Fatalf("the abandoned home returned %v, want context.Canceled", err)
	}

	beep := printer.start(t)
	expectNoFrame(t, frames, "before the printer answered the G28 already on the wire")

	printer.event(t, "ACCEPTED", homeID)
	expectNoFrame(t, frames, "while the printer was still homing")

	printer.event(t, "FINISHED", homeID)

	beepID := takeID(t, frames)
	printer.event(t, "FINISHED", beepID)

	if _, err := printer.await(t, beep); err != nil {
		t.Fatalf("the beep after the abandoned home failed: %v", err)
	}
}

// expectNoFrame fails when the session writes a frame inside quietWindow.
func expectNoFrame(t *testing.T, frames chan []byte, when string) {
	t.Helper()

	select {
	case frame := <-frames:
		t.Fatalf("the session sent %q %s", frame, when)
	case <-time.After(quietWindow):
	}
}

// offer sends one command and hands back the channel its outcome arrives on.
func (h *hostile) offer(t *testing.T, cmd connectorapi.Command) chan outcome {
	t.Helper()

	answered := make(chan outcome, 1)

	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), hostileBudget)
		defer cancel()

		result, err := h.session.Do(ctx, cmd)
		answered <- outcome{result: result, err: err}
	}()

	return answered
}

// event sends one event for one command id as the printer.
//
// It carries no state. A first state is a change, and the session answers a
// change by asking the printer what the print is, which puts frames of its own
// on the wire between the ones these cases count.
func (h *hostile) event(t *testing.T, name string, id uint32) {
	t.Helper()

	h.text(t, fmt.Sprintf(`{"event":%q,"command_id":%d}`, name, id))
}

// readFrames reads every server-to-printer frame in a goroutine of its own. A
// read that times out breaks a gorilla connection for good, so a case that
// checks that no frame arrives waits on this channel and not on the socket.
func readFrames(t *testing.T, client *websocket.Conn) chan []byte {
	t.Helper()

	frames := make(chan []byte, 8)

	go func() {
		defer close(frames)

		for {
			_, data, err := client.ReadMessage()
			if err != nil {
				return
			}

			frames <- data
		}
	}()

	return frames
}

// takeID waits for the next frame and returns the command id in its header.
func takeID(t *testing.T, frames chan []byte) uint32 {
	t.Helper()

	select {
	case data, open := <-frames:
		if !open {
			t.Fatal("the connection ended before the session sent the next command")
		}

		if len(data) < hostileHeader {
			t.Fatalf("the driver wrote %q, which is shorter than the nine-byte header", data)
		}

		id, err := strconv.ParseUint(string(data[1:hostileHeader]), 16, 32)
		if err != nil {
			t.Fatalf("the driver wrote a header whose id is not eight hex digits: %q", data[:hostileHeader])
		}

		return uint32(id)
	case <-time.After(hostileBudget):
		t.Fatal("the session sent no command, so the queue is held for ever")

		return 0
	}
}
