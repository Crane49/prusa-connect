package devtools

import (
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// DefaultCommandLimit is how many commands one process remembers. It is a bench
// tool for a person watching one printer, so the useful window is the last few
// minutes of a session; the bound is on the whole log rather than per printer so
// that what it costs does not grow with the fleet.
const DefaultCommandLimit = 256

// Command is one command this server carried to a printer, and what came back.
//
// It records no principal. A caller who may view a printer may read this log,
// and which operator sent which command is not something viewing a printer
// grants; the verb, the arguments and the outcome are what a person debugging
// the wire needs.
type Command struct {
	At       time.Time `json:"at"        doc:"When the command was offered to the printer."`
	DeviceID string    `json:"device_id" doc:"The device it was sent to."`
	Verb     string    `json:"verb"      doc:"The capability key that was invoked."`

	Args map[string]string `json:"args,omitempty" doc:"The arguments as the caller sent them."`

	ElapsedMS int64 `json:"elapsed_ms" doc:"How long the call took, in milliseconds, including any wait for a printer that was away."`
	OK        bool  `json:"ok"         doc:"Whether the printer accepted it."`

	// MachineReason is the printer's own word for a refusal, which is the fact a
	// person reading this log came for: "refused" sends them to a terminal to
	// ask the machine what it meant.
	MachineReason string `json:"machine_reason,omitempty" doc:"Why the printer refused, in the printer's own words. It is the machine's vocabulary rather than this API's, so it is shown to a person rather than branched on, and it is absent on a command the printer accepted. A reason longer than 32768 bytes is clipped and marked, because a printer chooses this text and this log keeps 256 of them."`

	Ignored     []string          `json:"ignored,omitempty"     doc:"Parameters the printer dropped."`
	Substituted map[string]string `json:"substituted,omitempty" doc:"Parameters the printer replaced, and with what."`

	// FaultCode and Failure describe a call that returned an error rather than a
	// result. Both are what the caller was told: a registered code, and the
	// message that went into the problem document.
	FaultCode string `json:"fault_code,omitempty" doc:"The registered error code the call was refused with."`
	Failure   string `json:"failure,omitempty"    doc:"The message the caller was given."`
}

// Outcome fills in what came back, whether that was a result or a refusal. It
// is a method so that the two shapes of answer are rendered the same way
// wherever a command is recorded.
//
// It records the answer the caller was given and decides nothing about it.
// Whether a machine reason may accompany an accepted command is the device
// plane's rule. That rule is held in one place and is not checked again here.
func (c *Command) Outcome(
	ok bool, machineReason string, ignored []string, substituted map[string]string, err error,
) {
	if err == nil {
		c.OK = ok
		c.MachineReason = machinetext.Clip(machineReason, machinetext.MaxBytes)
		c.Ignored = ignored
		c.Substituted = substituted

		return
	}

	if carried, isFault := errors.AsType[*fault.Fault](err); isFault {
		c.FaultCode = carried.Code().String()
		c.Failure = carried.Message()

		return
	}

	// An error carrying no registered code is one this surface is not supposed
	// to produce. Saying so rather than rendering the text keeps a path, a query
	// or a token out of a response that anybody who may view the printer reads.
	c.Failure = "the call failed with an error carrying no registered code"
}

// CommandLog is the last few commands this process carried, oldest first.
//
// It sits at the API boundary and not on the printer frame path: a command is
// one HTTP request, so the lock here is taken once per command and never once
// per frame or once per subscriber.
type CommandLog struct {
	limit int

	mu      sync.Mutex
	entries []Command
	dropped uint64
}

// NewCommandLog returns a log holding at most limit commands. Zero or less takes
// DefaultCommandLimit.
func NewCommandLog(limit int) *CommandLog {
	if limit <= 0 {
		limit = DefaultCommandLimit
	}

	return &CommandLog{limit: limit, entries: make([]Command, 0, limit)}
}

// Record files one command. The oldest is dropped once the log is full, and how
// many have been dropped is reported beside the entries, so a reader can tell a
// quiet printer from a window that has already scrolled past.
func (l *CommandLog) Record(entry Command) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.entries) >= l.limit {
		copy(l.entries, l.entries[1:])
		l.entries = l.entries[:l.limit-1]
		l.dropped++
	}

	l.entries = append(l.entries, clone(entry))
}

// Recent returns the commands this log holds for one printer, oldest first, and
// how many entries have been dropped from the whole log since it started.
func (l *CommandLog) Recent(printer string) (entries []Command, dropped uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	out := make([]Command, 0, len(l.entries))

	for _, entry := range l.entries {
		if entry.DeviceID == printer {
			out = append(out, clone(entry))
		}
	}

	return out, l.dropped
}

// clone copies the maps and slices a caller could otherwise write through. The
// log outlives the response it is rendered into and the argument map the caller
// handed in, and either side writing through the other is a record that changes
// after it was made.
func clone(entry Command) Command {
	entry.Args = maps.Clone(entry.Args)
	entry.Substituted = maps.Clone(entry.Substituted)
	entry.Ignored = slices.Clone(entry.Ignored)

	return entry
}
