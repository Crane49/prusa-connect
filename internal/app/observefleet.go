package app

// What the composition root writes into the metric catalog for the fleet, the
// bus and the process itself.
//
// It lives here because internal/app is the only package that can see both the
// device plane and a registry. internal/connector and internal/transport may
// not import internal/platform. The host emits a printer's series and the
// driver never does, so a community driver cannot invent a name or label a
// series with the wrong printer. Supervised resources follow the same rule.

import (
	"errors"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/prusa3d/prusa-connect/internal/platform/observability"
	"github.com/prusa3d/prusa-connect/internal/platform/observability/health"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The catalog series this package writes. They are constants so a rename
// reaches every call site.
const (
	seriesBuildInfo       = "pcl_build_info"
	seriesConnections     = "pcl_printer_connections"
	seriesAttempts        = "pcl_printer_connection_attempts_total"
	seriesReconnects      = "pcl_printer_reconnects_total"
	seriesCommandsPending = "pcl_printer_commands_pending"
	seriesCommandDuration = "pcl_printer_command_duration_seconds"
	seriesEventsPublished = "pcl_events_published_total"
	seriesRequestDuration = "pcl_http_request_duration_seconds"
	seriesPrinterUp       = "pcl_printer_up"
	seriesPrinterInfo     = "pcl_printer_info"
	seriesLastFrame       = "pcl_printer_last_frame_timestamp_seconds"
	seriesConnectedSince  = "pcl_printer_connected_since_timestamp_seconds"
	seriesPrinterErrors   = "pcl_printer_errors_total"
	seriesStateBounded    = "pcl_printer_state_bounded_total"
	seriesResourceUp      = "pcl_resource_up"
)

// The label values this package supplies. An outcome is one label value, so a
// dashboard divides one series and does not join two.
const (
	outcomeAdmitted = "admitted"
	outcomeRefused  = "refused"
	outcomeOK       = "ok"
	outcomeRefusal  = "refusal"
	outcomeError    = "error"

	reasonRearrived = "rearrived"
	reasonLinkBack  = "link-restored"

	boundClipped = "clipped"
	boundRefused = "refused"
)

// fanoutDeviceID is the label a per-device series carries. It is the catalog's
// own spelling, and it is here because Drop matches on it.
const fanoutDeviceID = "device_id"

// fleetMeters is the device plane's part of the metric catalog: handles it
// writes, and no way to reach the registry behind them.
//
// The zero value records nothing, for a test that is not about metrics. Every
// accessor answers with a handle either way, so no call site branches on
// whether metrics are on.
type fleetMeters struct {
	owner *observed
	// events is one counter per declared event type, resolved at construction.
	//
	// It is a table for two reasons. One event is published per state patch,
	// which is fifteen per frame per printer at four hertz, so resolving a
	// handle there would cost a label hash and two slice allocations on the
	// busiest path in the process. And whoever installed a module writes the
	// type on its emissions, so a series per type as given would be a series
	// per string a third party chose.
	events map[string]observability.Counter
	// otherEvents counts every type outside the declared vocabulary, so that
	// label stays bounded.
	otherEvents observability.Counter
}

// meters returns the plane's view of the registry. A nil registry gives the
// zero fleetMeters, which records nothing.
func (o *observed) meters() fleetMeters {
	if o == nil {
		return fleetMeters{}
	}

	return o.fleet
}

// otherEventType is the one label value every event outside the declared
// vocabulary is counted under.
const otherEventType = "other"

// newFleetMeters resolves the handles that can be resolved before anything has
// happened. The rest carry a driver or a printer id and are resolved where they
// are written.
func (o *observed) newFleetMeters() fleetMeters {
	declared := event.Types()

	meters := fleetMeters{
		owner:       o,
		events:      make(map[string]observability.Counter, len(declared)),
		otherEvents: o.counter(seriesEventsPublished, otherEventType),
	}

	for _, kind := range declared {
		meters.events[kind] = o.counter(seriesEventsPublished, kind)
	}

	return meters
}

// connections is the gauge for one driver's printers in one connection state.
func (m fleetMeters) connections(driver string, state health.State) observability.Gauge {
	if m.owner == nil {
		return observability.NopGauge()
	}

	return m.owner.gauge(seriesConnections, driver, string(state))
}

// admitted counts one printer this driver offered and the outcome the host gave
// it. A refusal is the same series with another outcome, so attempts and
// failures divide.
func (m fleetMeters) admitted(driver, outcome string) {
	m.count(seriesAttempts, driver, outcome)
}

// reconnected counts one printer this host saw come back, by what it came back
// from. A driver keeps a session across a broken link, so the two reasons are a
// link that came back and a machine that announced itself again.
func (m fleetMeters) reconnected(driver, reason string) {
	m.count(seriesReconnects, driver, reason)
}

// commanded records one command's outcome and how long the printer took.
func (m fleetMeters) commanded(driver, verb, outcome string, took time.Duration) {
	if m.owner == nil {
		return
	}

	m.owner.observer(seriesCommandDuration, driver, verb, outcome).Observe(took.Seconds())
}

// pending is the gauge of commands sent to one driver's printers and not yet
// answered. It moves by one either side of the call, so a printer that is away
// holds its command on this gauge for as long as the caller waits.
func (m fleetMeters) pending(driver string) observability.Gauge {
	if m.owner == nil {
		return observability.NopGauge()
	}

	return m.owner.gauge(seriesCommandsPending, driver)
}

// failed counts one registered error code against one printer. It is the only
// per-printer counter. A fleet-wide count would say an error happened and not
// which machine has it.
func (m fleetMeters) failed(code, printer string) {
	m.count(seriesPrinterErrors, code, printer)
}

// bounded records what one driver's printers have had cut or refused since the
// last walk. It takes deltas and not totals, because a counter is moved by
// adding to it. The entry keeps the totals.
//
// It is aggregate and not per printer. A printer may fan out to six series and
// observability.Fleet already holds five, so two more per printer would go
// over that limit.
func (m fleetMeters) bounded(driver string, clipped, refused uint64) {
	if m.owner == nil {
		return
	}

	if clipped > 0 {
		m.owner.counter(seriesStateBounded, driver, boundClipped).Add(float64(clipped))
	}

	if refused > 0 {
		m.owner.counter(seriesStateBounded, driver, boundRefused).Add(float64(refused))
	}
}

// published counts one event on the bus, by envelope type.
func (m fleetMeters) published(kind string) {
	if m.owner == nil {
		return
	}

	if handle, declared := m.events[kind]; declared {
		handle.Add(1)

		return
	}

	m.otherEvents.Add(1)
}

// handles takes one printer's four gauges, which the sampler then holds for as
// long as the printer is here.
//
// pcl_printer_last_frame_timestamp_seconds is a timestamp and not a count. The
// host is handed state patches and not frames. A frame's patches all arrive at
// one instant, so the instant belongs to the frame and a count of patches
// would not count frames. So the catalog has this gauge and no frame counter.
func (m fleetMeters) handles(entry *device, firmware string) printerHandles {
	if m.owner == nil {
		return printerHandles{
			up:             observability.NopGauge(),
			info:           observability.NopGauge(),
			lastFrame:      observability.NopGauge(),
			connectedSince: observability.NopGauge(),
			model:          entry.identity.Model,
			firmware:       firmware,
		}
	}

	return printerHandles{
		up:             m.owner.gauge(seriesPrinterUp, entry.id),
		info:           m.owner.gauge(seriesPrinterInfo, entry.driver, entry.identity.Model, firmware, entry.id),
		lastFrame:      m.owner.gauge(seriesLastFrame, entry.id),
		connectedSince: m.owner.gauge(seriesConnectedSince, entry.id),
		model:          entry.identity.Model,
		firmware:       firmware,
	}
}

// forget drops every series carrying one printer's id, so the number of
// per-printer series is bounded by the fleet and not by every machine that
// ever dialled this process.
func (m fleetMeters) forget(id string) {
	if m.owner == nil {
		return
	}

	for _, def := range observability.Fleet() {
		m.owner.registry.Drop(def, map[string]string{fanoutDeviceID: id})
	}
}

// forgetInfo drops one printer's info series, whatever model and firmware it
// was labelled with.
func (m fleetMeters) forgetInfo(id string) {
	if m.owner == nil {
		return
	}

	for _, def := range observability.Fleet() {
		if def.Name() == seriesPrinterInfo {
			m.owner.registry.Drop(def, map[string]string{fanoutDeviceID: id})
		}
	}
}

// count adds one to a catalog counter.
func (m fleetMeters) count(name string, values ...string) {
	if m.owner == nil {
		return
	}

	m.owner.counter(name, values...).Add(1)
}

// resourceUp records whether an instance of a supervised resource is serving.
// It is the only writer of pcl_resource_up, and it takes the status the plane
// publishes.
func (o *observed) resourceUp(kind, name string, serving bool) {
	o.gauge(seriesResourceUp, kind, name).Set(boolGauge(serving))
}

// timeRequest records one served HTTP request. httpapi.Config carries it as a
// function, because internal/transport may not reach a registry.
func (o *observed) timeRequest(method, route string, status int, took time.Duration) {
	o.observer(seriesRequestDuration, method, route, strconv.Itoa(status)).Observe(took.Seconds())
}

// stamp states the build this process is running. The gauge is always 1 and
// the facts are in its labels, which is the Prometheus convention for an info
// metric.
func (o *observed) stamp(version string) {
	o.gauge(seriesBuildInfo, version, revision(), runtime.Version()).Set(1)
}

// revision is the commit this binary was built from, and "unknown" for a build
// with no version control stamp. Go records it for a build made inside a
// repository, and a build from an unpacked module has none.
func revision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}

	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			return setting.Value
		}
	}

	return "unknown"
}

// commandOutcome is the label value a dashboard divides a command series by. A
// printer that refused a command is not a printer this server could not reach,
// and one value for both would hide a firmware refusal behind a network fault.
//
// A refusal is read off the result and an error is read off the error, as the
// driver contract on connectorapi.Result says. settleStatus divides the two
// the same way.
func commandOutcome(result connectorapi.Result, err error) string {
	switch {
	case err != nil:
		return outcomeError
	case !result.OK:
		return outcomeRefusal
	}

	return outcomeOK
}

// codeOf is the registered error code an error carries, and "unregistered" for
// one that carries none. The label is bounded by the code registry, so a
// per-printer counter is not one series per message.
//
// A nil error has no code, and the answer is the empty string. Every frame
// carrying a fault code declares the field omitempty, so a reader takes an
// absent code as "nothing went wrong". Answering "unregistered" for a nil
// error would put a fault code on the settled frame of every transfer that
// succeeded.
func codeOf(err error) string {
	if err == nil {
		return ""
	}

	if carried, ok := errors.AsType[*fault.Fault](err); ok {
		return carried.Code().String()
	}

	return "unregistered"
}

// boolGauge is the 0 or 1 a state gauge carries.
func boolGauge(yes bool) float64 {
	if yes {
		return 1
	}

	return 0
}
