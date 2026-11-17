package xbuddy

// The objects of a print that can be cancelled one by one, read off the
// CANCELABLE_CHANGED event in the shape the public firmware renders it
// (Prusa-Firmware-Buddy, src/connect/render.cpp): the job number on the
// envelope, and a data object holding an array of {"canceled", "id"}.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// cancelableFrame is one CANCELABLE_CHANGED event as the firmware writes it.
func cancelableFrame(job int, objects string) string {
	return fmt.Sprintf(`{"job_id":%d,"state":"PRINTING","event":"CANCELABLE_CHANGED","data":{"objects":[%s]}}`,
		job, objects)
}

func TestTheObjectsAreReadOffTheFirmwaresFrame(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data string
		want string
		said bool
	}{
		{
			name: "three objects, one cancelled",
			data: `{"objects":[{"canceled":false,"id":0},{"canceled":true,"id":1},{"canceled":false,"id":2}]}`,
			want: `[{"id":0,"cancelled":false},{"id":1,"cancelled":true},{"id":2,"cancelled":false}]`,
			said: true,
		},
		{
			name: "a print that has labelled no object yet",
			data: `{"objects":[]}`,
			want: `[]`,
			said: true,
		},
		{
			name: "out of order and repeated, the later entry wins",
			data: `{"objects":[{"canceled":false,"id":2},{"canceled":false,"id":0},{"canceled":true,"id":2}]}`,
			want: `[{"id":0,"cancelled":false},{"id":2,"cancelled":true}]`,
			said: true,
		},
		{
			name: "an entry with no id or one the machine cannot hold is dropped",
			data: `{"objects":[{"canceled":true},{"canceled":true,"id":-1},{"canceled":true,"id":1024},{"id":3}]}`,
			want: `[{"id":3,"cancelled":false}]`,
			said: true,
		},
		{name: "no data", data: ``, said: false},
		{name: "data with no array", data: `{}`, said: false},
		{name: "an array of something else", data: `{"objects":"many"}`, said: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			objects, said := cancelableObjectsOf(json.RawMessage(tc.data))
			if said != tc.said {
				t.Fatalf("said = %v, want %v", said, tc.said)
			}

			if !said {
				return
			}

			encoded, err := json.Marshal(objects)
			if err != nil {
				t.Fatal(err)
			}

			if string(encoded) != tc.want {
				t.Errorf("read %s, want %s", encoded, tc.want)
			}
		})
	}
}

// TestAFrameOfAnyLengthReportsAtMostTheFirmwaresLimit holds the array to the
// 1024 objects the firmware can hold, whatever a frame claims.
func TestAFrameOfAnyLengthReportsAtMostTheFirmwaresLimit(t *testing.T) {
	t.Parallel()

	entries := make([]string, 0, 3*maxCancelableObjects)
	for id := range 3 * maxCancelableObjects {
		entries = append(entries, fmt.Sprintf(`{"canceled":true,"id":%d}`, id))
	}

	objects, said := cancelableObjectsOf(json.RawMessage(`{"objects":[` + strings.Join(entries, ",") + `]}`))
	if !said {
		t.Fatal("a frame of valid entries was not read")
	}

	if len(objects) != maxCancelableObjects {
		t.Errorf("read %d objects, want %d", len(objects), maxCancelableObjects)
	}

	encoded, err := json.Marshal(objects)
	if err != nil {
		t.Fatal(err)
	}

	// The host keeps one value of up to 32 KiB, and refuses a longer array.
	if len(encoded) > 32<<10 {
		t.Errorf("the widest array the firmware can describe is %d bytes, over the host's 32 KiB", len(encoded))
	}
}

// TestTheObjectsFollowThePrint is the merge: an object cancelled, restored, and
// a new print that starts with an empty set.
func TestTheObjectsFollowThePrint(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, cancelableFrame(41, `{"canceled":false,"id":0},{"canceled":false,"id":1}`))
	printer.awaitDialogTree(t, map[string]string{
		jobObjectsPath:         `[{"id":0,"cancelled":false},{"id":1,"cancelled":false}]`,
		connectorapi.JobIDPath: "41",
	})

	// The answer to CANCEL_OBJECT, which is the same event.
	printer.text(t, cancelableFrame(41, `{"canceled":false,"id":0},{"canceled":true,"id":1}`))
	printer.awaitDialogTree(t, map[string]string{
		jobObjectsPath: `[{"id":0,"cancelled":false},{"id":1,"cancelled":true}]`,
	})

	// The answer to UNCANCEL_OBJECT.
	printer.text(t, cancelableFrame(41, `{"canceled":false,"id":0},{"canceled":false,"id":1}`))
	printer.awaitDialogTree(t, map[string]string{
		jobObjectsPath: `[{"id":0,"cancelled":false},{"id":1,"cancelled":false}]`,
	})

	// The print ends, and the firmware names no job between prints. The set
	// of print 41 stays.
	printer.text(t, `{"state":"FINISHED","job_id":0}`)
	printer.text(t, `{"state":"IDLE","temp_nozzle":215}`)
	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "215"})

	if held := printer.dialogValue(jobObjectsPath); held != `[{"id":0,"cancelled":false},{"id":1,"cancelled":false}]` {
		t.Errorf("%s reads %s after the print ended, want the set of the print that ended", jobObjectsPath, held)
	}

	// A new print, named by telemetry first. The set of the print before goes.
	printer.text(t, `{"state":"PRINTING","job_id":42}`)
	printer.awaitDialogTree(t, map[string]string{jobObjectsPath: dialogAbsent})

	// And its own set arrives.
	printer.text(t, cancelableFrame(42, `{"canceled":true,"id":0}`))
	printer.awaitDialogTree(t, map[string]string{jobObjectsPath: `[{"id":0,"cancelled":true}]`})
}

// TestANewPrintNamedByItsOwnSetKeepsTheSet is the other order: the event of a
// new print names its job number before telemetry does, and the number must not
// clear the set it arrived with.
func TestANewPrintNamedByItsOwnSetKeepsTheSet(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, cancelableFrame(7, `{"canceled":true,"id":0}`))
	printer.awaitDialogTree(t, map[string]string{jobObjectsPath: `[{"id":0,"cancelled":true}]`})

	printer.text(t, cancelableFrame(8, ``))
	printer.awaitDialogTree(t, map[string]string{jobObjectsPath: `[]`, connectorapi.JobIDPath: "8"})

	// A telemetry frame naming the same print changes nothing.
	printer.text(t, `{"state":"PRINTING","job_id":8,"temp_nozzle":215}`)
	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "215"})

	if held := printer.dialogValue(jobObjectsPath); held != `[]` {
		t.Errorf("%s reads %s, want the empty set of print 8", jobObjectsPath, held)
	}
}

// TestASetWithNoPrintIsDropped is a machine that connects after a print ended:
// it still holds that print's set and announces it, with no job number.
func TestASetWithNoPrintIsDropped(t *testing.T) {
	t.Parallel()

	printer := standHostile(t)

	printer.text(t, `{"state":"IDLE","event":"CANCELABLE_CHANGED","data":{"objects":[{"canceled":true,"id":0}]}}`)
	printer.text(t, `{"state":"IDLE","temp_nozzle":215}`)
	printer.awaitDialogTree(t, map[string]string{"/tools/0/nozzle_temp": "215"})

	if held := printer.dialogValue(jobObjectsPath); held != "" {
		t.Errorf("%s reads %s on a machine that has named no print, want nothing", jobObjectsPath, held)
	}
}
