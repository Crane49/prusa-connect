// Package scriptapi is the environment a script tier adapter is written
// against: the one event a script is given, and the message it returns.
//
// A script is an expression an operator writes in the configuration file. It
// maps one event onto zero, one or many messages, and the host publishes them.
// A script never names a destination address. It names an endpoint the
// operator declared, a routing key and a body.
//
// Every value here is a copy with no pointer, no method and no collection, so
// a script cannot reach a live host object. The expr struct tags are the field
// names a script author writes.
package scriptapi

// Event is one domain fact, flattened for a script to read. A field that does
// not apply to the fact is the zero value.
type Event struct {
	// Kind is what the fact is, such as printer.state_changed. A durable
	// event from the host's bus has its event type here, such as
	// com.prusa.connect.device.job.settled, and none of its payload.
	Kind string `expr:"kind"`
	// DeviceID is the device the fact is about, empty for a fact that is
	// about something else.
	DeviceID string `expr:"device_id"`
	// State is the device's connection or print state, for a state change.
	State string `expr:"state"`
	// Mode is the label mode, for a label change.
	Mode string `expr:"mode"`
	// LabelID is the label, for a label deletion.
	LabelID string `expr:"label_id"`
	// JobID is the job's own identity, for a job update. The host sends no
	// job update, so the three job fields are empty.
	JobID string `expr:"job_id"`
	// JobState is the job's state, for a job update.
	JobState string `expr:"job_state"`
	// JobSource is where the job came from, for a job update.
	JobSource string `expr:"job_source"`
	// Time is when the host read the fact, as RFC 3339 with a UTC offset.
	Time string `expr:"time"`
}

// Message is one thing the host publishes on the script's behalf. Endpoint is
// a name the operator declared for this instance, never an address.
type Message struct {
	// Endpoint is the operator-declared destination this goes to.
	Endpoint string `expr:"endpoint"`
	// Key is the routing key. A broker routes on it, and an HTTP destination
	// sends it as a header.
	Key string `expr:"key"`
	// Body is the payload, already encoded by the script.
	Body string `expr:"body"`
}
