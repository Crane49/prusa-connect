package connectorapi

import "github.com/prusa3d/prusa-connect/sdk/event"

// What a driver reports about the print on its machine, and the rules a host
// reads it by.
//
// A print reaches a host as ordinary state patches. There is no method for it
// and no second channel: a driver that reports a job writes these paths, and a
// driver whose machines have no notion of a job writes none of them and costs
// nothing.

// The paths a print is addressed at in the state tree.
const (
	// JobIDPath is the machine's own id for the print: a number, or text such
	// as a hash, in whatever form the machine gives it. A host keeps it as text
	// and matches readings to a print with it. It does not name the print: the
	// host gives each print a UUID, so an id the machine reuses after a reset
	// is a print of its own. A driver that reports any other path here reports
	// this one too. See JobIDRule.
	JobIDPath = "/job/id"
	// JobPathPath is the file the machine is printing, on the machine's own
	// storage. JobDisplayPath is the name the machine shows for it, which a
	// machine reports only when it shortened the file name.
	JobPathPath    = "/job/path"
	JobDisplayPath = "/job/display"
	// JobStatePath is the machine's word for what the print is doing, for a
	// machine that reports one apart from its own state. A host reads /state
	// for a machine that does not.
	JobStatePath = "/job/state"
	// JobProgressPath is how far the print has got, in whole percent.
	JobProgressPath = "/job/progress"
	// JobTimePrintingPath is how long the machine says it has been printing, in
	// seconds, and JobTimeRemainingPath is how long it says is left.
	JobTimePrintingPath  = "/job/time_printing"
	JobTimeRemainingPath = "/job/time_remaining"
	// JobOutcomePath is how the print ended, in the closed vocabulary below. It
	// is optional, and a print that ends with nothing under it is published as
	// event.JobUnknown.
	JobOutcomePath = "/job/outcome"
	// JobFilamentUsedPath is how much filament the machine says the print
	// consumed, in millimetres. It is optional. An absent value is unknown and
	// is never read as zero.
	JobFilamentUsedPath = "/job/filament_used_mm"
	// JobObjectsPath is the objects of the print that can be cancelled one by
	// one, for a machine that reports them. It is optional. The value is a
	// JSON array with one entry per object, in the shape JobObject gives,
	// ordered by id. An empty array is a print with no such objects, and an
	// absent path is a machine that has said nothing about them.
	//
	// A driver reports the whole array again each time one object changes,
	// and reports null when the machine starts a different print.
	JobObjectsPath = "/job/objects"
)

// JobObject is one entry of the array at JobObjectsPath.
//
// ID is the machine's own number for the object, the one its cancel verb
// takes. Name is the label the machine gives the object, and is empty for a
// machine that reports none. Cancelled is whether the machine skips the object
// for the rest of the print.
type JobObject struct {
	ID        int    `json:"id"`
	Name      string `json:"name,omitempty"`
	Cancelled bool   `json:"cancelled"`
}

// JobIDRule is the rule a host holds a driver to, stated once so that a driver
// author and the conformance suite read the same sentence.
//
// A machine that counts its own jobs gives the driver a number to report, and
// a machine that names its jobs gives it a text to report as it is. A machine
// that does neither still has a print, and the driver derives an id for it:
// something stable for the life of that print and different for the next one,
// such as the file's name with the moment the print started. A derived id must
// survive the driver reconnecting to the machine mid-print, because a second id
// for one print is a second print in every record that reads it.
const JobIDRule = "a driver that reports a job reports " + JobIDPath + ", derived where the machine " +
	"counts no jobs, and stable for the life of that print"

// JobOutcome is the driver's reading of how its machine ended a print, in the
// closed set sdk/event publishes.
//
// The mapping belongs here because only the driver knows its machine's words. A
// host knows none of them: it reads this path, and a print that ends with
// nothing under it is unknown rather than guessed.
type JobOutcome = string

// The values JobOutcomePath carries. They are sdk/event's, named again here so
// a driver writing the path and a consumer reading the event cannot drift.
const (
	// JobFinished is a machine that reached the end of the file.
	JobFinished JobOutcome = event.JobFinished
	// JobFailed is a print the machine stopped on a fault.
	JobFailed JobOutcome = event.JobFailed
	// JobCancelled is a print somebody stopped, at the machine or through the
	// host.
	JobCancelled JobOutcome = event.JobCancelled
	// JobUnknown is a print that stopped and said nothing about how. A driver
	// need not report it: a print that ends with no outcome reads as this one.
	JobUnknown JobOutcome = event.JobUnknown
)
