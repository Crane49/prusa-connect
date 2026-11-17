package app

import (
	"time"

	"github.com/prusa3d/prusa-connect/internal/platform/observability"
	"github.com/prusa3d/prusa-connect/internal/platform/observability/health"
)

// fleetGaugeInterval is how often the fleet's gauges are restated.
//
// It is the record flush's own period, and it runs on the record flush's own
// goroutine, so measuring the fleet costs this process no goroutine and no
// timer of its own. A Prometheus scrape is fifteen seconds by convention, so a
// gauge two seconds old is one the scrape cannot tell from a fresh one.
const fleetGaugeInterval = 2 * time.Second

// fleetSampler is what one sampler carries between walks.
//
// It remembers the printers it has written series for, so the ones that have
// gone can be dropped, and the drivers, so a driver whose last printer departed
// reports zero rather than vanishing from the scrape. A series that stops being
// exported and a series reporting zero are different facts, and an alert on
// "no printers connected" needs the second.
//
// It also holds each handle rather than resolving it per walk. A resolution is
// a label hash and two slice allocations, and this walks the whole fleet twice
// a second. One goroutine owns the whole struct, so none of it is locked.
type fleetSampler struct {
	printers map[string]printerHandles
	drivers  map[string]driverHandles
	// bounded is each printer's two totals, clipped and refused, as the last
	// walk read them, so the counter is given the difference. The entry carries the
	// total, which is what survives a walk that did not happen.
	bounded map[string]boundedCounts
}

// boundedCounts is one printer's running totals of what this host did not keep
// of what it sent.
type boundedCounts struct {
	clipped uint64
	refused uint64
}

// printerHandles are the per-printer gauges of one activation. They are dropped
// with the printer, and taken again when it comes back: a reconnection can
// carry another firmware, and the info gauge labels the firmware.
type printerHandles struct {
	up             observability.Gauge
	info           observability.Gauge
	lastFrame      observability.Gauge
	connectedSince observability.Gauge
	// model and firmware are what info was labelled with, so a printer that
	// came back as another model or on another firmware is noticed rather than
	// reported under the old labels.
	model    string
	firmware string
}

// driverHandles are one driver's two state gauges.
type driverHandles struct {
	connected    observability.Gauge
	disconnected observability.Gauge
}

func newFleetSampler() *fleetSampler {
	return &fleetSampler{
		printers: map[string]printerHandles{},
		drivers:  map[string]driverHandles{},
		bounded:  map[string]boundedCounts{},
	}
}

// handlesFor returns one printer's gauges, taking them the first time and
// whenever the model or firmware it reports has moved. The info series of the
// old labels is dropped then, or the printer would be counted under both.
func (s *fleetSampler) handlesFor(meters fleetMeters, entry *device) printerHandles {
	firmware := firmwareText(entry.identity.Firmware)

	held, taken := s.printers[entry.id]
	if taken && held.model == entry.identity.Model && held.firmware == firmware {
		return held
	}

	if taken {
		meters.forgetInfo(entry.id)
	}

	held = meters.handles(entry, firmware)
	s.printers[entry.id] = held

	return held
}

// bound hands the counter what this printer has had cut or refused since the
// last walk.
//
// The reading is taken here and not in device.record, because record is the
// frame path: one printer sends up to fifteen patches per frame at four hertz,
// and resolving a metric handle there is a label hash on the one path this
// project refuses to put work on. Two integers under a lock the writer already
// holds is what it costs instead.
func (s *fleetSampler) bound(meters fleetMeters, entry *device) {
	clipped, refused := entry.textCounts()

	last := s.bounded[entry.id]
	if clipped < last.clipped || refused < last.refused {
		// A counter that went backwards is an entry replaced under this
		// sampler, so the totals are read as this activation's own.
		last = boundedCounts{}
	}

	meters.bounded(entry.driver, clipped-last.clipped, refused-last.refused)

	s.bounded[entry.id] = boundedCounts{clipped: clipped, refused: refused}
}

// statesFor returns one driver's two gauges.
func (s *fleetSampler) statesFor(meters fleetMeters, driver string) driverHandles {
	held, taken := s.drivers[driver]
	if taken {
		return held
	}

	held = driverHandles{
		connected:    meters.connections(driver, health.StateConnected),
		disconnected: meters.connections(driver, health.StateDisconnected),
	}
	s.drivers[driver] = held

	return held
}

// sampleFleet restates every gauge the fleet carries and drops the series of
// every printer that has gone.
//
// The gauges are sampled rather than moved on each event. A gauge moved on an
// event has to be moved exactly once by every path that can cause it, and the
// path that forgets leaves a number that stays wrong until the process
// restarts. A walk of the live table cannot drift: what it reads is what it
// reports.
//
// It also keeps the frame path clear. One printer sends up to fifteen state
// patches per frame at four hertz, and a metric handle resolved per patch is a
// map probe and a label hash on the one path this project refuses to put work
// on (go-standards, hot paths).
func (d *devices) sampleFleet(sampler *fleetSampler) {
	live := d.liveEntries()

	counts := map[string]map[health.State]int{}
	present := make(map[string]struct{}, len(live))

	for _, entry := range live {
		up := entry.connected()

		byState, seen := counts[entry.driver]
		if !seen {
			byState = map[health.State]int{}
			counts[entry.driver] = byState
		}

		byState[presenceState(up)]++

		present[entry.id] = struct{}{}

		handles := sampler.handlesFor(d.meters, entry)
		handles.up.Set(boolGauge(up))
		handles.lastFrame.Set(float64(entry.seenAt().Unix()))
		handles.connectedSince.Set(float64(entry.since.Unix()))
		handles.info.Set(1)

		sampler.bound(d.meters, entry)
	}

	for id := range sampler.printers {
		if _, still := present[id]; still {
			continue
		}

		d.meters.forget(id)
		delete(sampler.printers, id)
		delete(sampler.bounded, id)
	}

	d.stateCounts(sampler, counts)
}

// stateCounts writes one gauge per driver per connection state, including the
// zeroes. A driver that appeared in an earlier walk keeps both of its series,
// because a fleet that has all gone away is a fleet reporting zero connected.
func (d *devices) stateCounts(sampler *fleetSampler, counts map[string]map[health.State]int) {
	for driver := range counts {
		sampler.statesFor(d.meters, driver)
	}

	for driver := range sampler.drivers {
		held := sampler.statesFor(d.meters, driver)
		held.connected.Set(float64(counts[driver][health.StateConnected]))
		held.disconnected.Set(float64(counts[driver][health.StateDisconnected]))
	}
}

// liveEntries is the live table as a slice, taken under the plane's own lock
// and read afterwards. Each entry has a lock of its own, and holding both at
// once would put a second order on them for the sake of a measurement.
func (d *devices) liveEntries() []*device {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]*device, 0, len(d.entries))
	for _, entry := range d.entries {
		out = append(out, entry)
	}

	return out
}
