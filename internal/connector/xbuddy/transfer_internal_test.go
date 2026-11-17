package xbuddy

import (
	"errors"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// newTestTransfer is a transfer offered to nobody, for the settlement rules
// below. The channels are the ones settle and the serving goroutine use.
func newTestTransfer(path string) *transfer {
	return &transfer{
		hash:   "token-" + path,
		path:   path,
		blocks: make(chan queuedBlock, blockBacklog),
		done:   make(chan error, 1),
		stop:   make(chan struct{}),
	}
}

func settled(pending *transfer) bool {
	select {
	case <-pending.done:
		return true
	default:
		return false
	}
}

// A transfer that ended without arriving carries what the printer said about
// it: the file it was, the event that ended it, and both of the printer's own
// words for why (ADR-20260826-ezr rule 5). A firmware that sends only the sentence for a
// person leaves the machine reason empty and the sentence readable, which is the
// half that would otherwise be dropped.
func TestAFailedTransferCarriesWhatThePrinterSaid(t *testing.T) {
	t.Parallel()

	pending := newTestTransfer("usb/part.bgcode")

	err := transferOutcome(printerEvent{
		Event:         eventTransferAborted,
		MachineReason: "STORAGE_FULL",
		Reason:        "there is no room on the drive",
	}, pending)

	var refusal *fault.Fault
	if !errors.As(err, &refusal) {
		t.Fatalf("the failure is %v, and a caller reads a code", err)
	}

	for name, want := range map[string]string{
		"path":           "usb/part.bgcode",
		"event":          eventTransferAborted,
		"machine_reason": "STORAGE_FULL",
		"reason":         "there is no room on the drive",
	} {
		reported, carried := refusal.Params().Get(name)
		if !carried {
			t.Errorf("the failure carries no %s", name)

			continue
		}

		if got := reported.String(); got != want {
			t.Errorf("the failure reports %s = %q, want %q", name, got, want)
		}
	}

	if transferOutcome(printerEvent{Event: eventTransferFinished}, pending) != nil {
		t.Error("a transfer that finished is reported as a failure")
	}
}

// Which connection a queued block request may be answered on.
//
// The queue holds up to four requests and the goroutine draining it can be
// inside a read while the printer's connection goes. The printer names the
// transfer again on the connection that replaces it, with a file id it has not
// used before, so a request served out of the queue afterwards would put the
// retired id on the fresh connection, exactly while the printer is opening the
// transfer, and this board resets within twelve milliseconds of a frame it
// dislikes.
func TestABlockRequestIsOnlyAnsweredOnTheConnectionItArrivedOn(t *testing.T) {
	t.Parallel()

	first, second := &link{gone: make(chan struct{})}, &link{gone: make(chan struct{})}

	for _, tc := range []struct {
		name    string
		current *link
		arrived *link
		bound   uint32
		asked   uint32
		serves  bool
	}{
		{
			name:    "the connection it arrived on, for the id in force",
			current: first, arrived: first, bound: 7919, asked: 7919, serves: true,
		},
		{
			name:    "a connection that has been replaced",
			current: second, arrived: first, bound: 7919, asked: 7919,
		},
		{
			name:    "an id this attempt no longer uses",
			current: first, arrived: first, bound: 15838, asked: 7919,
		},
		{
			name:    "no connection at all",
			current: nil, arrived: first, bound: 7919, asked: 7919,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			session := &Session{done: make(chan struct{}), current: tc.current}
			pending := newTestTransfer("usb/offered.bgcode")
			pending.fileID, pending.bound = tc.bound, true

			queued := queuedBlock{request: block{fileID: tc.asked}, arrived: tc.arrived}

			current, serves := session.linkFor(pending, queued)
			if serves != tc.serves {
				t.Errorf("the request is answered = %v, want %v", serves, tc.serves)
			}

			if serves && current != tc.current {
				t.Error("the request is answered on a connection other than the live one")
			}
		})
	}
}

// Which transfer a terminal event ends.
//
// The ordering that made this a defect: a caller cancels, the driver queues
// STOP_TRANSFER, a second file is offered and its start command queues behind
// the stop, and the printer's TRANSFER_STOPPED for the first transfer arrives
// while the second has not left the queue. Read as the second transfer's, it
// fails a transfer the printer has not been told about yet.
func TestATerminalEventOnlyEndsTheTransferItNames(t *testing.T) {
	t.Parallel()

	id := func(value int64) *int64 { return &value }

	for _, tc := range []struct {
		name    string
		arrange func(s *Session, pending *transfer)
		event   printerEvent
		settles bool
	}{
		{
			name:    "an event carrying no id ends the transfer that is running",
			event:   printerEvent{Event: eventTransferFinished},
			settles: true,
		},
		{
			name: "an id matching the one the printer gave ends it",
			arrange: func(s *Session, pending *transfer) {
				s.rememberTransferID(pending, 500)
			},
			event:   printerEvent{Event: eventTransferFinished, TransferID: id(500)},
			settles: true,
		},
		{
			name: "an id naming another transfer does not",
			arrange: func(s *Session, pending *transfer) {
				s.rememberTransferID(pending, 500)
			},
			event: printerEvent{Event: eventTransferStopped, TransferID: id(501)},
		},
		{
			name: "the id of a transfer this session retired does not",
			arrange: func(s *Session, pending *transfer) {
				retired := newTestTransfer("usb/abandoned.bgcode")
				s.transfer = retired
				s.rememberTransferID(retired, 500)
				s.endTransfer(retired)
				s.transfer = pending
			},
			event: printerEvent{Event: eventTransferStopped, TransferID: id(500)},
		},
		{
			name:  "an id this session has never seen, for a transfer the printer has not opened, does not",
			event: printerEvent{Event: eventTransferStopped, TransferID: id(900)},
		},
		{
			name: "an id this session has never seen, for a transfer the printer has opened, does",
			arrange: func(_ *Session, pending *transfer) {
				pending.fileID, pending.bound = 4242, true
			},
			event:   printerEvent{Event: eventTransferFinished, TransferID: id(900)},
			settles: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			session := &Session{done: make(chan struct{})}
			pending := newTestTransfer("usb/offered.bgcode")
			session.transfer = pending

			if tc.arrange != nil {
				tc.arrange(session, pending)
			}

			session.finishTransfer(tc.event)

			if got := settled(pending); got != tc.settles {
				t.Errorf("the transfer settled = %v, want %v", got, tc.settles)
			}
		})
	}
}
