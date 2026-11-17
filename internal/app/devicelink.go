package app

import (
	"time"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// linkWindow is the shortest span a link rate is measured over.
//
// It bounds two things at once. A window this long is what stops a reader
// hammering the listing from resetting the measurement to nothing on every
// call, and it is short enough that a person watching a printer sees a
// refusal storm in the reading after the one that started it.
const linkWindow = 2 * time.Second

// linkCounters is the published half of a session that says what its connection
// has cost. It is named here so the assertions below read as this package's own;
// the contract a driver author writes against is connectorapi.CountsLink.
type linkCounters = connectorapi.CountsLink

// link turns a driver's two running totals into the rate somebody at a bench can
// act on.
//
// A total answers the wrong question. Sixty refused frames on a printer that has
// been up for a week is nothing, sixty in the last minute is firmware speaking
// something this build does not know, and a reader handed only the total cannot
// tell those apart without remembering the last figure they saw.
//
// The span is carried across two marks rather than one. A single mark reset
// every window would report a printer refusing four frames a second as perfectly
// quiet, for whichever read landed just after a reset, and that reading is the
// one a person acts on.
type link struct {
	counters linkCounters

	// mark is when the open window began, and the two totals are what the
	// driver had counted then.
	mark    time.Time
	refused uint64
	dropped uint64

	// The window that ended at mark, kept so the span behind a rate never
	// collapses to nothing.
	priorSpan    time.Duration
	priorRefused uint64
	priorDropped uint64
}

// observe reads the driver's counters and returns the link as it stands.
//
// It rolls the window rather than sampling on a clock of its own. Nothing on the
// path from a printer frame to a subscriber pays for this: the driver counts what
// it was already counting, and the two atomic loads happen on the goroutine
// answering a read.
func (l *link) observe(now time.Time) httpapi.DeviceLink {
	if l.counters == nil {
		return httpapi.DeviceLink{}
	}

	refused, dropped := l.counters.Refused(), l.counters.Dropped()

	// A total that went backwards is a counter that was replaced underneath
	// this entry. Re-basing on it reports one reading with no span, where
	// subtracting would report a rate near the width of a uint64.
	if l.mark.IsZero() || refused < l.refused || dropped < l.dropped {
		*l = link{counters: l.counters, mark: now, refused: refused, dropped: dropped}

		return httpapi.DeviceLink{Counted: true, RefusedFrames: refused, DroppedTelemetry: dropped}
	}

	since := now.Sub(l.mark)
	sinceRefused, sinceDropped := refused-l.refused, dropped-l.dropped

	span := since + l.priorSpan
	windowRefused, windowDropped := sinceRefused+l.priorRefused, sinceDropped+l.priorDropped

	if since >= linkWindow {
		l.priorSpan, l.priorRefused, l.priorDropped = since, sinceRefused, sinceDropped
		l.mark, l.refused, l.dropped = now, refused, dropped
	}

	return httpapi.DeviceLink{
		Counted:                   true,
		RefusedFrames:             refused,
		RefusedFramesPerMinute:    perMinute(windowRefused, span),
		DroppedTelemetry:          dropped,
		DroppedTelemetryPerMinute: perMinute(windowDropped, span),
		MeasuredOverSeconds:       span.Seconds(),
	}
}

// perMinute is a count over a span. A span of nothing carries no rate, which is
// the first reading of a session rather than a printer refusing infinitely fast.
func perMinute(count uint64, span time.Duration) float64 {
	if span <= 0 {
		return 0
	}

	return float64(count) / span.Minutes()
}

// linkHealth is what this printer's connection has cost, as a rate.
//
// It is read here because this is where a printer is read at all, and it is
// where the reading is cheapest: no goroutine, no timer per printer, and nothing
// on the frame path. What it costs is two atomic loads on the goroutine already
// answering the request.
func (e *device) linkHealth() httpapi.DeviceLink {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.gone != "" {
		return e.lastLink
	}

	return e.link.observe(time.Now())
}
