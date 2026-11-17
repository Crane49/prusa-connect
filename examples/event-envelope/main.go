// Command event-envelope builds every shape sdk/event publishes, renders it,
// and does with it what a consumer does: decide a queue lane from the class,
// decrement a command's expiry across a relay hop, read an error out of a
// payload, and resume a stream from the id of the last event received.
//
// Every event names an origin, which is a resource kind and an instance id. A
// printer is one kind; a storage adapter reporting its own state transition is
// another, and both are addressed and resumed the same way.
//
// Run it with: go run ./examples/event-envelope
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const (
	site  = "01K2A0SPRAGUEFARM07"
	epoch = "01K2A0S3P4"

	// kindStorage comes from the registry's closed set. sdk/event owns the
	// shape of a kind, never the vocabulary.
	kindStorage event.Kind = "storage"

	linkLatency = 1500 * time.Millisecond

	// What the numbers in this example stand for: the job event's place in its
	// printer's stream and the seconds a pause has left.
	sequence         = 914233
	instanceSequence = 8
	offset           = 914233
	siblingOffset    = 12
	pauseSeconds     = 47
)

// pauseRejected is what this example reports as a failure. Codes come from
// the published catalogue; an example does not get to define product
// vocabulary, and a main package cannot be imported by the generator that
// documents it.
var pauseRejected = fault.PrinterCommandRejected

// The two origins this example publishes from: a printer, and a storage
// instance that owns no printer at all.
var (
	printer = mustOrigin(event.KindDevice, "01JB8XN2K7QW9ZR4MV3T6H0PDC")
	s3Main  = mustOrigin(kindStorage, "s3-main")
)

// mustOrigin builds an origin from constants in this file. A failure is a bug
// in the example rather than a runtime condition.
func mustOrigin(kind event.Kind, id string) event.Origin {
	origin, err := event.NewOrigin(kind, id)
	if err != nil {
		panic(err)
	}

	return origin
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "example failed:", err)
		os.Exit(1)
	}
}

func run() error {
	finished := jobFinished()

	if err := show("a state event, as it goes on the wire", finished); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "\nqueue lane: durable=%v (a conflated lane would lose it)\n\n", finished.Class.Durable())

	if err := showInstanceTransition(); err != nil {
		return err
	}

	if err := showCommandAcrossAHop(); err != nil {
		return err
	}

	if err := showFailure(); err != nil {
		return err
	}

	return showResume(finished)
}

// jobFinished is the ordinary case: something happened on a printer, and the
// node that owns the printer stamped it.
func jobFinished() event.Envelope {
	return event.Envelope{
		SpecVersion: event.SpecVersion,
		ID:          "01K3E7T0JOB0000000000000AA",
		Source:      event.SourceURN(site, printer),
		Type:        "com.prusa.connect.job.finished",
		Time:        time.Date(2026, time.August, 24, 11, 42, 7, 0, time.UTC),
		Subject:     printer.String(),
		Sequence:    event.FormatSequence(sequence),
		Epoch:       epoch,
		Class:       event.ClassState,
		Data:        json.RawMessage(`{"jobId":"01K3E7T0JOB0000000000000AA","result":"finished"}`),
	}
}

// showCommandAcrossAHop is what a relay does: increment the hop count and take
// the time it spent off the expiry. The expiry is relative for exactly this
// reason. A node with no real-time clock cannot be handed a wall-clock deadline.
func showCommandAcrossAHop() error {
	ttl := uint32(pauseSeconds)

	pause := event.Envelope{
		SpecVersion: event.SpecVersion,
		ID:          "01K3F9KZ4H0000000000000CMD",
		Source:      event.SourceURN(site, printer),
		Type:        "com.prusa.connect.device.pause",
		Time:        time.Date(2026, time.August, 24, 11, 42, 9, 0, time.UTC),
		Subject:     printer.String(),
		Epoch:       epoch,
		Class:       event.ClassCommand,
		TTL:         &ttl,
		AuthType:    "user",
		AuthID:      "01K1USER0000000000000042",
		Data:        json.RawMessage(`{"reason":"operator"}`),
	}

	if err := pause.Validate(); err != nil {
		return err
	}

	forwarded, err := pause.Forward(linkLatency)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "command before the hop: hops=%d ttl=%ds\n", pause.Hops, *pause.TTL)
	fmt.Fprintf(os.Stdout, "command after  the hop: hops=%d ttl=%ds (%v on the link)\n\n",
		forwarded.Hops, *forwarded.TTL, linkLatency)

	return nil
}

// showFailure puts a fault inside an envelope, which is the only shape an error
// takes there. The same payload frames an HTTP response and a command result.
func showFailure() error {
	failure := pauseRejected.
		New("the printer is not printing").
		With(fault.StringParam("device_id", printer.ID())).
		WithTrace("4bf92f3577b34da6a3ce929d0e0e4736")

	rejected, err := event.Envelope{
		SpecVersion: event.SpecVersion,
		ID:          "01K3F9M00ERR000000000000AA",
		Source:      event.SourceURN(site, printer),
		Type:        "com.prusa.connect.device.command.failed",
		Time:        time.Date(2026, time.August, 24, 11, 42, 10, 0, time.UTC),
		Subject:     printer.String(),
		Epoch:       epoch,
		Class:       event.ClassState,
		CausationID: "01K3F9KZ4H0000000000000CMD",
	}.WithError(failure)
	if err != nil {
		return err
	}

	if showErr := show("a failure, carried as a fault payload", rejected); showErr != nil {
		return showErr
	}

	var carried event.ErrorData
	if decodeErr := json.Unmarshal(rejected.Data, &carried); decodeErr != nil {
		return decodeErr
	}

	fmt.Fprintf(os.Stdout, "\nwhat a consumer branches on: code=%s retryable=%v\n\n",
		carried.Error.Code, carried.Error.Retryable)

	return nil
}

// showInstanceTransition is an earlier design decision: a storage adapter going degraded is an
// event on this bus like any other. It names no printer anywhere, which is the
// whole point, and a UI watching the stream sees it live.
func showInstanceTransition() error {
	degraded := event.Envelope{
		SpecVersion: event.SpecVersion,
		ID:          "01K3F9P00RES000000000000AA",
		Source:      event.SourceURN(site, s3Main),
		Type:        "com.prusa.connect.resource.degraded",
		Time:        time.Date(2026, time.August, 24, 11, 42, 11, 0, time.UTC),
		Subject:     s3Main.String(),
		Sequence:    event.FormatSequence(instanceSequence),
		Epoch:       epoch,
		Class:       event.ClassState,
		Data:        json.RawMessage(`{"state":"degraded","reason":"bucket unreachable","observedGeneration":4}`),
	}

	if err := show("an instance state transition, on the same bus", degraded); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "\nwhat it is addressed as: kind=%s id=%s subject=%s\n\n",
		s3Main.Kind(), s3Main.ID(), degraded.Subject)

	return nil
}

// showResume is the reconnect: the reader kept the id of the last event it saw,
// and that id is the place it asks to continue from. Offsets are per origin, so
// the number in it counts that origin's events and nothing else, and the epoch
// beside it scopes those offsets to the process that issued them.
func showResume(last event.Envelope) error {
	position := event.Position{Origin: printer, Offset: offset}

	id := position.ID(last.Epoch)
	fmt.Fprintf(os.Stdout, "the id the last frame carried: %s\n", id)

	resume, err := event.ParseID(id)
	if err != nil {
		return err
	}

	generation, held := resume.Generation(last.Epoch)
	if !held {
		return fmt.Errorf("cursor %q holds no place in generation %s", resume, last.Epoch)
	}

	offset, named := generation.Offset(printer)
	if !named {
		return fmt.Errorf("cursor %q does not name %s", resume, printer)
	}

	fmt.Fprintf(os.Stdout, "resuming from:                 %s (%s at offset %d)\n", resume, printer, offset)

	// A reader that follows two nodes carries one generation per node, so the
	// second node recreating its log leaves the first node's place resumable.
	fleet := event.NewCursor(
		generation,
		event.NewGeneration("01K2A0SIBLINGNODE000000000", []event.Position{{Origin: s3Main, Offset: siblingOffset}}),
	)

	fmt.Fprintf(os.Stdout, "following two nodes:           %s\n", fleet)

	return nil
}

func show(title string, envelope event.Envelope) error {
	if err := envelope.Validate(); err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "%s:\n%s\n", title, encoded)

	return nil
}
