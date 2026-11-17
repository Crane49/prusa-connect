package app

// The objects of a print that can be cancelled one by one.
//
// A driver reports them at connectorapi.JobObjectsPath as one array, and this
// file is the only reader of that path. It bounds what it reads to the numbers
// sdk/deviceapi publishes, so the state tree a driver filled, the row the
// record store keeps and the answer a module reads all carry the same set.
//
// The set is kept on the print's row as one JSON document, written with the
// rest of the reading and frozen with it when the print ends. That is what
// lets a module ask, after the print, how many objects it had and which of
// them were cancelled.

import (
	"cmp"
	"encoding/json"
	"slices"

	"github.com/prusa3d/prusa-connect/internal/kernel/machinetext"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// jobObjectsPath is where a driver reports the objects.
const jobObjectsPath = connectorapi.JobObjectsPath

// jobObjects is the document printer_job.objects holds, and the bounded
// reading of one state tree. Count is how many distinct objects the machine
// reported, which can be more than Objects holds.
type jobObjects struct {
	Count   int                 `json:"count"`
	Objects []httpapi.JobObject `json:"objects"`
}

// jobObjectsOf reads the objects out of one state tree, bounded, and encodes
// them as the document the row keeps. It is empty for a tree that says nothing
// about them, and for a value that is not an array of objects.
//
// The result is text so that jobFacts stays comparable: the flush compares the
// whole struct to decide whether the print moved.
func jobObjectsOf(state map[string]json.RawMessage) string {
	raw := state[jobObjectsPath]
	if len(raw) == 0 || string(raw) == nullNode {
		return ""
	}

	var reported []connectorapi.JobObject
	if err := json.Unmarshal(raw, &reported); err != nil || reported == nil {
		return ""
	}

	encoded, err := json.Marshal(boundJobObjects(reported))
	if err != nil {
		return ""
	}

	return string(encoded)
}

// boundJobObjects holds one reported set to deviceapi.MaxJobObjects objects and
// each name to deviceapi.MaxJobObjectNameBytes.
//
// Two entries for one id keep the later one, and the result is ordered by id.
// A set over the bound keeps every cancelled object before the others, because
// a reader asks which objects were cancelled.
func boundJobObjects(reported []connectorapi.JobObject) jobObjects {
	byID := make(map[int]connectorapi.JobObject, len(reported))
	for _, object := range reported {
		byID[object.ID] = object
	}

	all := make([]connectorapi.JobObject, 0, len(byID))
	for _, object := range byID {
		all = append(all, object)
	}

	slices.SortFunc(all, func(a, b connectorapi.JobObject) int { return cmp.Compare(a.ID, b.ID) })

	kept := all
	if len(all) > deviceapi.MaxJobObjects {
		// A stable sort on the flag alone keeps the id order inside each half.
		kept = slices.Clone(all)
		slices.SortStableFunc(kept, func(a, b connectorapi.JobObject) int {
			switch {
			case a.Cancelled == b.Cancelled:
				return 0
			case a.Cancelled:
				return -1
			default:
				return 1
			}
		})

		kept = kept[:deviceapi.MaxJobObjects]
		slices.SortFunc(kept, func(a, b connectorapi.JobObject) int { return cmp.Compare(a.ID, b.ID) })
	}

	out := jobObjects{Count: len(all), Objects: make([]httpapi.JobObject, 0, len(kept))}

	for _, object := range kept {
		out.Objects = append(out.Objects, httpapi.JobObject{
			ID:        object.ID,
			Name:      clipObjectName(object.Name),
			Cancelled: object.Cancelled,
		})
	}

	return out
}

// clipObjectName cuts a name over the bound on a character boundary, marker
// included, so the result is never longer than the bound.
func clipObjectName(name string) string {
	if len(name) <= deviceapi.MaxJobObjectNameBytes {
		return name
	}

	return machinetext.Clip(name, deviceapi.MaxJobObjectNameBytes-len(machinetext.Marker))
}

// renderJobObjects reads the document back for the wire. A document this build
// cannot read renders as no objects, the answer for a machine that said
// nothing, because a row must not be lost over one column.
func renderJobObjects(document string) (*int, []httpapi.JobObject) {
	if document == "" {
		return nil, nil
	}

	var held jobObjects
	if err := json.Unmarshal([]byte(document), &held); err != nil {
		return nil, nil
	}

	count := held.Count

	return &count, held.Objects
}
