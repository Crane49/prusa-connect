// Package historyapi is the contract a history provider implements: the store
// that keeps metrics and events. A provider is optional, and the core never
// waits for one.
//
// The class of an event decides where it is stored. Telemetry goes to
// AppendSamples and every other class goes to AppendFacts. See VerbOf.
//
// A metric name is the state path the value was reported under.
//
// A provider declares what it serves in Caps. A method whose bit is clear
// returns ErrUnsupported.
package historyapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/event"
)

// ErrUnsupported reports a method whose capability bit this provider does not
// set. It is not a failure of the provider.
var ErrUnsupported = errors.New("historyapi: the provider does not serve this capability")

// Caps is the set of capabilities a provider serves. Every method whose bit
// is clear returns ErrUnsupported.
type Caps uint8

const (
	// CapSamples means AppendSamples and QuerySamples store and answer.
	CapSamples Caps = 1 << iota
	// CapFacts means AppendFacts and QueryFacts store and answer.
	CapFacts
	// CapDownsample means raw samples are rolled into buckets the provider
	// keeps after the raw rows have expired.
	CapDownsample
	// CapAggregate means QuerySamples honours Step and Agg. A provider without
	// it refuses a query that sets them.
	CapAggregate
	// CapRetention means the provider expires what it stores by its own
	// policy.
	CapRetention
	// CapCommands means the provider folds the facts of each command into one
	// row and answers QueryCommands from it.
	CapCommands
)

// Has reports whether every bit in want is set.
func (c Caps) Has(want Caps) bool { return c&want == want }

// Verb is which append method stores an event.
type Verb uint8

const (
	// VerbNone is an event a history provider never stores.
	VerbNone Verb = iota
	// VerbSample is AppendSamples.
	VerbSample
	// VerbFact is AppendFacts.
	VerbFact
)

// VerbOf returns the append method for an event class. Telemetry is a sample.
// State, audit and command events are facts. A provider stores a command
// event even after the command has expired.
func VerbOf(class event.Class) Verb {
	switch class {
	case event.ClassTelemetry:
		return VerbSample
	case event.ClassState, event.ClassAudit, event.ClassCommand:
		return VerbFact
	}

	return VerbNone
}

// Precision is how finely every provider stores an instant. A provider
// truncates a finer instant and never rounds it, so two adjacent samples do
// not get the same key.
const Precision = time.Microsecond

// Truncate reduces an instant to Precision, in UTC. A provider stores and
// compares the truncated value.
func Truncate(at time.Time) time.Time { return at.UTC().Truncate(Precision) }

// SampleKind says whether a sample is a number or a text. A provider must
// store it. An empty Text is a valid text value.
type SampleKind uint8

const (
	// SampleNumber is a measurement, in Value. It is the zero value.
	SampleNumber SampleKind = iota
	// SampleText is a word, in Text, such as a state or a material name.
	SampleText
)

// ErrSampleShape reports a sample with both values set, or with the wrong one
// for its kind. A provider refuses such a sample and does not repair it.
var ErrSampleShape = errors.New("historyapi: a sample carries exactly one of a number and a word")

// Sample is one measurement. Ref and Metric together name the series. A
// sample has a number or a text, never both, and Kind says which.
type Sample struct {
	// At is when the measurement was taken, stored to Precision.
	At     time.Time
	Ref    event.Origin
	Metric string
	// Kind says which of Value and Text is set.
	Kind SampleKind
	// Value is the measurement. It is read only when Kind is SampleNumber.
	Value float64
	// Text is the word. It is read only when Kind is SampleText.
	Text string
}

// Check reports ErrSampleShape for a sample whose values do not match its
// kind. A provider calls it on every sample of a batch and refuses the batch
// on an error.
func (s Sample) Check() error {
	switch s.Kind {
	case SampleNumber:
		if s.Text != "" {
			return fmt.Errorf("%w: %s of %s is a number and carries the word %q",
				ErrSampleShape, s.Metric, s.Ref.String(), s.Text)
		}
	case SampleText:
		if s.Value != 0 {
			return fmt.Errorf("%w: %s of %s is a word and carries the number %v",
				ErrSampleShape, s.Metric, s.Ref.String(), s.Value)
		}
	default:
		return fmt.Errorf("%w: %s of %s names no kind this contract carries",
			ErrSampleShape, s.Metric, s.Ref.String())
	}

	return nil
}

// Fact is one event, stored whole. Payload is the event's data, unparsed.
// Ref, Epoch and Sequence together are the dedup key, so an event that is
// appended twice is stored once.
type Fact struct {
	// At is when the event happened, stored to Precision.
	At       time.Time
	Ref      event.Origin
	Type     string
	Sequence uint64
	Epoch    string
	Payload  json.RawMessage
	// Class is the event's class. A provider must store it, because
	// FactQuery.Classes filters on it.
	Class event.Class
}

// Agg is how samples inside one step are reduced.
type Agg uint8

// The reductions a provider carrying CapAggregate offers.
const (
	// AggNone returns raw samples, and is what a zero Step means.
	AggNone Agg = iota
	AggMean
	AggMin
	AggMax
	AggLast
	AggSum
)

// SampleQuery asks for a range of one or more series.
type SampleQuery struct {
	// Refs is the origins to read. The host has already reduced it to what
	// the caller may read. A provider checks no permission.
	Refs    []event.Origin
	Metrics []string
	From    time.Time
	To      time.Time
	// Step is the bucket width. Zero means raw samples.
	Step  time.Duration
	Agg   Agg
	Limit int
	// Cursor is the cursor of the previous page. The provider creates it, and
	// it is opaque to the caller.
	Cursor string
}

// SampleResult is one page of samples and the cursor of the next page.
type SampleResult struct {
	Samples []Sample
	// Cursor is empty when the page is the last one.
	Cursor string
	// Incomplete reports a window in which the provider knows it missed
	// samples.
	Incomplete bool
}

// FactQuery asks for the facts of one or more origins in a window.
type FactQuery struct {
	// Refs is the origins to read, already reduced to what the caller may
	// read.
	Refs  []event.Origin
	Types []string
	// Classes is the classes the answer may contain. The host sets it from
	// what the caller may see, for example to hide audit events. A provider
	// must return no fact of another class. Empty means every class.
	Classes []event.Class
	From    time.Time
	To      time.Time
	Limit   int
	Cursor  string
}

// FactResult is one page of facts and the cursor of the next page.
type FactResult struct {
	Facts []Fact
	// Cursor is empty when the page is the last one.
	Cursor string
	// Incomplete reports a window in which the provider knows it missed
	// events.
	Incomplete bool
}

// Command is one command as a provider folded it from the requested, the
// authorized and the settled fact with the same command id. A provider fills
// Authorized, AuthType and AuthID whenever it has them. The host empties them
// for a caller who may not see audit events.
type Command struct {
	// ID is the command id, created by the host.
	ID string
	// Ref is the origin the command was issued against.
	Ref event.Origin
	// Verb is the capability key that was invoked.
	Verb string
	// Key is the caller's idempotency key, empty when none was sent.
	Key string

	// Requested is when the command was published. Zero when the store never
	// saw the requested fact.
	Requested time.Time
	// Authorized is when the authorization was recorded. Zero when the store
	// has no audit fact for this command or the caller may not see it.
	Authorized time.Time
	// Settled is when the outcome was published. Zero while the command is
	// open.
	Settled time.Time

	// Status is one of the values in sdk/event, empty while the command is
	// open. OutcomeUnknown says the host never observed how it ended.
	Status         string
	OK             bool
	OutcomeUnknown bool
	// MachineReason is the device's own word for a refusal, FaultCode the
	// registered code of a failure, and ElapsedMS how long the command took.
	MachineReason string
	FaultCode     string
	ElapsedMS     int64

	// AuthType and AuthID are who authorized it, from the audit fact. Empty
	// without it.
	AuthType string
	AuthID   string
}

// Open reports a command that was requested and has not settled.
func (c Command) Open() bool { return !c.Requested.IsZero() && c.Settled.IsZero() }

// CommandQuery asks for folded command rows.
type CommandQuery struct {
	// Refs is the origins to read, already reduced to what the caller may
	// read. Empty means every origin in the store. Only the host itself asks
	// that.
	Refs []event.Origin
	// IDs limits the answer to these command ids. Empty means every id.
	IDs []string
	// OpenOnly limits the answer to commands that are requested and not
	// settled.
	OpenOnly bool
	// From and To bound Requested. A zero value sets no bound.
	From time.Time
	To   time.Time
	// Limit and Cursor page the answer, as in the other queries.
	Limit  int
	Cursor string
}

// CommandResult is one page of folded commands, newest first, and the cursor
// of the next page.
type CommandResult struct {
	Commands []Command
	// Cursor is empty when the page is the last one.
	Cursor string
}

// Provider is the contract of a history store. The host calls it per batch
// and per query, never per event.
type Provider interface {
	// Caps is what this provider serves. It must not change while the
	// provider runs.
	Caps() Caps
	// AppendSamples stores a batch of measurements. Two equal measurements at
	// different instants are two samples.
	AppendSamples(ctx context.Context, samples []Sample) error
	// AppendFacts stores a batch of events. It is idempotent on the dedup key
	// of Fact.
	AppendFacts(ctx context.Context, facts []Fact) error
	// QuerySamples returns the samples of a time range.
	QuerySamples(ctx context.Context, query SampleQuery) (SampleResult, error)
	// QueryFacts returns the events of a time range.
	QueryFacts(ctx context.Context, query FactQuery) (FactResult, error)
	// QueryCommands returns folded command rows. A provider without
	// CapCommands returns ErrUnsupported.
	QueryCommands(ctx context.Context, query CommandQuery) (CommandResult, error)
	// Retention is what this provider keeps, sorted coarsest last. Nil means
	// it keeps everything. It must not change while the provider runs.
	Retention() []Resolution
}

// Resolution is one step a provider keeps rows at, and for how long. It only
// describes the provider's own retention. The core deletes nothing.
type Resolution struct {
	// Step is the bucket width rows are kept at. Zero is raw.
	Step time.Duration
	// Keeps is how long a row at this step is kept. Zero is forever.
	Keeps time.Duration
}

// ErrQueryRefused reports a valid query that the provider cannot answer
// correctly, such as raw samples of a window whose raw rows have expired. A
// provider must refuse such a query and must not return a partial answer.
var ErrQueryRefused = errors.New("historyapi: the provider will not answer a query in this shape")

// QueryRefusedError is a refusal with its reason and what the caller can ask
// for instead.
type QueryRefusedError struct {
	// Reason is one sentence, shown to the caller.
	Reason string
	// Coarsest is the smallest step the provider can still answer the window
	// at, and zero for none.
	Coarsest time.Duration
	// Oldest is the earliest start the provider could have answered. Zero
	// when the provider does not know it.
	Oldest time.Time
}

// RefuseQuery returns a QueryRefusedError.
func RefuseQuery(reason string, coarsest time.Duration, oldest time.Time) error {
	return &QueryRefusedError{Reason: reason, Coarsest: coarsest, Oldest: oldest}
}

// Error returns the reason, followed by the coarsest step when there is one.
func (r *QueryRefusedError) Error() string {
	if r.Coarsest > 0 {
		return r.Reason + "; the coarsest step still available is " + r.Coarsest.String()
	}

	return r.Reason
}

// Unwrap returns ErrQueryRefused, so errors.Is matches it.
func (r *QueryRefusedError) Unwrap() error { return ErrQueryRefused }
