package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config"
	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
)

// gcodeVerb is the one dangerous-tier verb this build marks, and the one whose
// arguments a requested frame publishes as a digest.
const gcodeVerb = "core.command.send_gcode"

// busReader collects every durable frame the plane publishes, so a case can
// count what one command cost the origin rather than look for one frame.
type busReader struct {
	mu     sync.Mutex
	frames []event.Envelope
	sub    *stream.Subscription
}

// readBus subscribes as the deployment's own principal, which sees every
// printer and holds audit sight of it.
func readBus(t *testing.T, hub *stream.Hub) *busReader {
	t.Helper()

	sub, err := hub.Subscribe(context.WithoutCancel(t.Context()), stream.SubscribeRequest{
		Principal: "user:" + config.DefaultPrincipal,
		Encoding:  stream.EncodingJSON,
	})
	require.NoError(t, err)

	t.Cleanup(sub.Close)

	reader := &busReader{sub: sub}

	go func() {
		for batch := range sub.Batches() {
			reader.mu.Lock()

			for _, frame := range batch.Events {
				var envelope event.Envelope
				if json.Unmarshal(frame.Bytes, &envelope) == nil {
					reader.frames = append(reader.frames, envelope)
				}
			}

			reader.mu.Unlock()
		}
	}()

	return reader
}

// commands returns the command-lifecycle frames seen so far, in arrival order.
func (r *busReader) commands() []event.Envelope {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]event.Envelope, 0, len(r.frames))

	for _, envelope := range r.frames {
		if strings.HasPrefix(envelope.Type, event.CoreNamespace+"device.command.") {
			out = append(out, envelope)
		}
	}

	return out
}

// awaitCommands waits for at least want command frames and returns them.
func (r *busReader) awaitCommands(t *testing.T, want int) []event.Envelope {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for {
		got := r.commands()
		if len(got) >= want {
			return got
		}

		if time.Now().After(deadline) {
			t.Fatalf("only %d command frames arrived, wanted %d", len(got), want)
		}

		time.Sleep(5 * time.Millisecond)
	}
}

// byType indexes frames by their event type.
func byType(frames []event.Envelope) map[string]event.Envelope {
	out := make(map[string]event.Envelope, len(frames))
	for _, frame := range frames {
		out[frame.Type] = frame
	}

	return out
}

// nozzleWithArgument is the unmarked verb with the one argument it takes, so a
// case can publish an argument plain and watch it arrive that way.
func nozzleWithArgument() connectorapi.Set {
	return connectorapi.Set{
		connectorapi.Capability(nozzleVerb): {Flag: true},
		connectorapi.ArgumentKey(connectorapi.Capability(nozzleVerb), "nozzle_temperature"): {
			Flag: true, Kind: connectorapi.KindNumber, Required: true,
		},
	}
}

// heldSession is a driver session whose Do blocks until the case releases it,
// which is what a printer that has not answered yet looks like from here.
type heldSession struct {
	countedSession

	release chan struct{}
	calls   chan struct{}
	answer  connectorapi.Result
	verbs   connectorapi.Set
}

func (s *heldSession) Capabilities() (connectorapi.Set, uint64) { return s.verbs, 1 }

func (s *heldSession) Do(ctx context.Context, _ connectorapi.Command) (connectorapi.Result, error) {
	select {
	case s.calls <- struct{}{}:
	default:
	}

	select {
	case <-s.release:
		return s.answer, nil
	case <-ctx.Done():
		return connectorapi.Result{}, ctx.Err()
	}
}

// arriveHeld puts a printer in the plane whose commands wait for the case.
func arriveHeld(t *testing.T, plane *devices, serial string, verbs connectorapi.Set) (
	string, *heldSession,
) {
	t.Helper()

	host := plane.Arrivals("test", verbs, capability.NewResolver(profile.Registry{}, discard()), "")

	session := &heldSession{
		countedSession: countedSession{
			identity: connectorapi.Identity{Vendor: "Prusa", Model: "MK4", Serial: serial},
			patches:  make(chan connectorapi.StatePatch),
		},
		release: make(chan struct{}),
		calls:   make(chan struct{}, 1),
		answer:  connectorapi.Result{OK: true},
		verbs:   verbs,
	}

	_, err := host.Admit(context.WithoutCancel(t.Context()), session.identity)
	require.NoError(t, err)
	require.NoError(t, host.Arrived(context.WithoutCancel(t.Context()), session))

	t.Cleanup(func() { close(session.patches) })

	return plane.idOf(session.identity), session
}

// planeAndBus is a device plane with a reader attached to its hub.
func planeAndBus(t *testing.T) (*devices, *busReader) {
	t.Helper()

	plane, bus, _ := planeBusAndGraph(t)

	return plane, bus
}

// planeBusAndGraph is planeAndBus for a test that must write a tuple: a
// dangerous verb is conferred by no level, so reaching one takes ownership or
// a row naming it (ADR-20260901-b2z rules 16 to 19).
func planeBusAndGraph(t *testing.T) (*devices, *busReader, *authz.Store) {
	t.Helper()

	graph := newGraph(t)

	hub, err := stream.New(stream.Options{
		Site: "local", Authorizer: visibility{graph: graph}, Logger: discard(),
		Interval: 5 * time.Millisecond,
	})
	require.NoError(t, err)

	t.Cleanup(hub.Close)

	return newPlaneOn(t, graph, hub), readBus(t, hub), graph
}

// TestOneCommandPublishesExactlyThreeDurableFrames is ADR-20260901-atx rule 10. The
// replay window is 32 events and 32 KiB per origin, so the count is the whole
// of what a command costs a reconnecting reader's continuity, and a fourth
// frame is a change to that arithmetic rather than a detail.
func TestOneCommandPublishesExactlyThreeDurableFrames(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus := planeAndBus(t)
		id, session := arriveHeld(t, plane, "SIM-THREE", nozzleWithArgument())
		close(session.release)

		answer, err := plane.Command(t.Context(), "user:"+config.DefaultPrincipal, id,
			httpapi.PrinterCommand{Verb: nozzleVerb, Args: map[string]string{"nozzle_temperature": "215"}})
		require.NoError(t, err)
		require.NotEmpty(t, answer.CommandID)

		frames := bus.awaitCommands(t, 3)
		require.Len(t, frames, 3, "one command published something other than three frames")

		indexed := byType(frames)
		require.Len(t, indexed, 3)

		// Rule 6: the causation id is the join, and a consumer folds by it.
		require.Equal(t, answer.CommandID, indexed[event.TypeDeviceCommandSettled].CausationID)
		require.Equal(t, answer.CommandID, indexed[event.TypeDeviceCommandAuthorized].CausationID)

		// Rule 11: only the instruction may carry a TTL, and the hub refuses one on
		// anything else, so the record of what happened is never expirable.
		require.Nil(t, indexed[event.TypeDeviceCommandSettled].TTL)
		require.Nil(t, indexed[event.TypeDeviceCommandAuthorized].TTL)

		require.Equal(t, event.ClassCommand, indexed[event.TypeDeviceCommandRequested].Class)
		require.Equal(t, event.ClassState, indexed[event.TypeDeviceCommandSettled].Class)
		require.Equal(t, event.ClassAudit, indexed[event.TypeDeviceCommandAuthorized].Class)

		t.Logf("observed: %s published %d frames, joined by the causation id",
			nozzleVerb, len(frames))
	})
}

// TestTheCommandFramesKeepThePrincipalToTheAuditFrame is ADR-20260901-atx rule 9's
// last bullet. Who authorized lives in the audit frame alone, which is the one
// frame rules 12 and 13 gate: a principal on the instruction or the outcome
// would be the same widening published on two ungated classes.
func TestTheCommandFramesKeepThePrincipalToTheAuditFrame(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus := planeAndBus(t)
		id := arriveTalking(t, plane, "SIM-ACTOR", connectorapi.Result{
			OK: false, MachineReason: "BUSY", Raw: json.RawMessage(`{"vendor":"secret"}`),
		})

		_, err := plane.Command(t.Context(), "user:"+config.DefaultPrincipal, id,
			httpapi.PrinterCommand{Verb: nozzleVerb})
		require.NoError(t, err)

		indexed := byType(bus.awaitCommands(t, 3))

		var authorized event.CommandAuthorized
		require.NoError(t, json.Unmarshal(indexed[event.TypeDeviceCommandAuthorized].Data, &authorized))
		require.Equal(t, "user", authorized.AuthType)
		require.Equal(t, config.DefaultPrincipal, authorized.AuthID)
		require.Equal(t, "user", indexed[event.TypeDeviceCommandAuthorized].AuthType)
		require.Equal(t, config.DefaultPrincipal, indexed[event.TypeDeviceCommandAuthorized].AuthID)

		for _, kind := range []string{event.TypeDeviceCommandRequested, event.TypeDeviceCommandSettled} {
			body := string(indexed[kind].Data)
			require.NotContains(t, body, "auth_", "%s carries the principal", kind)
			require.NotContains(t, body, config.DefaultPrincipal, "%s carries the principal", kind)
			require.Empty(t, indexed[kind].AuthID, "%s carries the audit attributes", kind)

			// ADR-20260829-jdq rule 7's premise: Result.Raw reaches no published payload.
			require.NotContains(t, body, "secret", "%s carries the vendor's raw reply", kind)
		}

		// Each payload is inside the bound its schema declares.
		require.LessOrEqual(t, len(indexed[event.TypeDeviceCommandRequested].Data),
			event.CommandRequestedMaxBytes)
		require.LessOrEqual(t, len(indexed[event.TypeDeviceCommandSettled].Data),
			event.CommandSettledMaxBytes)
		require.LessOrEqual(t, len(indexed[event.TypeDeviceCommandAuthorized].Data),
			event.CommandAuthorizedMaxBytes)

		t.Logf("observed: the actor is on the audit frame and the other two carry %d and %d bytes",
			len(indexed[event.TypeDeviceCommandRequested].Data),
			len(indexed[event.TypeDeviceCommandSettled].Data))
	})
}

// TestADangerousVerbPublishesItsArgumentsAsADigest is ADR-20260901-atx rule 9's second
// bullet. A requested frame travels to every subscriber holding sight of the
// device, and raw gcode broadcast to every device viewer is the widening the
// debug log already refused for the actor.
func TestADangerousVerbPublishesItsArgumentsAsADigest(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus, graph := planeBusAndGraph(t)

		verbs := connectorapi.Set{
			connectorapi.Capability(gcodeVerb): {Flag: true},
			connectorapi.ArgumentKey(connectorapi.Capability(gcodeVerb), "gcode"): {
				Flag: true, Kind: connectorapi.KindString, Required: true,
			},
		}

		id, session := arriveHeld(t, plane, "SIM-DANGEROUS", verbs)
		close(session.release)

		// send_gcode carries opaque_payload, so no level confers it. Ownership does
		// (ADR-20260901-b2z rule 19), which is what makes this frame reachable at all.
		require.NoError(t, graph.Write(t.Context(),
			tuple(t, authz.TypeDevice, id, authz.RelOwner, "user:"+config.DefaultPrincipal)))

		_, err := plane.Command(t.Context(), "user:"+config.DefaultPrincipal, id,
			httpapi.PrinterCommand{Verb: gcodeVerb, Args: map[string]string{"gcode": "M997"}})
		require.NoError(t, err)

		indexed := byType(bus.awaitCommands(t, 3))

		var requested event.CommandRequested
		require.NoError(t, json.Unmarshal(indexed[event.TypeDeviceCommandRequested].Data, &requested))

		require.Empty(t, requested.Args, "a dangerous verb published its arguments as text")
		require.True(t, requested.ArgsOmitted)
		require.Len(t, requested.ArgsDigest, 64)
		require.NotContains(t, string(indexed[event.TypeDeviceCommandRequested].Data), "M997")

		// The digest still proves what was issued.
		require.Equal(t, argumentsDigest(map[string]string{"gcode": "M997"}), requested.ArgsDigest)

		t.Logf("observed: %s published args_digest %s and no text", gcodeVerb, requested.ArgsDigest)
	})
}

// TestAnUnmarkedVerbPublishesItsArgumentsPlain is the other half of the same
// rule, without which the case above is satisfied by a plane that never
// publishes an argument at all.
func TestAnUnmarkedVerbPublishesItsArgumentsPlain(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus := planeAndBus(t)
		id, session := arriveHeld(t, plane, "SIM-PLAIN", nozzleWithArgument())
		close(session.release)

		_, err := plane.Command(t.Context(), "user:"+config.DefaultPrincipal, id,
			httpapi.PrinterCommand{Verb: nozzleVerb, Args: map[string]string{"nozzle_temperature": "215"}})
		require.NoError(t, err)

		indexed := byType(bus.awaitCommands(t, 3))

		var requested event.CommandRequested
		require.NoError(t, json.Unmarshal(indexed[event.TypeDeviceCommandRequested].Data, &requested))

		require.Equal(t, map[string]string{"nozzle_temperature": "215"}, requested.Args)
		require.False(t, requested.ArgsOmitted)
		require.Empty(t, requested.ArgsDigest)

		t.Logf("observed: %s published its arguments as %v", nozzleVerb, requested.Args)
	})
}

// TestACommandAbandonedByItsCallerStillTerminatesItsRecord is ADR-20260901-atx rules 7
// and 8, and the gap this record exists to close. The caller's context dies
// while the command is outstanding; the driver has no cancellation case on
// purpose, so the printer may already have it. The record says the caller went
// away and says the outcome is not known, rather than guessing one.
func TestACommandAbandonedByItsCallerStillTerminatesItsRecord(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus := planeAndBus(t)
		id, session := arriveHeld(t, plane, "SIM-ABANDONED", talkingBaseline())

		ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))

		done := make(chan error, 1)

		go func() {
			_, err := plane.Command(ctx, "user:"+config.DefaultPrincipal, id,
				httpapi.PrinterCommand{Verb: nozzleVerb})
			done <- err
		}()

		<-session.calls
		cancel()

		require.Error(t, <-done)

		indexed := byType(bus.awaitCommands(t, 3))

		var settled event.CommandSettled
		require.NoError(t, json.Unmarshal(indexed[event.TypeDeviceCommandSettled].Data, &settled))

		require.Equal(t, event.CommandAbandoned, settled.Status)
		require.True(t, settled.OutcomeUnknown, "an abandoned command claimed an outcome nobody observed")
		require.False(t, settled.OK)

		t.Logf("observed: the abandoned command settled as %q with the outcome unknown", settled.Status)
	})
}

// TestAnInFlightDuplicateIsAnsweredWithTheFirstCommandID is ADR-20260901-atx rule 16.
// The second caller is not dispatched twice and publishes no frames of its own,
// which is a real and stated gap in the trail rather than a hidden one.
func TestAnInFlightDuplicateIsAnsweredWithTheFirstCommandID(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		plane, bus := planeAndBus(t)
		id, session := arriveHeld(t, plane, "SIM-DUPLICATE", talkingBaseline())

		// The invocation's error travels out rather than being asserted in place:
		// require calls t.FailNow, which is only the test goroutine's to call.
		type invocation struct {
			answer httpapi.PrinterResult
			err    error
		}

		first := make(chan invocation, 1)

		go func() {
			answer, err := plane.Command(context.WithoutCancel(t.Context()),
				"user:"+config.DefaultPrincipal, id,
				httpapi.PrinterCommand{Verb: nozzleVerb, Key: "k-1"})

			first <- invocation{answer: answer, err: err}
		}()

		<-session.calls

		// The first invocation is inside Do. The second must not reach the printer.
		duplicate, err := plane.Command(context.WithoutCancel(t.Context()),
			"user:"+config.DefaultPrincipal, id,
			httpapi.PrinterCommand{Verb: nozzleVerb, Key: "k-1"})
		require.NoError(t, err)
		require.True(t, duplicate.Duplicate)
		require.NotEmpty(t, duplicate.CommandID)

		close(session.release)

		got := <-first
		require.NoError(t, got.err)
		require.Equal(t, got.answer.CommandID, duplicate.CommandID,
			"the duplicate was answered with an id the first invocation never used")

		frames := bus.awaitCommands(t, 3)
		require.Len(t, frames, 3, "the duplicate published frames of its own")

		t.Logf("observed: both callers were answered %s and the printer was commanded once",
			got.answer.CommandID)
	})
}

// TestAStoredCommandIsNotAnExpiredOne is ADR-20260901-atx rule 11. A store keeps an
// instruction whose TTL has passed, because a record that an instruction was
// issued does not expire; an actor that replays one checks the envelope first.
func TestAStoredCommandIsNotAnExpiredOne(t *testing.T) {
	t.Parallel()

	// In a bubble: a reader attached before the printer arrives is fed on the
	// hub's authorize tick, a second the bubble's clock spends at once.
	synctest.Test(t, func(t *testing.T) {
		// TTL is remaining seconds, counted down by each hop, so an instruction
		// with none left is one that reached here expired.
		spent := uint32(0)
		envelope := event.Envelope{
			Type:  event.TypeDeviceCommandRequested,
			Class: event.ClassCommand,
			Time:  time.Now().Add(-time.Hour),
			TTL:   &spent,
		}

		require.True(t, envelope.Expired(), "the fixture is not an expired instruction")

		// The store's side of the split: the class routes to the fact verb whatever
		// the envelope's expiry says.
		require.Equal(t, historyapi.VerbFact, historyapi.VerbOf(envelope.Class))

		t.Log("observed: an expired instruction is a fact a provider stores and an actor refuses")
	})
}
