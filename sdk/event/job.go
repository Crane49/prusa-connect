package event

import "time"

// The payload types of the two print transitions.
//
// A job is one print on one machine. It contributes two durable frames to that
// machine's origin: started when the host first sees the print, and settled
// when it sees it stop. Each is published once per print, and the pair is what
// a reader folds into one row.
//
// Neither names a person. Who authorized the print is in CommandAuthorized,
// which takes view_audit, and a reader that may see it joins the two by
// command_id. A print somebody started at the machine's own screen has no
// command, so JobStarted.CommandID is empty for it.
//
// Progress while the print runs is telemetry at /job/progress and is not here.

// The declared payload bounds, in bytes. They are the transfer frames' bounds
// for the same reason: the replay window holds 32 events and 32 KiB per origin,
// and a print must not evict a printer's presence frames.
const (
	// JobStartedMaxBytes bounds the started payload, which carries a path and
	// a display name the machine chose.
	JobStartedMaxBytes = 4096
	// JobSettledMaxBytes bounds the settled payload.
	JobSettledMaxBytes = 1024
)

// How a print ended. The set is closed: a consumer that reads a fifth value is
// reading a frame from a later build.
//
// The value comes from the driver, which maps its machine's own word onto one
// of these and reports it at /job/outcome. The core adds nothing to that map:
// it knows no machine's vocabulary, so a print that ended with no outcome
// reported is JobUnknown.
const (
	// JobFinished is a machine that reached the end of the file.
	JobFinished = "finished"
	// JobFailed is a print the machine stopped on a fault.
	JobFailed = "failed"
	// JobCancelled is a print somebody stopped, at the machine or through this
	// host. A machine that reports one word for both is not asked to separate
	// them.
	JobCancelled = "cancelled"
	// JobUnknown is a print that stopped and said nothing about how. A power
	// cut reads like this, and so does a machine that goes back to idle without
	// a word for the print that was on the bed.
	JobUnknown = "unknown"
)

// JobStarted is the data member of com.prusa.connect.device.job.started.
type JobStarted struct {
	// JobID is the print this frame opens, a version 7 UUID the host gives
	// each print. It is the id the job row carries and the id JobSettled
	// repeats.
	JobID string `json:"job_id"`
	// DeviceJobID is the job number the machine issued, as the machine spells
	// it. It is not unique: a machine reuses its numbers after a reset.
	DeviceJobID string `json:"device_job_id"`
	// Path is the file on the machine's own storage, and DisplayName is what
	// the machine shows for it. Each is empty for a machine that reported none.
	Path        string `json:"path,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	// CommandID is the instruction this host sent to start the print, and is
	// empty for a print that started at the machine's own screen. It joins this
	// frame to the command frames of that instruction, so a reader that may see
	// audit frames reads who authorized the print from
	// com.prusa.connect.device.command.authorized.
	CommandID string `json:"command_id,omitempty"`
	// Started is when this host first saw the print.
	Started time.Time `json:"started"`
}

// JobSettled is the data member of com.prusa.connect.device.job.settled.
type JobSettled struct {
	// JobID joins this frame to the started one.
	JobID string `json:"job_id"`
	// DeviceJobID is the job number the machine issued for the print.
	DeviceJobID string `json:"device_job_id"`
	// Path and DisplayName repeat the file, so this frame says what ended
	// without the started one.
	Path        string `json:"path,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	// Started is when this host first saw the print and Ended is when it saw it
	// stop.
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended"`
	// Outcome is one of JobFinished, JobFailed, JobCancelled and JobUnknown.
	Outcome string `json:"outcome"`
	// MachineState is the machine's own last word for the print, in the
	// vocabulary its driver declares. It is what Outcome was read from, and it
	// is here because the closed set above loses the difference between two
	// machines that both say cancelled.
	MachineState string `json:"machine_state,omitempty"`
	// Progress is the last percentage the machine reported.
	Progress float64 `json:"progress"`
	// TimePrintingS is the machine's own count of how long it printed, in
	// seconds. It is absent for a machine that reports none, which is what
	// TimePrintingKnown separates from a machine that reported zero.
	TimePrintingS     int64 `json:"time_printing_s,omitempty"`
	TimePrintingKnown bool  `json:"time_printing_known,omitempty"`
	// FilamentUsedMM is how much filament the machine says the print consumed,
	// in millimetres. It is absent for a machine that reports none, and absent
	// means unknown: FilamentUsedKnown is what separates that from a print that
	// used nothing.
	FilamentUsedMM    float64 `json:"filament_used_mm,omitempty"`
	FilamentUsedKnown bool    `json:"filament_used_known,omitempty"`
}
