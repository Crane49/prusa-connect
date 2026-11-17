package app

import (
	"context"
	"time"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
)

// reachability puts one printer going away, and coming back, on the bus.
//
// A printer that is unreachable is not a printer that departed: its session is
// still open, so a command still queues for it and it is still in the fleet.
// The two are separate facts and both are published, because a reader told the
// wrong one either drops a commandable printer off a screen or keeps showing a
// machine that is not there.
//
// The rate is bounded rather than the edge delayed. The first change after a
// quiet stretch leaves immediately, which is what somebody watching a cable
// come out is waiting for; every change a bouncing link makes inside the period
// that follows is folded into the state the period ends on. A reader is
// therefore never wrong for longer than one period, and one printer costs at
// most one durable frame per period however fast its link bounces.
type reachability struct {
	plane *devices
	entry *device
	// changed wakes on a change. It is nil for a driver whose session is its
	// connection, and a nil channel is what makes the pump's select ignore it.
	changed <-chan struct{}
	quiet   *time.Timer
	// published is the last reachability a subscriber was given. A printer is
	// admitted with a connection under it, so that is where it starts and an
	// arrival is not followed by a frame repeating what the arrival said.
	published bool
	armed     bool
}

// reachabilityOf returns the reporter for one printer, quiet and holding no
// timer until something changes.
func (d *devices) reachabilityOf(entry *device) *reachability {
	quiet := time.NewTimer(presenceQuiet)
	quiet.Stop()

	return &reachability{
		plane:     d,
		entry:     entry,
		changed:   entry.session.PresenceChanged(),
		quiet:     quiet,
		published: true,
	}
}

// wake takes a change reported while no quiet period is running. One that lands
// inside a period is dropped here and answered by elapsed, which publishes the
// state the period ends on: the same answer, in one frame rather than two.
func (r *reachability) wake() {
	if r.armed {
		return
	}

	r.announce()
}

// elapsed ends a quiet period and publishes whatever the link settled on.
func (r *reachability) elapsed() {
	r.armed = false

	r.announce()
}

// announce publishes the reachability the printer has now, if it is not the one
// the reader already holds, and opens a quiet period over whatever follows.
func (r *reachability) announce() {
	now := r.entry.session.Connected()
	if now == r.published {
		return
	}

	r.published = now

	if now {
		r.plane.meters.reconnected(r.entry.driver, reasonLinkBack)
	} else {
		// Whatever is on the storage when the link comes back may differ.
		r.plane.listings.drop(r.entry.id)
		r.entry.stopPrefetch()
		r.entry.warm.Store(false)
	}

	r.plane.publish(context.Background(), r.entry.id, event.TypeDevicePresenceChanged,
		event.ClassState, "", presenceOf(r.entry, now))

	// Presence is a promoted column, and a promoted column is written as it
	// changes and not on the flush timer. It changes at the rate a cable does
	// and not at the frame rate, and the quiet period above already bounds
	// that to one change per printer per second.
	r.plane.writeSoon(r.entry)

	r.quiet.Reset(presenceQuiet)
	r.armed = true
}

func (r *reachability) stop() { r.quiet.Stop() }

// linkReport puts what this server is making of one printer's frames on the
// bus.
//
// It is the one reading about a printer that no other frame can carry. A
// printer whose frames are all being refused publishes no telemetry, keeps its
// session and reports reachable, so every other frame about it says the machine
// is fine. Without this reading, a reader that only learns from the stream
// would learn nothing about exactly that printer.
//
// It reads on a tick rather than on a change, because the change is a counter
// inside a driver and nothing signals it. The tick is the cost: two atomic
// loads per printer per interval, on the goroutine that is already reading that
// printer, and nothing on the path from a frame to a subscriber pays for it.
type linkReport struct {
	plane *devices
	entry *device
	tick  *time.Ticker

	// published is the reading a reader already holds, and moving is whether
	// the frame that carried it reported either total advancing. The second is
	// what makes a storm that stopped its own last frame: without it a reader
	// keeps a rate on screen for a printer that has gone quiet.
	published httpapi.DeviceLink
	moving    bool

	// last is the reading the latest tick took, published or not. The issue
	// report judges it rather than taking a reading of its own.
	last httpapi.DeviceLink
}

// linkReportInterval is how often one printer's link reading is taken.
//
// A shorter interval would put a frame in front of a person sooner. It would
// also cost one durable event per printer per period during a refusal storm,
// which is the case where the fleet is already the problem.
const linkReportInterval = 5 * time.Second

// linkReportOf returns the reporter for one printer, holding the reading its
// arrival already stated.
//
// A driver that counts nothing is reported as counting nothing, once, by the
// arrival. Nothing about that reading can change while the session lasts, so
// the ticker below never publishes for such a printer.
func (d *devices) linkReportOf(entry *device) *linkReport {
	reading := entry.linkHealth()

	return &linkReport{
		plane:     d,
		entry:     entry,
		tick:      time.NewTicker(linkReportInterval),
		published: reading,
		last:      reading,
	}
}

// sample takes one reading and publishes it if a reader would act on it.
func (r *linkReport) sample() {
	now := r.entry.linkHealth()
	r.last = now

	if !r.worth(now) {
		return
	}

	r.plane.publish(context.Background(), r.entry.id, event.TypeDeviceLinkChanged,
		event.ClassState, "", linkOf(r.entry, now))
}

// worth reports whether a reading is worth a frame, and takes it as published
// when it is.
//
// What is compared is the two totals and not the rate. The rate moves on every
// tick of a storm, so comparing it would put a frame on the bus every interval
// for as long as the storm ran and again for ever after it, because a rate
// decaying towards zero never quite arrives.
//
// A reading whose totals have stopped moving is published once and then not
// again. That one frame is what takes the storm off a reader's screen: the
// totals it carries are the ones the last frame carried, and the rate beside
// them has fallen to what the printer is doing now.
func (r *linkReport) worth(now httpapi.DeviceLink) bool {
	moving := now.RefusedFrames != r.published.RefusedFrames ||
		now.DroppedTelemetry != r.published.DroppedTelemetry

	if !moving && !r.moving {
		return false
	}

	r.published, r.moving = now, moving

	return true
}
