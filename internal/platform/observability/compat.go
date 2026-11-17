package observability

// What the shipping server exposes, and what replaces it.
//
// That server serves the series below at /metrics through the OTel Prometheus
// exporter with default options. The exporter stamps each one with
// otel_scope_name, otel_scope_version and otel_scope_schema_url, and publishes
// a target_info gauge beside them. None of these names is prefixed, so the pcl_
// namespace of the new catalog cannot collide with them and one process can
// serve both sets during a migration.
//
// Some of them are a bucketed metric carrying printer_id, which the new catalog
// refuses. Those are not preserved.

// LegacySeries is one metric the shipping server exposes, and what the new
// catalog answers the same question with.
type LegacySeries struct {
	// Name is the series as it appears in a scrape.
	Name string
	// Kind is what it is exposed as.
	Kind Kind
	// Labels are the dimensions it carries, before the exporter adds its own.
	Labels []string
	// Successor is the catalog metric that answers the same question, empty
	// when the catalog does not answer it yet.
	Successor string
	// Note says what changes for a dashboard written against this series.
	Note string
}

// legacyFanout is the per-device dimension the shipping server spells its
// series with. The new catalog spells the same dimension device_id. These rows
// record what the shipping server publishes, so the old name is read only
// here.
const legacyFanout = "printer_id"

// FanoutHistogram reports whether this legacy series is a bucketed metric
// carrying a per-device dimension, the shape the new catalog refuses.
func (l LegacySeries) FanoutHistogram() bool {
	if l.Kind != KindHistogram {
		return false
	}

	for _, label := range l.Labels {
		if label == legacyFanout || isFanout(Label(label)) {
			return true
		}
	}

	return false
}

// Legacy is every metric the shipping server exposes, with the catalog series
// that replaces it. It is data and not prose, so a test can read it.
//
//nolint:funlen // one row per shipping series, and a shorter form would hide the table.
func Legacy() []LegacySeries {
	return []LegacySeries{
		{
			Name: "websocket_connected_printers", Kind: KindGauge,
			Labels:    []string{"printer_id"},
			Successor: "pcl_printer_up",
			Note:      "A gauge per printer either way. The new one is 0 or 1, not a running delta.",
		},
		{
			Name: "websocket_messages_received_total", Kind: KindCounter,
			Labels:    []string{"printer_id", "message_type"},
			Successor: "",
			Note: "No successor. A frame exists inside the driver's socket, and the host receives " +
				"state patches, several per frame. A host-side count would be a wrong number, so " +
				"there is none. pcl_printer_last_frame_timestamp_seconds answers whether a printer " +
				"is still reporting.",
		},
		{
			Name: "websocket_messages_sent_total", Kind: KindCounter,
			Labels:    []string{"printer_id", "message_type"},
			Successor: "",
			Note: "No successor, for the reason given on the received counter. " +
				"pcl_printer_command_duration_seconds counts what the host asked a printer to do.",
		},
		{
			Name: "websocket_message_bytes_received_total", Kind: KindCounter,
			Labels:    []string{"printer_id"},
			Successor: "",
			Note: "No successor. Bytes are counted where they are read, which is inside the " +
				"driver's socket, and no driver contract carries the total up to the host. " +
				"A panel that needs one printer's link reads the refusal and drop rates the " +
				"printer's own listing entry carries.",
		},
		{
			Name: "websocket_message_bytes_sent_total", Kind: KindCounter,
			Labels:    []string{"printer_id"},
			Successor: "",
			Note:      "No successor, for the reason given on the received counter.",
		},
		{
			Name: "websocket_connection_errors_total", Kind: KindCounter,
			Labels:    []string{"printer_id", "error_type"},
			Successor: "pcl_printer_connection_attempts_total",
			Note:      "Outcome is a label on one series, so attempts and failures divide.",
		},
		{
			Name: "websocket_message_errors_total", Kind: KindCounter,
			Labels:    []string{"printer_id", "error_type"},
			Successor: "pcl_printer_errors_total",
			Note:      "error_type becomes code, taken from the registered error taxonomy, not free text.",
		},
		{
			Name: "api_requests_total", Kind: KindCounter,
			Labels:    []string{"method", "endpoint", "status_code"},
			Successor: "pcl_http_request_duration_seconds",
			Note:      "The histogram's _count answers it. endpoint becomes route, which is the template.",
		},
		{
			Name: "api_request_duration_seconds", Kind: KindHistogram,
			Labels:    []string{"method", "endpoint", "status_code"},
			Successor: "pcl_http_request_duration_seconds",
			Note:      "Same shape, renamed labels, and native buckets alongside the classic ones.",
		},
		{
			Name: "api_request_errors_total", Kind: KindCounter,
			Labels:    []string{"method", "endpoint", "status_code"},
			Successor: "pcl_http_request_duration_seconds",
			Note:      "A filter on the code label, instead of a second series counting the same requests.",
		},
		{
			Name: "queue_commands_total", Kind: KindCounter,
			Labels:    []string{"printer_id", "command_type"},
			Successor: "pcl_printer_command_duration_seconds",
			Note:      "The histogram's _count answers it, by driver and command instead of by printer.",
		},
		{
			Name: "queue_commands_pending", Kind: KindGauge,
			Labels:    []string{"printer_id"},
			Successor: "pcl_printer_commands_pending",
			Note: "Aggregated by driver. A gauge per printer would spend the fan-out allowance. " +
				"This gauge says whether anything is backed up, and the printer's own entry in " +
				"the listing says which printer is.",
		},
		{
			Name: "queue_commands_completed_total", Kind: KindCounter,
			Labels:    []string{"printer_id", "command_type"},
			Successor: "pcl_printer_command_duration_seconds",
			Note:      "The outcome label carries completion, so completed and failed come from one series.",
		},
		{
			Name: "queue_commands_failed_total", Kind: KindCounter,
			Labels:    []string{"printer_id", "command_type", "reason"},
			Successor: "pcl_printer_command_duration_seconds",
			Note:      "The outcome label carries failure. The reason moves to pcl_printer_errors_total.",
		},
		{
			Name: "queue_command_duration_seconds", Kind: KindHistogram,
			Labels:    []string{"printer_id", "command_type"},
			Successor: "pcl_printer_command_duration_seconds",
			Note:      "printer_id is dropped. A bucketed metric per printer is what the catalog refuses.",
		},
		{
			Name: "commands_total", Kind: KindCounter,
			Labels:    []string{"printer_id", "command"},
			Successor: "pcl_printer_command_duration_seconds",
			Note:      "The histogram's _count answers it.",
		},
		{
			Name: "command_duration_seconds", Kind: KindHistogram,
			Labels:    []string{"printer_id", "command", "success"},
			Successor: "pcl_printer_command_duration_seconds",
			Note:      "printer_id is dropped and success becomes the outcome label.",
		},
		{
			Name: "command_errors_total", Kind: KindCounter,
			Labels:    []string{"printer_id", "command"},
			Successor: "pcl_printer_errors_total",
			Note:      "Kept per printer. A counter costs one series per printer, a histogram many more.",
		},
	}
}
