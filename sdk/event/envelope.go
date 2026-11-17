// Package event is the shape of every event on the wire: a CloudEvents 1.0
// envelope and the cursor a reader resumes from.
package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

const (
	// SpecVersion is the CloudEvents version every envelope declares.
	SpecVersion = "1.0"

	// ContentTypeJSON is what a consumer assumes when datacontenttype is
	// absent. An envelope with JSON data omits the attribute.
	ContentTypeJSON = "application/json"

	// MaxHops is the most times an envelope may be forwarded. It bounds a
	// forwarding loop.
	MaxHops = 32

	// sequenceDigits pads a uint64 so lexicographic and numeric order agree,
	// which the CloudEvents sequence extension requires.
	sequenceDigits = 20

	sourceScheme = "urn:prusa:site:"
)

// Errors reported by envelope construction and forwarding. Callers inspect
// them with errors.Is.
var (
	// ErrInvalidEnvelope reports an envelope missing a required attribute or
	// having one its class forbids.
	ErrInvalidEnvelope = errors.New("invalid envelope")
	// ErrHopLimit reports an envelope that has been forwarded MaxHops times.
	ErrHopLimit = errors.New("hop limit exceeded")
	// ErrExpired reports a command whose remaining time ran out in transit.
	ErrExpired = errors.New("envelope expired")
)

// Class is the event class. It tells a node the queue class, the propagation
// policy and whether expiry applies, even for an event type it does not know.
type Class string

const (
	// ClassTelemetry is a repeatedly-superseded measurement: conflated,
	// at-most-once, never replayed.
	ClassTelemetry Class = "telemetry"
	// ClassState is a transition worth keeping, such as a job finishing. FIFO,
	// replayable, never silently dropped.
	ClassState Class = "state"
	// ClassCommand is an instruction with a deadline, and the only class that
	// may have a TTL.
	ClassCommand Class = "command"
	// ClassAudit is a record of who did what: FIFO and replayable like state,
	// and signed per event.
	ClassAudit Class = "audit"
)

// Envelope is one event. Every attribute except the last block is CloudEvents
// 1.0 by name and by meaning.
//
// Do not forward by unmarshalling into it and marshalling again. That drops
// every extension attribute it does not know. Forward the original bytes.
type Envelope struct {
	SpecVersion string    `json:"specversion"`
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	Type        string    `json:"type"`
	Time        time.Time `json:"time"`
	Subject     string    `json:"subject,omitempty"`
	DataSchema  string    `json:"dataschema,omitempty"`
	// DataContentType is omitted for JSON, which a consumer assumes.
	DataContentType string `json:"datacontenttype,omitempty"`
	// Sequence is a zero-padded uint64 scoped to Source, assigned by the owning
	// node and identical at every hop. Order events by it, never by Time.
	Sequence    string `json:"sequence,omitempty"`
	TraceParent string `json:"traceparent,omitempty"`
	// CausationID is the id of the command that caused this event.
	CausationID string `json:"causationid,omitempty"`
	// Epoch identifies the numbering generation of Source. It changes when the
	// owning node's store is recreated and sequences restart.
	Epoch string `json:"epoch,omitempty"`
	// Hops counts forwarders, zero at the origin.
	Hops uint8 `json:"hops"`
	// Class drives queue, propagation and expiry decisions without a type table.
	Class Class `json:"evclass"`
	// TTL is the remaining seconds, never an absolute deadline. Each hop
	// reduces it by the time measured on its own monotonic clock. Commands
	// only.
	TTL *uint32 `json:"ttl,omitempty"`
	// AuthType and AuthID record who authorized a command. Audit only: the
	// receiving node re-authorizes locally.
	AuthType string `json:"authtype,omitempty"`
	AuthID   string `json:"authid,omitempty"`
	// Data stays raw so a router can forward an event without parsing its
	// payload.
	Data json.RawMessage `json:"data,omitempty"`
}

// ErrorData is the data member of an envelope that reports a failure. A code
// means the same on the API, on the stream and in a printer command result.
type ErrorData struct {
	Error fault.EventError `json:"error"`
}

// SourceURN renders the origin of an event: the site that owns the resource,
// and the resource. The form is "urn:prusa:site:<site>/<kind>/<id>". A relay
// never rewrites it, because source plus id is the dedup key and sequence is
// scoped to it.
func SourceURN(site string, origin Origin) string {
	if site == "" || origin.IsZero() {
		return ""
	}

	kind, id := origin.split()

	return sourceScheme + site + pathSeparator + kind + pathSeparator + id
}

// ParseSourceURN reads the form SourceURN renders.
func ParseSourceURN(s string) (site string, origin Origin, err error) {
	rest, found := strings.CutPrefix(s, sourceScheme)
	if !found {
		return "", Origin{}, fmt.Errorf("%w: source %q does not start with %q",
			ErrInvalidEnvelope, echo(s), sourceScheme)
	}

	site, path, found := strings.Cut(rest, pathSeparator)
	if !found || site == "" {
		return "", Origin{}, fmt.Errorf("%w: source %q names no site", ErrInvalidEnvelope, echo(s))
	}

	origin, err = ParsePath(path)
	if err != nil {
		return "", Origin{}, fmt.Errorf("%w: source %q: %w", ErrInvalidEnvelope, echo(s), err)
	}

	return site, origin, nil
}

// FormatSequence renders n zero-padded to twenty digits, so sequences compare
// as strings the way they compare as numbers.
func FormatSequence(n uint64) string {
	digits := strconv.FormatUint(n, 10)

	return strings.Repeat("0", sequenceDigits-len(digits)) + digits
}

// ParseSequence reads the form FormatSequence renders.
func ParseSequence(s string) (uint64, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: sequence %q: %w", ErrInvalidEnvelope, s, err)
	}

	return n, nil
}

// Valid reports whether c is a member of the closed set.
func (c Class) Valid() bool {
	switch c {
	case ClassTelemetry, ClassState, ClassCommand, ClassAudit:
		return true
	}

	return false
}

// String returns the class as it appears on the wire.
func (c Class) String() string { return string(c) }

// Durable reports whether the class survives a slow reader. A durable event is
// queued in order and replayed on resume. Telemetry is conflated and not
// replayed.
func (c Class) Durable() bool { return c != ClassTelemetry }

// Validate reports whether the envelope has every attribute the spec requires
// and none its class forbids. It checks the syntax of the type, not whether
// the type is registered. ValidateType checks that.
func (e Envelope) Validate() error {
	if err := ValidateTypeSyntax(e.Type); err != nil {
		return err
	}

	switch {
	case e.SpecVersion != SpecVersion:
		return fmt.Errorf("%w: specversion %q, want %q", ErrInvalidEnvelope, e.SpecVersion, SpecVersion)
	case e.ID == "":
		return fmt.Errorf("%w: id is empty", ErrInvalidEnvelope)
	case e.Source == "":
		return fmt.Errorf("%w: source is empty", ErrInvalidEnvelope)
	case !e.Class.Valid():
		return fmt.Errorf("%w: evclass %q", ErrInvalidEnvelope, e.Class)
	case e.TTL != nil && e.Class != ClassCommand:
		return fmt.Errorf("%w: ttl on class %q, commands only", ErrInvalidEnvelope, e.Class)
	case e.Hops > MaxHops:
		return fmt.Errorf("%w: %d hops", ErrHopLimit, e.Hops)
	}

	return nil
}

// Expired reports whether a command's remaining time has run out.
func (e Envelope) Expired() bool { return e.TTL != nil && *e.TTL == 0 }

// Forward returns the envelope as the next hop should see it: one more hop, and
// any remaining time reduced by elapsed, measured on this node's monotonic
// clock. The forwarder increments the count, so a peer cannot understate it.
func (e Envelope) Forward(elapsed time.Duration) (Envelope, error) {
	if e.Hops >= MaxHops {
		return Envelope{}, fmt.Errorf("%w: %d hops", ErrHopLimit, e.Hops)
	}

	out := e
	out.Hops = e.Hops + 1

	if e.TTL == nil {
		return out, nil
	}

	spent := secondsSpent(elapsed)
	if spent >= *e.TTL {
		return Envelope{}, fmt.Errorf("%w: %d seconds left, %d spent", ErrExpired, *e.TTL, spent)
	}

	left := *e.TTL - spent
	out.TTL = &left

	return out, nil
}

// WithError returns a copy whose data member is the fault.
func (e Envelope) WithError(f *fault.Fault) (Envelope, error) {
	data, err := json.Marshal(ErrorData{Error: f.EventError()})
	if err != nil {
		return Envelope{}, fmt.Errorf("encode fault %s: %w", f.Code(), err)
	}

	out := e
	out.Data = data

	return out, nil
}

// secondsSpent rounds up, so a chain of fast hops cannot extend a deadline by
// never spending a whole second.
func secondsSpent(elapsed time.Duration) uint32 {
	if elapsed <= 0 {
		return 0
	}

	seconds := int64(elapsed+time.Second-1) / int64(time.Second)
	if seconds > math.MaxUint32 {
		return math.MaxUint32
	}

	//nolint:gosec // guarded directly above: seconds is in [1, math.MaxUint32].
	return uint32(seconds)
}
