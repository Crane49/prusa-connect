package xbuddy

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/lane"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// A person's beep queued behind a background beep goes out first.
func TestAPersonsCommandOvertakesQueuedBackgroundWork(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)
	frames := readFrames(t, printer.client)
	homeID := holdTheQueue(t, printer, frames)

	background := printer.beepAs(t, lane.NewBackground())
	printer.awaitQueued(t, 1)

	person := printer.beepAs(t, nil)
	printer.awaitQueued(t, 2)
	personID := printer.session.nextID.Load()

	printer.event(t, "FINISHED", homeID)
	require.Equal(t, personID, takeID(t, frames))
	printer.event(t, "FINISHED", personID)
	printer.event(t, "FINISHED", takeID(t, frames))

	_, err := printer.await(t, person)
	require.NoError(t, err)
	_, err = printer.await(t, background)
	require.NoError(t, err)
}

// A person's flow queues its next command a moment after the last is answered,
// as a send queues its offer after the name check. A background command
// queued before that moment does not slip in between.
func TestABackgroundCommandWaitsOutAPersonsNextStep(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)
	frames := readFrames(t, printer.client)

	first := printer.beepAs(t, nil)
	firstID := takeID(t, frames)

	background := printer.beepAs(t, lane.NewBackground())
	printer.awaitQueued(t, 1)

	printer.event(t, "FINISHED", firstID)

	_, err := printer.await(t, first)
	require.NoError(t, err)

	time.Sleep(personLull / 5)

	next := printer.beepAs(t, nil)
	nextID := takeID(t, frames)
	require.Equal(t, printer.session.nextID.Load(), nextID, "the background beep went out between two steps")

	printer.event(t, "FINISHED", nextID)
	printer.event(t, "FINISHED", takeID(t, frames))

	_, err = printer.await(t, next)
	require.NoError(t, err)
	_, err = printer.await(t, background)
	require.NoError(t, err)
}

// A send's landing check follows TRANSFER_FINISHED at once, so background
// work waits while a transfer is open and goes out when it ends.
func TestABackgroundCommandWaitsForAnOpenTransfer(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)
	frames := readFrames(t, printer.client)
	homeID := holdTheQueue(t, printer, frames)

	open := newTestTransfer("usb/a.gcode")

	printer.session.mu.Lock()
	printer.session.transfer = open
	printer.session.mu.Unlock()

	background := printer.beepAs(t, lane.NewBackground())
	printer.awaitQueued(t, 1)

	printer.event(t, "FINISHED", homeID)
	time.Sleep(personLull)
	expectNoFrame(t, frames, "while a transfer was open")

	printer.session.endTransfer(open)
	printer.event(t, "FINISHED", takeID(t, frames))

	_, err := printer.await(t, background)
	require.NoError(t, err)
}

// Promoting a queued background command, which is what a person joining it
// does, moves it ahead of the background work queued before it.
func TestJoiningABackgroundReadPromotesIt(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)
	frames := readFrames(t, printer.client)
	homeID := holdTheQueue(t, printer, frames)
	_, _, bID := printer.promoteTheSecondOfTwo(t)

	printer.event(t, "FINISHED", homeID)
	require.Equal(t, bID, takeID(t, frames))
}

// A promoted command keeps its one place in the queue and is written once.
func TestAPromotedCommandIsSentOnce(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)
	frames := readFrames(t, printer.client)
	homeID := holdTheQueue(t, printer, frames)
	a, b, bID := printer.promoteTheSecondOfTwo(t)

	printer.event(t, "FINISHED", homeID)
	require.Equal(t, bID, takeID(t, frames))
	printer.event(t, "FINISHED", bID)

	aID := takeID(t, frames)
	require.Equal(t, bID-1, aID)
	printer.event(t, "FINISHED", aID)

	_, err := printer.await(t, b)
	require.NoError(t, err)
	_, err = printer.await(t, a)
	require.NoError(t, err)

	expectNoFrame(t, frames, "a promoted command went out twice")
}

// A background G-code the printer accepted is still running on it, so a
// person's command waits for its FINISHED like any other.
func TestABackgroundCommandOnTheWireRunsToItsEnd(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)
	frames := readFrames(t, printer.client)

	background := printer.beepAs(t, lane.NewBackground())
	backgroundID := takeID(t, frames)
	printer.event(t, "ACCEPTED", backgroundID)

	_, err := printer.await(t, background)
	require.NoError(t, err)

	person := printer.beepAs(t, nil)
	printer.awaitQueued(t, 1)
	expectNoFrame(t, frames, "while the background beep was still running")

	printer.event(t, "FINISHED", backgroundID)
	printer.event(t, "FINISHED", takeID(t, frames))

	_, err = printer.await(t, person)
	require.NoError(t, err)
}

// A background command still queued when the session ends is told so, and does
// not wait out its caller's context.
func TestClosingSettlesQueuedBackgroundCommands(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)
	frames := readFrames(t, printer.client)
	holdTheQueue(t, printer, frames)

	background := printer.beepAs(t, lane.NewBackground())
	printer.awaitQueued(t, 1)

	require.NoError(t, printer.session.Close())

	select {
	case answer := <-background:
		require.Error(t, answer.err)
	case <-time.After(hostileBudget):
		t.Fatal("a queued background command was still waiting after the session closed")
	}
}

// holdTheQueue puts an accepted G28 on the printer, which holds every command
// queued after it until its FINISHED, and returns its id.
func holdTheQueue(t *testing.T, printer *hostile, frames chan []byte) uint32 {
	t.Helper()

	home := printer.offer(t, connectorapi.Command{Verb: VerbHome})
	homeID := takeID(t, frames)
	printer.event(t, "ACCEPTED", homeID)

	_, err := printer.await(t, home)
	require.NoError(t, err)

	return homeID
}

// beepAs sends a beep marked background when bg is not nil, and as a
// person's command when it is.
func (h *hostile) beepAs(t *testing.T, bg *lane.Background) chan outcome {
	t.Helper()

	answered := make(chan outcome, 1)

	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), hostileBudget)
		defer cancel()

		if bg != nil {
			ctx = lane.WithBackground(ctx, bg)
		}

		result, err := h.session.Do(ctx, connectorapi.Command{Verb: VerbBeep})
		answered <- outcome{result: result, err: err}
	}()

	return answered
}

// awaitQueued waits until the queue holds n commands.
func (h *hostile) awaitQueued(t *testing.T, n int) {
	t.Helper()

	require.Eventually(t, func() bool {
		return h.session.queue.len() == n
	}, hostileBudget, time.Millisecond)
}

// promoteTheSecondOfTwo queues background beeps A and B behind a held queue,
// promotes B and waits for it to lose its background mark.
func (h *hostile) promoteTheSecondOfTwo(t *testing.T) (a, b chan outcome, bID uint32) {
	t.Helper()

	a = h.beepAs(t, lane.NewBackground())
	h.awaitQueued(t, 1)

	promoted := lane.NewBackground()
	b = h.beepAs(t, promoted)
	h.awaitQueued(t, 2)
	bID = h.session.nextID.Load()

	promoted.Promote()
	require.Eventually(t, func() bool {
		h.session.queue.mu.Lock()
		defer h.session.queue.mu.Unlock()

		return h.session.queue.persons == 1
	}, hostileBudget, time.Millisecond)

	return a, b, bID
}
