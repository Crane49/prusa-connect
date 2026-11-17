// Package deviceissue is the closed set of reasons a device needs somebody's
// attention, and the descriptor a published reference and the API both read.
//
// It is a vocabulary: enumerable from code, held by a gate before shipping,
// generated into a page and served under /api/v1/vocabularies. A client
// branches on the reason and reads the sentence from the vocabulary, so a
// device record carries only the token and its severity.
//
// It says nothing about how an issue is found. The host's own reading of a
// device raises one, beside the device plane. This package holds the names and
// the prose, so one set answers the page, the endpoint and the code that raises
// them.
package deviceissue

import "github.com/prusa3d/prusa-connect/sdk/vocabulary"

// What a published device-issue reference declares about itself. The content
// version moves when a reason is added, retired or redefined. The format
// version moves when the shape of an entry changes, so a consumer can tell a
// reshaped file from a reworded one.
const (
	VocabularyName    = "device-issues"
	VocabularyVersion = "1.0.0"
	VocabularyFormat  = "1.0"
)

// Reason names one thing that is wrong with a device, in the closed set the
// core owns. A client branches on it. A translator translates the sentence the
// vocabulary carries beside it, and nothing on the device record.
type Reason string

// The reasons this build raises. They are declared in severity order, and
// Reasons() is the order an issue list on a device record is built in, so the
// entry a person should read first is first.
const (
	// ReasonAwaitingOperator is a device that has raised a dialog. It is
	// waiting for a person and nothing else will move it.
	ReasonAwaitingOperator Reason = "awaiting_operator"
	// ReasonDeparted is a device whose session has ended.
	ReasonDeparted Reason = "departed"
	// ReasonDisconnected is a held session with no connection under it.
	ReasonDisconnected Reason = "disconnected"
	// ReasonFramesRefused is a device sending frames its driver cannot read.
	ReasonFramesRefused Reason = "frames_refused"
	// ReasonTelemetryDropped is this server falling behind a device's telemetry.
	ReasonTelemetryDropped Reason = "telemetry_dropped"
	// ReasonNoCapabilityProfile is a device no capability profile covered.
	ReasonNoCapabilityProfile Reason = "no_capability_profile"
)

// Severity is how loudly a client should report an issue. It belongs to the
// reason, so it is the same on every record carrying that reason.
type Severity string

// The severities, in ascending order. A client that shows one badge for a
// device shows the highest severity its issues carry.
const (
	// SeverityInfo is worth showing and needs no action.
	SeverityInfo Severity = "info"
	// SeverityWarning is something a person should look at.
	SeverityWarning Severity = "warning"
	// SeverityError is a device that is not doing its job until somebody acts.
	SeverityError Severity = "error"
)

// The reserved paths in the state tree that raise ReasonAwaitingOperator.
//
// A dialog is the one part of the state tree the host reads instead of passing
// it through. It holds a slot the session queue cannot evict, because nothing
// repeats a dropped dialog. A driver that raises a dialog writes it at these
// paths, and the host can then say a device is waiting for a person without
// knowing the dialect.
//
// Every value belongs to the driver and is passed through as the device
// encoded it. Nothing here looks a code up in a catalogue.
const (
	// DialogSubtree is the prefix every dialog path shares. A patch under it
	// can change whether a device waits for a person.
	DialogSubtree = "/dialog/"
	// DialogIDPath is what the machine is waiting on. Its presence is the
	// signal, and a driver removes it when the dialog closes.
	DialogIDPath = "/dialog/id"
	// DialogTitlePath is the dialog's heading, as the machine displayed it.
	DialogTitlePath = "/dialog/title"
	// DialogTextPath is the dialog's body, as the machine displayed it.
	DialogTextPath = "/dialog/text"
	// DialogCodePath is the firmware's own code for the dialog.
	DialogCodePath = "/dialog/code"
	// DialogButtonsPath is the labels of the dialog's buttons, as a list,
	// in the order the machine displayed them.
	DialogButtonsPath = "/dialog/buttons"
)

// Doc is one reason with the prose a published reference carries.
type Doc struct {
	Reason      Reason   `json:"reason"`
	Severity    Severity `json:"severity"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	// Action is what a person does about it. A reason a reader cannot act on
	// is a reason that should not be raised.
	Action  string `json:"action"`
	Version string `json:"version"`
	// Deprecated marks a reason that is going away, and is nil for one that is
	// staying. It is removed only after a release published this marker for it,
	// and the compatibility gate refuses any other removal.
	Deprecated *vocabulary.Deprecation `json:"deprecated,omitempty"`
}

// docs is the descriptor table. It is a static literal, so it costs no heap and
// no work at start-up.
//
//nolint:gochecknoglobals // the descriptor table for this vocabulary, read-only after init.
var docs = []Doc{
	{
		Reason:   ReasonAwaitingOperator,
		Severity: SeverityError,
		Summary:  "The device raised a dialog and is waiting for a person.",
		Description: "The device put a question on its own screen and stopped. Nothing this server " +
			"sends moves it on except an answer to that dialog, so a job does not continue until " +
			"somebody answers it. The host reads it from the reserved dialog subtree of the state " +
			"tree: " + DialogIDPath + " raises this issue, and " + DialogTitlePath + ", " +
			DialogTextPath + " and " + DialogCodePath + " are what the machine displayed, in the " +
			"machine's own words.",
		Action: "Read the dialog title and text on the device record, then answer it at the machine " +
			"or with the dialog action verb.",
		Version: VocabularyVersion,
	},
	{
		Reason:   ReasonDeparted,
		Severity: SeverityWarning,
		Summary:  "The device's session has ended, so nothing can be sent to it.",
		Description: "The connector released the device. What the record carries is the last reading " +
			"this server took, and every operation that has to reach the machine is refused until it " +
			"dials in again. A printer switched off overnight reads this way, so it is not a fault on " +
			"its own. It is the only issue a departed device carries: the rest of the record " +
			"describes a machine nothing is talking to, and reporting a stale dialog as live work " +
			"would send somebody to a printer that is off.",
		Action: "Read the departed member for why the session ended and when. Check that the machine " +
			"is powered and on the network.",
		Version: VocabularyVersion,
	},
	{
		Reason:   ReasonDisconnected,
		Severity: SeverityWarning,
		Summary:  "The session is held and no connection to the device is up.",
		Description: "The connector still holds the session, so a command queues instead of failing, " +
			"and the machine is not answering now. It is what a device that lost power or fell off " +
			"the network looks like before its session ends.",
		Action:  "Check power and the network. A command sent now waits until the device answers again.",
		Version: VocabularyVersion,
	},
	{
		Reason:   ReasonFramesRefused,
		Severity: SeverityWarning,
		Summary:  "The device is sending frames its driver has no reading of.",
		Description: "The driver dropped frames it could not read, over the window the link member " +
			"reports. Anything above zero on a connected device is firmware speaking something this " +
			"build does not know, and it stays connected and silent while it does. A driver that " +
			"counts nothing never raises this: no measurement is not a clean link.",
		Action: "Read the refused frame rate under link, then report the model and the firmware " +
			"version so the dialect can be covered.",
		Version: VocabularyVersion,
	},
	{
		Reason:   ReasonTelemetryDropped,
		Severity: SeverityWarning,
		Summary:  "This server is not reading the device's telemetry fast enough.",
		Description: "The session discarded state patches because this server was behind, so the " +
			"state tree is older than the machine. It is load on this server rather than a fault at " +
			"the device. A dialog is never discarded: it holds a slot the queue cannot evict, " +
			"because a dropped dialog is repeated by nothing.",
		Action: "Read the dropped telemetry rate under link. Reduce what this server is doing, or " +
			"give it more of the machine it runs on.",
		Version: VocabularyVersion,
	},
	{
		Reason:   ReasonNoCapabilityProfile,
		Severity: SeverityInfo,
		Summary:  "No capability profile covered this model and firmware.",
		Description: "The capability set was resolved from the driver's baseline alone, so the device " +
			"works and offers fewer verbs than a covered device of the same model does. A model this " +
			"build has never seen and a firmware newer than the profiles it ships both read this way.",
		Action: "Compare the capabilities member against a covered device of the same model, and " +
			"report the model and firmware so a profile can be added.",
		Version: VocabularyVersion,
	},
}

// severities is the closed severity set, ascending. It is published beside the
// reasons, so a client ordering badges reads the order from the vocabulary
// instead of hard-coding it.
//
//nolint:gochecknoglobals // the severity table for this vocabulary, read-only after init.
var severities = []Severity{SeverityInfo, SeverityWarning, SeverityError}

// Docs returns the descriptor for every reason, in declaration order. The
// published page, the API endpoint and the device plane all read it, so none of
// them can answer differently.
func Docs() []Doc {
	out := make([]Doc, len(docs))
	copy(out, docs)

	return out
}

// Reasons returns the closed set in declaration order, which is severity order.
// It is the order an issue list on a device record is built in.
func Reasons() []Reason {
	out := make([]Reason, 0, len(docs))
	for _, doc := range docs {
		out = append(out, doc.Reason)
	}

	return out
}

// Severities returns the closed severity set, ascending.
func Severities() []Severity {
	out := make([]Severity, len(severities))
	copy(out, severities)

	return out
}

// Lookup returns the descriptor for r, reporting false for a reason outside the
// closed set.
func Lookup(r Reason) (Doc, bool) {
	for _, doc := range docs {
		if doc.Reason == r {
			return doc, true
		}
	}

	return Doc{}, false
}

// SeverityOf returns the severity declared for r, and the empty severity for a
// reason outside the closed set. A projection reads it, so the severity on the
// wire comes from this table and not from a second list.
func SeverityOf(r Reason) Severity {
	doc, ok := Lookup(r)
	if !ok {
		return ""
	}

	return doc.Severity
}

// Valid reports whether r is one of the declared reasons.
func (r Reason) Valid() bool {
	_, ok := Lookup(r)

	return ok
}

// String returns the reason as it is rendered in the API.
func (r Reason) String() string { return string(r) }

// Valid reports whether s is one of the declared severities.
func (s Severity) Valid() bool {
	switch s {
	case SeverityInfo, SeverityWarning, SeverityError:
		return true
	}

	return false
}

// String returns the severity as it is rendered in the API.
func (s Severity) String() string { return string(s) }
