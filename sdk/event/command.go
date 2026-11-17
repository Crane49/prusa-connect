package event

// The payload types of the three command events. A command adds three durable
// frames to its device's origin, so each payload is kept small.

// The payload bounds, in bytes. A payload over its bound is published with the
// arguments replaced by their digest.
const (
	// CommandRequestedMaxBytes bounds the requested payload, arguments
	// included.
	CommandRequestedMaxBytes = 4096
	// CommandSettledMaxBytes bounds the settled payload.
	CommandSettledMaxBytes = 1024
	// CommandAuthorizedMaxBytes bounds the audit payload.
	CommandAuthorizedMaxBytes = 1024
)

// How a command ended, as far as the host observed it. The set is closed.
const (
	// CommandSucceeded is a printer that accepted the command.
	CommandSucceeded = "succeeded"
	// CommandFailed is a command the host or the driver could not carry out.
	CommandFailed = "failed"
	// CommandRefused is a printer that answered no. The exchange completed.
	CommandRefused = "refused"
	// CommandTimedOut is a deadline that expired while the command was
	// outstanding. The printer may still execute it, so the outcome is
	// unknown.
	CommandTimedOut = "timed_out"
	// CommandAbandoned is a caller that went away while the command was
	// outstanding. The printer may still execute it, so the outcome is
	// unknown.
	CommandAbandoned = "abandoned"
	// CommandUnauthorized is a command the host refused before it reached any
	// machine, because the caller may not send the verb. It still gets an id
	// and a record.
	CommandUnauthorized = "unauthorized"
	// CommandServerRestarted is a command that was requested and not settled
	// when the host restarted. The printer may have executed it, so the
	// outcome is unknown. The host does not send it again.
	CommandServerRestarted = "server_restarted"
)

// CommandRequested is the data member of com.prusa.connect.device.command.requested.
// It does not name who authorized the command. That is in CommandAuthorized.
type CommandRequested struct {
	// CommandID is the host-minted id of this invocation, a ULID. It is not the
	// caller's idempotency key.
	CommandID string `json:"command_id"`
	// Verb is the capability key that was invoked.
	Verb string `json:"verb"`
	// Key is the caller's idempotency key, empty when none was sent.
	Key string `json:"key,omitempty"`
	// CapabilityEpoch and CapabilityDigest identify the capability set the
	// command was validated against.
	CapabilityEpoch  uint64 `json:"capability_epoch,omitempty"`
	CapabilityDigest string `json:"capability_digest,omitempty"`
	// Args are the arguments as the caller sent them. They are absent whenever
	// ArgsDigest is set.
	Args map[string]string `json:"args,omitempty"`
	// ArgsDigest is the SHA-256 of the canonical rendering of the arguments,
	// hex encoded. It is set instead of Args for a verb marked dangerous, and
	// for arguments that would break CommandRequestedMaxBytes.
	ArgsDigest string `json:"args_digest,omitempty"`
	// ArgsOmitted says the arguments were replaced by their digest. Without
	// it, the command had no arguments.
	ArgsOmitted bool `json:"args_omitted,omitempty"`
}

// CommandSettled is the data member of com.prusa.connect.device.command.settled.
// It repeats no arguments and names no principal.
type CommandSettled struct {
	// CommandID joins this frame to the requested one, and is repeated in the
	// envelope's causation id.
	CommandID string `json:"command_id"`
	// Verb is the capability key that was invoked.
	Verb string `json:"verb"`
	// Status is one of the Command status constants.
	Status string `json:"status"`
	// OutcomeUnknown says the host never observed how the command ended: the
	// caller went away, a deadline fired, the device lost its connection
	// after the frame was written, or the host restarted.
	OutcomeUnknown bool `json:"outcome_unknown,omitempty"`
	// OK is the printer's answer, and is meaningless when OutcomeUnknown is
	// set.
	OK bool `json:"ok,omitempty"`
	// MachineReason is the printer's own word for a refusal, truncated to the
	// host's bound.
	MachineReason string `json:"machine_reason,omitempty"`
	// Ignored and Substituted are what the printer did differently.
	Ignored     []string          `json:"ignored,omitempty"`
	Substituted map[string]string `json:"substituted,omitempty"`
	// FaultCode is the registered code of the refusal, empty when the command
	// did not fail with one.
	FaultCode string `json:"fault_code,omitempty"`
	// ElapsedMS is how long the invocation took, in milliseconds.
	ElapsedMS int64 `json:"elapsed_ms"`
}

// CommandAuthorized is the data member of com.prusa.connect.device.command.authorized.
// It repeats the actor of the envelope's audit attributes, because a history
// provider stores the data member and not the envelope.
type CommandAuthorized struct {
	// CommandID joins this frame to the other two, and is repeated in the
	// envelope's causation id.
	CommandID string `json:"command_id"`
	// Verb is the capability key that was authorized.
	Verb string `json:"verb"`
	// AuthType and AuthID name the principal, split as the envelope's audit
	// attributes split it.
	AuthType string `json:"auth_type"`
	AuthID   string `json:"auth_id"`
}
