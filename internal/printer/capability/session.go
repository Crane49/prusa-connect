package capability

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// Session is the host's half of a printer connection: it holds the resolved
// capability set and checks every command against it before the driver is
// called.
//
// The check is above the driver, so it is written once and not once per
// dialect. A driver reached with an argument nothing checked would be
// reachable from an API route. With the check here, a new firmware revision is
// a profile edit.
//
// A resolution is swapped whole and never edited, so a caller that read the
// set and the epoch together read one activation's answer. The pointer is
// atomic because a reconnecting printer resolves on the driver's goroutine
// while an API request is reading.
type Session struct {
	inner   connectorapi.Session
	current atomic.Pointer[activation]
}

// activation is one resolution together with what a reader takes off it that is
// not the set itself.
//
// names is every capability key, sorted, which is all the fleet listing
// renders. It is computed when the resolution is stored, because the listing
// asks it of every printer the caller may see and the answer cannot change
// until the next activation. Computing it per read would deep-copy the map for
// every row.
type activation struct {
	resolution Resolution
	names      []string
}

// newActivation walks the resolved set once, so nothing after this has to.
func newActivation(resolution Resolution) *activation {
	names := make([]string, 0, len(resolution.Capabilities))
	for key := range resolution.Capabilities {
		names = append(names, key.String())
	}

	slices.Sort(names)

	return &activation{resolution: resolution, names: names}
}

// Wrap returns the host session for a driver session and the capability set the
// host resolved.
//
// The host's resolution is authoritative, not the driver's own
// Capabilities: the driver reports what it believes, and the host commands only
// what it resolved and enabled.
func Wrap(inner connectorapi.Session, resolution Resolution) *Session {
	s := &Session{inner: inner}
	s.current.Store(newActivation(resolution))

	return s
}

// Adopt replaces the resolution this session commands against.
//
// A printer that reconnects onto a session the driver still holds is resolved
// again, because a disconnection the host did not watch can carry a firmware
// update or a corrected profile. A host that kept the first answer would
// command an earlier activation of the machine.
func (s *Session) Adopt(resolution Resolution) { s.current.Store(newActivation(resolution)) }

// Capabilities returns the resolved set and its epoch.
//
// The set is a copy. connectorapi.Session promises both are immutable for the
// life of the epoch, and handing out the live map would let a caller write a
// phantom capability into the map Do validates against, or crash the process on
// a concurrent read and write.
//
// A caller that wants only the key names asks CapabilityNames, which answers
// without the copy.
func (s *Session) Capabilities() (connectorapi.Set, uint64) {
	current := s.current.Load()

	return current.resolution.Capabilities.Clone(), current.resolution.Epoch
}

// CapabilityNames returns every key in the resolved set, sorted, and the epoch
// they belong to. It is what the fleet listing publishes for one printer.
//
// The slice is a copy of one this session keeps, so a caller may sort it,
// truncate it or write into it without editing what the next reader is handed.
// Copying a list of names is cheap. The map copy Capabilities makes carries
// every supported-value slice with it.
func (s *Session) CapabilityNames() (names []string, epoch uint64) {
	current := s.current.Load()

	return slices.Clone(current.names), current.resolution.Epoch
}

// Resolution returns where the capability set came from, for the birth event
// and for diagnostics. Its capability set is a copy, for the same reason
// Capabilities returns one.
func (s *Session) Resolution() Resolution {
	out := s.current.Load().resolution
	out.Capabilities = out.Capabilities.Clone()

	return out
}

// Digest is the stable hash of the capability set in force. It answers "did
// this resolve to the same thing" without copying the set, which Resolution and
// Capabilities both do.
func (s *Session) Digest() string { return s.current.Load().resolution.Digest }

// ProfileMatched reports whether a capability profile covered the identity the
// device reported. False means a reduced set, not a refusal.
//
// It reads the one field without copying the capability set, which Resolution
// does. The fleet listing asks this of every printer the caller may see, so a
// copy per printer would allocate in proportion to the fleet on every request.
func (s *Session) ProfileMatched() bool { return s.current.Load().resolution.ProfileMatched }

// Do validates a command against the resolved descriptor and, only if it
// passes, sends it to the driver. A refusal carries the code and the parameters
// a caller acts on; the driver is not called at all.
func (s *Session) Do(ctx context.Context, cmd connectorapi.Command) (connectorapi.Result, error) {
	checked, err := s.current.Load().resolution.Capabilities.Validate(cmd)
	if err != nil {
		return connectorapi.Result{}, err
	}

	result, err := s.inner.Do(ctx, checked)
	if err != nil {
		// The result travels with the error. A driver that refused and gave the
		// refusal a code answers with both, and the machine reason is on the
		// result. A driver that failed outright returns the zero result.
		return result, fmt.Errorf("printer command %s: %w", checked.Verb, err)
	}

	return result, nil
}

// State forwards the driver's state patches.
func (s *Session) State() <-chan connectorapi.StatePatch { return s.inner.State() }

// Presence is a driver session that outlives the connection underneath it and
// can say whether the device is reachable right now.
//
// It is optional. A dialect whose session ends with its transport is reachable
// while the session exists. A dialect that queues for a machine that is
// switched off is not, and it implements Presence.
//
// A driver that implements it gives both the state and the changes. A host
// with the state and no changes would have to poll. A host with the changes
// and no state would have to trust the order they arrived in.
type Presence interface {
	// Connected reports whether the device is reachable right now.
	Connected() bool
	// PresenceChanged wakes a reader on every change to reachability. It
	// carries no value. The reader takes the answer from Connected, so a
	// wake-up that arrives after two more changes still reports the present
	// state. It closes with the session.
	PresenceChanged() <-chan struct{}
}

// Connected reports whether the device is reachable right now. A driver whose
// session is its connection does not implement Presence, and its device is
// reachable while the session is held.
func (s *Session) Connected() bool {
	if reachable, asks := As[Presence](s.inner); asks {
		return reachable.Connected()
	}

	return true
}

// PresenceChanged wakes a reader whenever the device becomes unreachable or
// reachable again. It is nil for a driver whose session is its connection,
// which never has a change to report and whose reader blocks on it forever.
func (s *Session) PresenceChanged() <-chan struct{} {
	if reachable, asks := As[Presence](s.inner); asks {
		return reachable.PresenceChanged()
	}

	return nil
}

// Identity returns what the device said it is.
func (s *Session) Identity() connectorapi.Identity { return s.inner.Identity() }

// Close releases the driver session.
func (s *Session) Close() error { return s.inner.Close() }
