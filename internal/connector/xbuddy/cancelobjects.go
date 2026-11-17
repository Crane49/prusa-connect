package xbuddy

// The objects of a print that can be cancelled one by one.
//
// The firmware names them in a CANCELABLE_CHANGED event, and nowhere else:
//
//	{"event": "CANCELABLE_CHANGED", "job_id": 42, "state": "PRINTING",
//	 "data": {"objects": [{"canceled": false, "id": 0}, {"canceled": true, "id": 1}]}}
//
// It sends one whenever the set moves: the G-code reaches an object it has not
// labelled before or declares how many there are, an object is cancelled or
// restored, or a new print starts and the set is emptied. It also answers
// CANCEL_OBJECT and UNCANCEL_OBJECT with one, whether or not the object moved
// (Prusa-Firmware-Buddy, src/connect/planner.cpp and the CancelableChanged arm
// of src/connect/render.cpp). The entry carries the number and the flag and no
// name, although the machine shows names on its own screen.
//
// So the driver reports the whole array each time, at connectorapi.JobObjectsPath,
// and a reader never has to merge one event into the last.

import (
	"encoding/json"
	"slices"
	"strconv"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// eventCancelableChanged is the event that carries the objects.
const eventCancelableChanged = "CANCELABLE_CHANGED"

// jobObjectsPath is where this driver writes them.
const jobObjectsPath = connectorapi.JobObjectsPath

// maxCancelableObjects is the firmware's own limit: it keeps one bit per
// object in a set of 1024 (src/feature/cancel_object/cancel_object.hpp), and an
// id outside it is one the machine cannot cancel. An entry outside it is
// dropped, so a frame of any length reports at most this many objects.
const maxCancelableObjects = 1024

// cancelableEntry is one object as the firmware spells it. Both members are
// pointers, so that an entry with no id is told apart from object 0.
type cancelableEntry struct {
	ID       *int64 `json:"id"`
	Canceled *bool  `json:"canceled"`
}

// cancelableObjectsOf reads the objects out of one event's data, sorted by id.
// It reports false for data that names no array of objects, which is a frame
// that has said nothing about them.
//
// An entry with no id, or with an id the machine cannot hold, is dropped. Two
// entries for one id keep the later one, which is the order the firmware wrote
// them in.
func cancelableObjectsOf(data json.RawMessage) ([]connectorapi.JobObject, bool) {
	if len(data) == 0 {
		return nil, false
	}

	var frame struct {
		Objects *[]cancelableEntry `json:"objects"`
	}

	if err := json.Unmarshal(data, &frame); err != nil || frame.Objects == nil {
		return nil, false
	}

	byID := make(map[int64]bool, min(len(*frame.Objects), maxCancelableObjects))

	for _, entry := range *frame.Objects {
		if entry.ID == nil || *entry.ID < 0 || *entry.ID >= maxCancelableObjects {
			continue
		}

		byID[*entry.ID] = entry.Canceled != nil && *entry.Canceled
	}

	out := make([]connectorapi.JobObject, 0, len(byID))
	for id, cancelled := range byID {
		out = append(out, connectorapi.JobObject{ID: int(id), Cancelled: cancelled})
	}

	slices.SortFunc(out, func(a, b connectorapi.JobObject) int { return a.ID - b.ID })

	return out, true
}

// noteCancelableObjects turns a CANCELABLE_CHANGED event into one patch at
// /job/objects.
//
// The job number on the event's envelope is read first, because the set
// belongs to that print: a print that has just started announces its empty set
// with its own number, and reading the number after the set would let the new
// number clear what was just written.
//
// A set reported while no print is named is dropped. A machine that connects
// after a print ended still holds that print's set and announces it, and the
// tree has no print to hang it on.
func (s *Session) noteCancelableObjects(message inboundMessage) {
	if message.event.Event != eventCancelableChanged {
		return
	}

	objects, said := cancelableObjectsOf(message.event.Data)
	if !said {
		return
	}

	// The number is reported as well as noted, as noteJob does: a driver that
	// writes a path of a print writes the print's id too.
	if raw, present := message.fields["job_id"]; present {
		var id int64
		if json.Unmarshal(raw, &id) == nil && id > 0 {
			s.noteJobID(id)
			s.report(connectorapi.StatePatch{
				Path:  telemetryPaths["job_id"],
				Value: json.RawMessage(strconv.FormatInt(id, 10)),
			})
		}
	}

	job := s.job.Load()
	if job <= 0 {
		return
	}

	encoded, err := json.Marshal(objects)
	if err != nil {
		return
	}

	s.objectsJob.Store(job)
	s.report(connectorapi.StatePatch{Path: jobObjectsPath, Value: encoded})
}

// forgetTheObjects drops the array, from the state tree, for a machine that has
// named a print other than the one the array belongs to. A patch of null is how
// this dialect says a node is gone.
//
// The same print named again keeps it. The firmware reports job number 0
// between prints, so a machine can name 42, then 0, then 42 again, and the set
// of print 42 is still the set of print 42.
func (s *Session) forgetTheObjects(job int64) {
	held := s.objectsJob.Load()
	if held == jobIDUnknown || held == job {
		return
	}

	if !s.objectsJob.CompareAndSwap(held, jobIDUnknown) {
		return
	}

	s.report(connectorapi.StatePatch{Path: jobObjectsPath, Value: json.RawMessage(dialogAbsent)})
}
