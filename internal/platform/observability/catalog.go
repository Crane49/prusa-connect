package observability

// DefaultFanoutBudget is how many metrics may carry a per-device dimension.
//
// Each one costs a series per printer, so the next metric past the budget costs
// a whole fleet of series. A deployment that wants one raises the budget in its
// configuration.
const DefaultFanoutBudget = 6

// Core is the aggregate metric set: always on, and bounded by the number of
// drivers, sinks and routes, not by the size of the fleet.
func Core() []Def {
	return []Def{
		NewGauge("pcl_build_info",
			"Always 1. The build this process is running, in its labels.",
			[]Label{"version", "commit", "go_version"}),
		NewGauge("pcl_printer_connections",
			"Printers in each connection state, by driver.",
			[]Label{"driver", "state"}),
		NewCounter("pcl_printer_connection_attempts_total",
			"Connection attempts, by driver and outcome.",
			[]Label{"driver", "outcome"}),
		NewCounter("pcl_printer_reconnects_total",
			"Reconnections this host observed, by driver and reason.",
			[]Label{"driver", "reason"}),
		NewGauge("pcl_printer_commands_pending",
			"Commands sent to a printer and not yet answered, by driver.",
			[]Label{"driver"}),
		NewHistogram("pcl_printer_command_duration_seconds",
			"Time from queueing a command to its result, by driver, command and outcome.",
			LatencySeconds(), "driver", "command", "outcome"),
		NewCounter("pcl_events_published_total",
			"Events published on the bus, by envelope type.",
			[]Label{"type"}),
		NewHistogram("pcl_http_request_duration_seconds",
			"Time to serve an HTTP request, by method, route template and status code.",
			LatencySeconds(), "method", "route", "code"),
		NewCounter("pcl_printer_state_bounded_total",
			"Values a driver sent that this server did not keep whole, by driver and by which bound "+
				"stopped them: clipped is one value over the per-value bound, refused is one path "+
				"a full state tree would not take.",
			[]Label{"driver", "bound"}),
		NewCounter("pcl_log_lines_total",
			"Log lines emitted, by level.",
			[]Label{"level"}),
		NewCounter("pcl_log_lines_dropped_total",
			"Log lines sampling discarded before they were written, by level.",
			[]Label{"level"}),
		NewCounter("pcl_mdns_packets_dropped_total",
			"mDNS packets the announcer or the listener had no room for, because it had not read the ones before.",
			nil),
	}
}

// Fleet is the per-printer set, and it is the whole fan-out allowance. Every
// metric here is a counter or a gauge. The same question asked with a histogram
// would cost one series per printer per bucket.
func Fleet() []Def {
	return []Def{
		NewGauge("pcl_printer_up",
			"1 while a printer is connected, 0 otherwise.",
			nil, DeviceID),
		NewGauge("pcl_printer_info",
			"Always 1. A printer's driver, model and firmware, in its labels.",
			[]Label{"driver", "model", "firmware"}, DeviceID),
		NewGauge("pcl_printer_last_frame_timestamp_seconds",
			"When the last frame arrived from a printer, as a unix timestamp.",
			nil, DeviceID),
		NewGauge("pcl_printer_connected_since_timestamp_seconds",
			"When a printer's current connection was established, as a unix timestamp.",
			nil, DeviceID),
		NewCounter("pcl_printer_errors_total",
			"Errors reported for a printer, by registered error code.",
			[]Label{"code"}, DeviceID),
	}
}

// Resource is what the host records for every supervised resource, whatever
// contract it serves. The host emits these, never the resource itself, so a
// community plugin is measured the same way as a built-in one and cannot invent
// its own names.
func Resource() []Def {
	return []Def{
		NewCounter("pcl_resource_operations_total",
			"Calls into a resource, by kind, instance, operation and outcome.",
			[]Label{"kind", "resource", "operation", "outcome"}),
		NewHistogram("pcl_resource_operation_duration_seconds",
			"Time a resource took to answer a call, by kind, instance and operation.",
			LatencySeconds(), "kind", "resource", "operation"),
		NewGauge("pcl_resource_up",
			"1 while a resource is serving, 0 otherwise.",
			[]Label{"kind", "resource"}),
		NewCounter("pcl_breaker_transitions_total",
			"Circuit breaker state changes, by kind and instance.",
			[]Label{"kind", "resource"}),
		NewCounter("pcl_resource_rejected_total",
			"Calls the host refused before they reached a resource, by kind, instance and operation. "+
				"It carries no duration: a refusal took no time in the resource, and recording it as a "+
				"fast call drags a latency panel towards zero at the moment the resource stops answering.",
			[]Label{"kind", "resource", "operation"}),
		NewCounter("pcl_resource_starts_total",
			"Constructions the supervisor has made, by kind and instance.",
			[]Label{"kind", "resource"}),
		NewCounter("pcl_resource_dropped_total",
			"Items a resource's queue lost, by kind and instance.",
			[]Label{"kind", "resource"}),
		NewCounter("pcl_resource_panics_total",
			"Panics the host recovered inside a resource, by kind and instance.",
			[]Label{"kind", "resource"}),
		NewCounter("pcl_resource_state_transitions_total",
			"Derived state changes, by kind and instance.",
			[]Label{"kind", "resource"}),
		NewCounter("pcl_resource_stale_reports_total",
			"Status reports discarded because the incarnation that sent them is gone.",
			[]Label{"kind", "resource"}),
		NewCounter("pcl_resource_wedged_total",
			"Resources abandoned because they did not return inside a budget the host could not enforce.",
			[]Label{"kind", "resource"}),
	}
}

// OptIn is off by default and costs the fan-out budget to turn on. It is empty
// in this build.
//
// A metric joins it once there is a configuration key that raises the budget
// and something in the process that produces the values.
func OptIn() []Def { return nil }

// Catalog is everything registered by default: the aggregate set, the fan-out
// allowance and the per-resource set.
//
// Every name in it has a writer in this process, which
// TestEveryCatalogFamilyHasAWriter checks. A family with no producer is a name
// an operator can point a dashboard at and never see, and the dashboard then
// reads as healthy.
func Catalog() []Def {
	defs := Core()
	defs = append(defs, Fleet()...)
	defs = append(defs, Resource()...)

	return defs
}
