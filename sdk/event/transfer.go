package event

// The payload types of the three transfer events.
//
// A transfer is one file moving from a storage onto a device, through the
// core. Started and settled are durable. Progress is telemetry: it is
// conflated and never stored.

// The payload bounds, in bytes.
const (
	// TransferStartedMaxBytes bounds the started payload, which includes three
	// caller-supplied names.
	TransferStartedMaxBytes = 4096
	// TransferSettledMaxBytes bounds the settled payload.
	TransferSettledMaxBytes = 1024
)

// How a transfer ended. The set is closed.
const (
	// TransferDone is a device that reported the file arrived.
	TransferDone = "done"
	// TransferFailed is a transfer the host, the storage or the device could
	// not carry out.
	TransferFailed = "failed"
	// TransferCancelled is a transfer somebody stopped, including one whose
	// caller went away.
	TransferCancelled = "cancelled"
	// TransferInterrupted is a transfer that was running when the server
	// stopped. It is written on the next start and the transfer is not
	// resumed.
	TransferInterrupted = "interrupted"
)

// The states of a running transfer. The live record reports them. They are
// never the value of TransferSettled.State.
const (
	// TransferQueued is a transfer this host has accepted and not yet offered
	// to the device.
	TransferQueued = "queued"
	// TransferSending is a transfer the device has accepted and is fetching.
	TransferSending = "sending"
)

// TransferStarted is the data member of com.prusa.connect.device.transfer.started.
// It names no principal. The audit record is the command frame of the request
// that began the transfer.
type TransferStarted struct {
	// TransferID is the host-minted id of this transfer, a ULID. It is not the
	// number the device gives the transfer on its own wire.
	TransferID string `json:"transfer_id"`
	// Storage is the name of the declared storage the bytes come from.
	Storage string `json:"storage"`
	// Source is the path inside that storage.
	Source string `json:"source"`
	// Destination is where the file lands on the device, storage-prefixed.
	Destination string `json:"destination"`
	// BytesTotal is the file's length, as the host measured it before the first
	// byte left.
	BytesTotal int64 `json:"bytes_total"`
}

// TransferProgressed is the data member of
// com.prusa.connect.device.transfer.progressed. It is telemetry and is never
// stored.
type TransferProgressed struct {
	// TransferID joins this frame to the started one.
	TransferID string `json:"transfer_id"`
	// BytesTotal repeats the length, so a subscriber that joined late can show
	// a proportion from one frame.
	BytesTotal int64 `json:"bytes_total"`
	// BytesSent is how many of the file's bytes this host has put on the wire.
	// A byte the device asks for again counts once.
	BytesSent int64 `json:"bytes_sent"`
	// BytesReceived is how many the device says it has taken. It is valid
	// only when BytesReceivedKnown is true. Some devices report no such
	// number.
	BytesReceived      int64 `json:"bytes_received,omitempty"`
	BytesReceivedKnown bool  `json:"bytes_received_known,omitempty"`
}

// TransferSettled is the data member of com.prusa.connect.device.transfer.settled.
type TransferSettled struct {
	// TransferID joins this frame to the started one, and is repeated in the
	// envelope's causation id.
	TransferID string `json:"transfer_id"`
	// State is one of TransferDone, TransferFailed, TransferCancelled or
	// TransferInterrupted.
	State string `json:"state"`
	// BytesSent is how many of the file's bytes this host had put on the wire
	// when the transfer ended.
	BytesSent int64 `json:"bytes_sent"`
	// BytesTotal repeats the length.
	BytesTotal int64 `json:"bytes_total"`
	// Reason is why it ended that way, in the words of whoever ended it. It is
	// empty on a transfer that finished.
	Reason string `json:"reason,omitempty"`
	// FaultCode is the host's own code for the failure, empty when there is
	// none.
	FaultCode string `json:"fault_code,omitempty"`
	// ElapsedMS is how long the transfer ran, in milliseconds.
	ElapsedMS int64 `json:"elapsed_ms,omitempty"`
}
