package deviceapi

import "fmt"

// JobObject is one object of a print that the machine can skip while it
// prints the rest.
type JobObject struct {
	// ID is the machine's own number for the object. Pass it to
	// core.command.cancel_object and core.command.uncancel_object.
	ID int `json:"id"`
	// Name is the machine's label for the object, at most
	// MaxJobObjectNameBytes. Empty for a machine that reports no names.
	Name string `json:"name,omitempty"`
	// Cancelled is whether the machine skips the object for the rest of the
	// print.
	Cancelled bool `json:"cancelled"`
}

// validObjects checks one job's objects against the limits of this contract.
func (j *Job) validObjects() error {
	if j == nil {
		return nil
	}

	if len(j.Objects) > MaxJobObjects {
		return fmt.Errorf("job %s carries %d objects and the limit is %d", j.ID, len(j.Objects), MaxJobObjects)
	}

	for _, object := range j.Objects {
		if len(object.Name) > MaxJobObjectNameBytes {
			return fmt.Errorf("job %s names object %d in %d bytes and the limit is %d",
				j.ID, object.ID, len(object.Name), MaxJobObjectNameBytes)
		}
	}

	return nil
}
