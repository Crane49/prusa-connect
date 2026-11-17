package kafka

import "github.com/prusa3d/prusa-connect/sdk/extension"

// Kind is one value of the `kind` member in every record value. The record
// key is the printer id, so a consumer selects records by `kind`. A kind whose
// meaning changes gets a new name.
type Kind struct {
	// Name is the string in the record value's `kind` member.
	Name string
	// Summary is the line the published reference prints for it.
	Summary string
}

// kindVersion is the version of every destination in the reference. The kinds
// share one body shape, so they share one version.
const kindVersion = "1.1.0"

// The kinds this sink publishes. doc() and the Processor methods both read
// this table, so the published reference cannot differ from the wire.
var (
	// PrinterStateChanged is a printer's connection or print state changing.
	PrinterStateChanged = Kind{
		Name:    "printer.state_changed",
		Summary: "A printer's connection or print state changed. Data is the printer id and the state.",
	}

	// PrinterLabelsChanged is a printer's label set changing.
	PrinterLabelsChanged = Kind{
		Name:    "printer.labels_changed",
		Summary: "A printer's label set changed. Data is the printer id, the mode and the labels.",
	}

	// PrinterDeleted is a printer being removed.
	PrinterDeleted = Kind{
		Name:    "printer.deleted",
		Summary: "A printer was removed. Data is the printer id.",
	}

	// JobUpdated is a job being created or changed.
	JobUpdated = Kind{
		Name:    "job.updated",
		Summary: "A job was created or changed. Data is the job, its printer and its state.",
	}

	// LabelDeleted is a label being removed.
	LabelDeleted = Kind{
		Name:    "label.deleted",
		Summary: "A label was removed. Data is the label id.",
	}

	// FactObserved is any durable event on the host's bus, in its envelope.
	// It covers every event type that has no kind of its own.
	FactObserved = Kind{
		Name: "fact.observed",
		Summary: "A durable event from the host's bus. Data is the envelope: origin, type, class, time, " +
			"sequence, epoch, causation id and the payload the emitter owns.",
	}
)

// Kinds is every kind this sink publishes, in the order the reference lists
// them.
var Kinds = []Kind{
	PrinterStateChanged,
	PrinterLabelsChanged,
	PrinterDeleted,
	JobUpdated,
	LabelDeleted,
	FactObserved,
}

// destinations builds the Publishes list from Kinds.
func destinations() []extension.Destination {
	out := make([]extension.Destination, 0, len(Kinds))

	for _, kind := range Kinds {
		out = append(out, extension.Destination{
			Address: kind.Name,
			Version: kindVersion,
			Summary: kind.Summary,
		})
	}

	return out
}
