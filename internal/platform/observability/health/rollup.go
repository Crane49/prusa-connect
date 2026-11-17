// Package health reduces one connector's whole device population to a single
// report: counts by state, and a capped list of the devices a person should
// look at.
//
// A full per-device listing is not a health report. It is large, nobody reads
// it, and a dashboard polling it makes the endpoint a load source. The
// per-device view belongs in the pcl_printer_up metric, where a query language
// can select from it.
//
// Nothing here may reach readiness. A server with no printers connected can
// still serve its API, and a readiness probe that fails because a printer is
// unplugged takes the process out of rotation for a normal condition. The gate
// is TestReadinessDoesNotDependOnConnectorHealth in internal/arch.
package health

import (
	"cmp"
	"slices"
	"time"
)

// DefaultCap is how many degraded devices a rollup lists. It is enough to see a
// pattern and short enough to read. The count of what was left out is reported
// beside the list.
const DefaultCap = 20

// DefaultFloor is the connected fraction below which a connector is failing and
// not just degraded. One offline printer in a large farm is a normal condition,
// and a report that calls it a failure gets muted.
const DefaultFloor = 0.5

// State is where one device is, as its connector last observed it.
type State string

const (
	// StateConnected is a device answering.
	StateConnected State = "connected"
	// StateConnecting is a device with a connection attempt in flight.
	StateConnecting State = "connecting"
	// StateDisconnected is a device that is not answering. For a printer that
	// is a normal overnight condition and not a fault.
	StateDisconnected State = "disconnected"
	// StateError is a device whose connector reported a failure.
	StateError State = "error"
	// StateUnknown is a device the connector has not heard from within its
	// grace period. A printer several heartbeats overdue is neither up nor
	// down, and reporting "connected" for it would be a guess.
	StateUnknown State = "unknown"
)

// Status is a connector's verdict over its whole population.
type Status string

const (
	// StatusPass is every device connected.
	StatusPass Status = "pass"
	// StatusDegraded is some devices down, which for a fleet is expected.
	StatusDegraded Status = "degraded"
	// StatusFail is the connected fraction below the floor.
	StatusFail Status = "fail"
)

// Device is one device as its connector last observed it. A connector pushes
// these when they change and is never polled: it already knows its own
// transitions, and probing a whole farm on every request would be expensive.
type Device struct {
	// ID identifies the device within its connector.
	ID string
	// Name is what a person calls the device. Empty leaves the reader the ID.
	Name string
	// State is where it was when the connector last reported.
	State State
	// Reason is machine-readable and stable, for example AuthRejected. It is
	// not a message and not an error string.
	Reason string
	// Since is when it entered this state, so a reader can see a device that
	// has been flapping for an hour.
	Since time.Time
	// Attempts is how many times the connector has tried since then.
	Attempts int
	// LastSeen is when the connector last heard from the device. A zero value
	// means never.
	LastSeen time.Time
}

// Policy is what one connector declares about reading its own population.
// The zero value is usable: no grace period, the default cap and floor.
type Policy struct {
	// Grace is how long a device may go unheard from before its state is
	// reported as unknown instead of whatever it last reported. About three
	// heartbeat intervals is what Kubernetes uses for node readiness. Zero
	// disables it, and a device is then as stale as its connector says.
	Grace time.Duration
	// Cap is how many degraded devices to list. Zero means DefaultCap.
	Cap int
	// Floor is the connected fraction below which the verdict is fail instead
	// of degraded. Zero means DefaultFloor.
	Floor float64
}

// Rollup is one connector's population in one entry.
type Rollup struct {
	// Connector names the connector this rolls up.
	Connector string
	// Status is the verdict over the whole population.
	Status Status
	// Counts is how many devices are in each state. Every state is present,
	// including the ones with no devices, so a client never has to tell a zero
	// from a missing key.
	Counts map[State]int
	// Degraded is the devices worth looking at, longest-broken first, capped.
	Degraded []Device
	// DegradedTruncated is how many degraded devices are not in the list.
	DegradedTruncated int
	// Total is the population size.
	Total int
}

// Roll reduces one connector's devices to a rollup as of now.
//
// The degraded list starts with the device that has been broken longest, so the
// entry a person should look at first is first and the order does not change
// between polls. Ordering by attempt count would reorder on every scrape.
func Roll(connector string, now time.Time, policy Policy, devices []Device) Rollup {
	rollup := Rollup{
		Connector: connector,
		Counts:    emptyCounts(),
		Total:     len(devices),
		Degraded:  []Device{},
	}

	for _, device := range devices {
		device.State = observed(device, now, policy.Grace)
		rollup.Counts[device.State]++

		if device.State != StateConnected {
			rollup.Degraded = append(rollup.Degraded, device)
		}
	}

	slices.SortFunc(rollup.Degraded, byLongestBroken)

	rollup.Status = verdict(rollup.Counts[StateConnected], rollup.Total, policy.Floor)
	rollup.Degraded, rollup.DegradedTruncated = trim(rollup.Degraded, policy.Cap)

	return rollup
}

// observed is the state to report for one device. It is unknown once the
// connector has not heard from the device within the grace period, whatever the
// device last said.
func observed(device Device, now time.Time, grace time.Duration) State {
	if grace <= 0 {
		return device.State
	}

	if device.LastSeen.IsZero() || now.Sub(device.LastSeen) > grace {
		return StateUnknown
	}

	return device.State
}

// verdict is the threshold rule: degraded while some devices are down, and fail
// once the connected fraction falls below the floor. A connector with no
// devices passes.
func verdict(connected, total int, floor float64) Status {
	if total == 0 || connected == total {
		return StatusPass
	}

	if floor <= 0 {
		floor = DefaultFloor
	}

	if float64(connected)/float64(total) < floor {
		return StatusFail
	}

	return StatusDegraded
}

// trim shortens the list and reports what was left out.
func trim(degraded []Device, limit int) ([]Device, int) {
	if limit <= 0 {
		limit = DefaultCap
	}

	if len(degraded) <= limit {
		return degraded, 0
	}

	return degraded[:limit], len(degraded) - limit
}

// byLongestBroken puts the longest-broken device first and breaks ties on the
// identifier, so two devices that changed state in the same instant do not swap
// places between polls. A device with no recorded transition sorts first: it
// has been broken for as long as this process has known about it.
func byLongestBroken(a, b Device) int {
	if order := a.Since.Compare(b.Since); order != 0 {
		return order
	}

	return cmp.Compare(a.ID, b.ID)
}

// emptyCounts is every state at zero, so a rollup names every state it knows
// and not only the populated ones.
func emptyCounts() map[State]int {
	return map[State]int{
		StateConnected:    0,
		StateConnecting:   0,
		StateDisconnected: 0,
		StateError:        0,
		StateUnknown:      0,
	}
}
