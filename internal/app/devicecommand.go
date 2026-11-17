package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/printer/capability"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/stream"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The durable command record.
//
// A command publishes three append-only frames on the device's own origin and
// exactly three: the audit frame at the authorization point, the instruction
// before it is handed to the driver, and the outcome after. A host-minted
// command id joins them, carried in the envelope's causation id, and whoever
// wants a mutable command row folds them.
//
// Three and not five is arithmetic and not taste. The replay window is 32
// events and 32 KiB per origin, so eleven commands evict a printer's whole
// window on count and about five evict it on bytes. dispatched and acknowledged
// are observable only inside the driver and would cost a contract change for
// two more frames, so they are not published.

// mintCommand returns the id of one invocation.
//
// It is a ULID, the tree's existing shape for a host-minted id, and it is not
// the caller's idempotency key: the two identities are distinct and both are
// carried. A mint that cannot draw entropy is not a reason to refuse a command,
// so the record is skipped and the command runs: a printer that pauses is worth
// more than a record of it pausing.
func (d *devices) mintCommand() string {
	id, err := d.commands.Mint(time.Now().UTC())
	if err != nil {
		d.log.Error("a command could not be given an id, so it is unrecorded", "error", err.Error())

		return ""
	}

	return id.String()
}

// refuseCommand records an invocation this host refused before any machine was
// reached, and hands the caller back the id it was recorded under.
//
// It publishes two frames and not three. The instruction was issued, so there
// is a requested frame; the invocation ended, so there is a settled one
// carrying event.CommandUnauthorized and the registered code of the refusal.
// There is no authorized frame, because nothing authorized it: an audit record
// of an authorization that did not happen would be the one lie in the trail.
//
// The caller is told the id on the problem document rather than in a body,
// because a refusal has no body. Without it the caller holds a 403 and no way
// to read back the record this just wrote.
func (d *devices) refuseCommand(
	ctx context.Context, entry *device, commandID string,
	cmd httpapi.PrinterCommand, kind authz.DeviceKind, refusal error,
) error {
	if commandID == "" {
		return refusal
	}

	d.publishRequested(ctx, entry.id, commandID, cmd, kind, entry.resolution())

	d.publishCommand(ctx, entry.id, event.TypeDeviceCommandSettled, event.ClassState,
		settledFrame(commandID, cmd.Verb, event.CommandUnauthorized, false,
			false, "", nil, nil, codeOf(refusal), 0),
		commandID, nil, "", "")

	return withCommandID(refusal, commandID)
}

// withCommandID puts the id the record was written under on a refusal, so that
// a caller holding the problem document can read the row back. Without it the
// caller holds a 403 or a 409 and no way to address the record this just wrote.
//
// It reaches a caller only on a code that declares command_id. fault.With drops
// a param the code does not name, so the promise this function makes is kept by
// the registry and not here: every code reachable from a refusal path declares
// it, which is what the case beside this one holds. A code that does not is not
// a silent drop either, because Error() names the param in the log line.
//
// An error carrying no fault is handed back unchanged rather than wrapped,
// because there is nothing on it for a param to ride on.
func withCommandID(refusal error, commandID string) error {
	if commandID == "" {
		return refusal
	}

	if carried, ok := errors.AsType[*fault.Fault](refusal); ok {
		return carried.With(fault.StringParam("command_id", commandID))
	}

	return refusal
}

// resolution is the capability set this device was last resolved against, and
// the zero value for a device this process has never held a session for.
//
// A device read back out of the record store at start-up has no session: the
// socket died with the process that held it. It is still an entry a caller can
// reach and be refused on, and a refusal is a record, so the record has to be
// renderable without a session. The empty resolution says what is true of
// such a device, which is that nothing here resolved a capability set for it.
func (e *device) resolution() capability.Resolution {
	if e.session == nil {
		return capability.Resolution{}
	}

	return e.session.Resolution()
}

// dangerousVerb reports whether this verb's arguments are published as their
// digest rather than as text.
//
// It reads the `dangerous` marking of the verb. A marked verb's arguments
// travel as a digest, because the requested frame reaches every subscriber
// who sees the device, and the payload of a dangerous verb must not reach
// every viewer of the device.
//
// The digest still proves what was issued. The accepted cost is that the durable
// record of a dangerous verb's payload is its hash and not its text.
func dangerousVerb(kind authz.DeviceKind, verb string) bool {
	return authz.Dangerous(kind, verb)
}

// argumentsDigest is the SHA-256 of one argument map, hex encoded.
//
// The rendering is sorted by key and length-prefixed on nothing, because both
// halves are already delimited: a key may not contain '=' or '\n' in any
// descriptor this host validates against, so no two distinct maps render the
// same text.
func argumentsDigest(args map[string]string) string {
	sum := sha256.New()

	for _, key := range slices.Sorted(maps.Keys(args)) {
		sum.Write([]byte(key))
		sum.Write([]byte("="))
		sum.Write([]byte(args[key]))
		sum.Write([]byte("\n"))
	}

	return hex.EncodeToString(sum.Sum(nil))
}

// requestedFrame renders the instruction, holding it inside its declared bound.
//
// Arguments that would break the bound are replaced by their digest rather than
// truncated: a truncated argument list proves nothing about what was issued,
// and a hash proves exactly it. A dangerous verb takes the same treatment
// whatever its arguments cost.
func requestedFrame(
	commandID, verb, key string, kind authz.DeviceKind,
	args map[string]string, resolution capability.Resolution,
) json.RawMessage {
	payload := event.CommandRequested{
		CommandID:        commandID,
		Verb:             verb,
		Key:              key,
		CapabilityEpoch:  resolution.Epoch,
		CapabilityDigest: resolution.Digest,
		Args:             args,
	}

	if dangerousVerb(kind, verb) {
		payload.Args = nil
		payload.ArgsDigest = argumentsDigest(args)
		payload.ArgsOmitted = true
	}

	rendered, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	if len(rendered) <= event.CommandRequestedMaxBytes {
		return rendered
	}

	payload.Args = nil
	payload.ArgsDigest = argumentsDigest(args)
	payload.ArgsOmitted = true

	rendered, err = json.Marshal(payload)
	if err != nil {
		return nil
	}

	return rendered
}

// settledFrame renders the outcome, and it is honest about what this host
// observed.
//
// A command whose caller went away may already have been written to the wire:
// the driver settles the call locally and deliberately has no cancellation case
// there, so the printer may execute it and the reply arrives to nobody. That
// outcome is recorded as unknown and never guessed. It is half of the gap this
// record closes and half of one it only documents: the invocation, the
// authorization and the abandonment are always recorded, and what the printer
// did after the abandonment is known only to the printer.
func settledFrame(
	commandID, verb string, status string, unknown bool,
	ok bool, reason string, ignored []string, substituted map[string]string,
	code string, elapsed time.Duration,
) json.RawMessage {
	payload := event.CommandSettled{
		CommandID:      commandID,
		Verb:           verb,
		Status:         status,
		OutcomeUnknown: unknown,
		OK:             ok,
		MachineReason:  reason,
		Ignored:        ignored,
		Substituted:    substituted,
		FaultCode:      code,
		ElapsedMS:      elapsed.Milliseconds(),
	}

	rendered, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	// The bound is held by dropping what a fold already has from the requested
	// frame, never by truncating a field: the arguments are not here at all,
	// and what is left that a device controls is the machine reason, already
	// clipped to the host's bound.
	if len(rendered) > event.CommandSettledMaxBytes {
		payload.Ignored = nil
		payload.Substituted = nil

		if rendered, err = json.Marshal(payload); err != nil {
			return nil
		}
	}

	return rendered
}

// authorizedFrame renders who authorized the command.
//
// The actor is in the payload as well as on the envelope's audit attributes,
// deliberately: a hop reads the envelope without parsing data and a history
// provider stores the data member, so an audit frame with the actor on the
// envelope alone is stored as an audit record with no actor.
func authorizedFrame(commandID, verb, authType, authID string) json.RawMessage {
	rendered, err := json.Marshal(event.CommandAuthorized{
		CommandID: commandID,
		Verb:      verb,
		AuthType:  authType,
		AuthID:    authID,
	})
	if err != nil {
		return nil
	}

	return rendered
}

// principalParts splits a principal into the pair the envelope's audit
// attributes carry. A principal with no type is carried whole as the id, which
// is what an audit record of an actor this build cannot parse should say.
func principalParts(principal string) (authType, authID string) {
	kind, id, found := strings.Cut(principal, ":")
	if !found {
		return "", principal
	}

	return kind, id
}

// settleStatus is the word one invocation ended under, and whether this host
// observed the outcome at all.
//
// A caller's context that died is the case the honesty matters for. The command
// may have been on the wire, so the status says the caller went away and the
// outcome says it is not known. A driver answering connectorapi.CommandUnanswered
// says the same thing in its own words: the frame reached the machine and the
// connection went before any answer, so the status is failed and the outcome is
// not known. Everything else is observed: a printer that answered no is
// refused, another error is failed, and a clean reply succeeded.
//
// A deadline the driver owns reads the same way as the caller's, because the
// record answers "how long did this wait and what came back", and a driver that
// stopped waiting for a printer waited out a deadline. The caller's is read
// first: a caller that went away is abandoned even where a driver deadline fired
// as well, because that is the fact an operator is looking at.
//
// The refusal is read off the result and never off an error, which is the
// driver contract on connectorapi.Result: the machine saying no is not the same
// event as the command failing on the way to it, and one status for both would
// put the printer's answer and a broken connection in the same row.
func settleStatus(ctx context.Context, result connectorapi.Result, err error) (string, bool) {
	switch {
	case err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		return event.CommandTimedOut, true
	case err != nil && errors.Is(ctx.Err(), context.Canceled):
		return event.CommandAbandoned, true
	case errors.Is(err, context.DeadlineExceeded):
		return event.CommandTimedOut, true
	case fault.HasCode(err, connectorapi.CommandUnanswered.Code()):
		return event.CommandFailed, true
	case err != nil:
		return event.CommandFailed, false
	case !result.OK:
		return event.CommandRefused, false
	}

	return event.CommandSucceeded, false
}

// inFlight is the idempotency map, scoped to (device, key), and it survives
// nothing by design.
//
// A second command with a live (device, key) is not dispatched twice: it is
// answered with the first invocation's id and publishes no frames of its own.
// That is a real and stated gap in the trail: it then shows one principal
// requesting a command two callers asked for, and the second asker exists only
// in the access log. The window is the first invocation's lifetime, which the
// caller's context bounds, and for a printer that is away the context is the
// only bound, so the window is not necessarily short.
//
// Dedup across a restart is not promised and nothing here may promise it. The
// requested frame is in memory for up to the feed's flush interval, nothing in
// the core waits on a provider, and the driver session is gone on a restart
// anyway: only the printer knows what it executed. This is not a queue.
type inFlight struct {
	device string
	key    string
}

// claimKey registers an invocation under its idempotency key, reporting the id of
// the invocation already holding it. An empty key is not deduplicated, which is
// what connectorapi.Command.Key already declares.
func (d *devices) claimKey(device, key, commandID string) (held string, mine bool) {
	if key == "" {
		return "", true
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running == nil {
		d.running = make(map[inFlight]string)
	}

	if already, live := d.running[inFlight{device: device, key: key}]; live {
		return already, false
	}

	d.running[inFlight{device: device, key: key}] = commandID

	return commandID, true
}

// releaseKey drops the claim, whatever the invocation ended as.
func (d *devices) releaseKey(device, key string) {
	if key == "" {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.running, inFlight{device: device, key: key})
}

// The publishers. Each one puts a frame the readers above built on the device's
// own origin, so a frame's shape and the act of publishing it read together.

// publishAudit puts the authorized frame on the device's origin.
//
// The actor rides on the envelope's audit attributes and in the payload both,
// which is deliberate and is written out at sdk/event.CommandAuthorized.
func (d *devices) publishAudit(ctx context.Context, id, commandID, verb, authType, authID string) {
	if commandID == "" {
		return
	}

	d.publishCommand(ctx, id, event.TypeDeviceCommandAuthorized, event.ClassAudit,
		authorizedFrame(commandID, verb, authType, authID),
		commandID, nil, authType, authID)
}

// publishRequested puts the instruction on the device's origin, with the
// remaining seconds of the caller's deadline as its TTL.
//
// A caller with no deadline publishes no TTL rather than a made-up one: the
// envelope's expiry is what a consumer that acts on a replayed instruction
// checks, and an invented deadline would expire a live instruction.
func (d *devices) publishRequested(
	ctx context.Context, id, commandID string, cmd httpapi.PrinterCommand,
	kind authz.DeviceKind, resolution capability.Resolution,
) {
	if commandID == "" {
		return
	}

	var ttl *uint32

	if deadline, set := ctx.Deadline(); set {
		remaining := time.Until(deadline)
		if remaining > 0 {
			seconds := uint32(min(remaining.Seconds()+1, math.MaxUint32))
			ttl = &seconds
		}
	}

	d.publishCommand(ctx, id, event.TypeDeviceCommandRequested, event.ClassCommand,
		requestedFrame(commandID, cmd.Verb, cmd.Key, kind, cmd.Args, resolution),
		commandID, ttl, "", "")
}

// publishSettled puts the outcome on the device's origin.
//
// It takes two contexts on purpose. detached is what the publication runs on,
// so the frame is written even when the caller has gone; asked is the caller's
// own context, read only to tell a deadline that expired from a caller that
// went away, which are two different words in the record.
func (d *devices) publishSettled(
	detached, asked context.Context, id, commandID, verb string,
	result connectorapi.Result, failure error, elapsed time.Duration,
) {
	if commandID == "" {
		return
	}

	status, unknown := settleStatus(asked, result, failure)

	// The code comes off whichever of the two carries it: a failure's own, and
	// for a refusal the one the driver put on the result. A row with no code
	// says the command was carried out, so a refusal that lost its code here
	// would read back as a success that happened to say no.
	code := refusalCode(result)
	if failure != nil {
		code = codeOf(failure)
	}

	d.publishCommand(detached, id, event.TypeDeviceCommandSettled, event.ClassState,
		settledFrame(commandID, verb, status, unknown, result.OK, refusalReason(result),
			result.Ignored, result.Substituted, code, elapsed),
		commandID, nil, "", "")
}

// publishCommand is publish with the fields only a command frame carries: the
// causation id that joins the three transitions, the TTL the instruction alone
// takes, and the audit attributes.
//
// A frame that could not be rendered is not published. A record that is missing
// is a record a reader can see is missing; a record carrying half a payload is
// one it cannot.
func (d *devices) publishCommand(
	ctx context.Context, id, kind string, class event.Class, data json.RawMessage,
	causation string, ttl *uint32, authType, authID string,
) {
	if len(data) == 0 {
		d.log.ErrorContext(ctx, "a command frame could not be rendered and is unpublished",
			"device", id, "type", kind, "command_id", causation)

		return
	}

	origin, err := event.NewOrigin(event.KindDevice, id)
	if err != nil {
		d.log.ErrorContext(ctx, "a device id is not an origin", "device", id, "error", err.Error())

		return
	}

	env, _, err := d.hub.PublishStored(stream.Publication{
		Origin:      origin,
		Type:        kind,
		Class:       class,
		Data:        data,
		CausationID: causation,
		TTL:         ttl,
		AuthType:    authType,
		AuthID:      authID,
	})
	if err != nil {
		d.log.DebugContext(ctx, "a command frame reached no hub",
			"device", id, "type", kind, "error", err.Error())

		return
	}

	// The frame is also written to the store here, on this goroutine. The feed
	// behind the bus writes it within two seconds, and a process killed inside
	// those two seconds would leave no row for the command.
	d.durable.write(ctx, factNow(origin, env))

	d.meters.published(kind)
}
