package deviceapi_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// answering is a Caller that answers every call with one encoded value.
type answering struct{ answer any }

func (a answering) Call(context.Context, wirerpc.Method, any) (json.RawMessage, error) {
	return json.Marshal(a.answer)
}

// A finished print crosses with the objects it ended with, so a module can say
// how many it had and which were cancelled.
func TestAFinishedPrintCrossesWithItsObjects(t *testing.T) {
	t.Parallel()

	ended := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	three := 3

	fleet := &fakeFleet{jobs: deviceapi.Jobs{Last: &deviceapi.Job{
		ID:          "0199e2c1-0405-7e48-b0af-e812537ad808",
		State:       "FINISHED",
		Started:     ended.Add(-time.Hour),
		Ended:       &ended,
		ObjectCount: &three,
		Objects: []deviceapi.JobObject{
			{ID: 0}, {ID: 1, Name: "Benchy", Cancelled: true}, {ID: 2},
		},
	}}}

	answer, err := call(t, deviceapi.Handlers(fleet), deviceapi.MethodJobs, deviceapi.JobsParams{ID: "p1"})
	if err != nil {
		t.Fatalf("jobs: %v", err)
	}

	jobs, err := deviceapi.NewClient(answering{answer: answer}).Jobs(t.Context(), deviceapi.JobsParams{ID: "p1"})
	if err != nil {
		t.Fatalf("the client refused a job within the bounds: %v", err)
	}

	last := jobs.Last
	if last == nil || last.ObjectCount == nil || *last.ObjectCount != 3 || len(last.Objects) != 3 {
		t.Fatalf("last = %+v, want three objects", last)
	}

	if !last.Objects[1].Cancelled || last.Objects[1].Name != "Benchy" || last.Objects[0].Cancelled {
		t.Errorf("objects = %+v, want object 1 cancelled and named", last.Objects)
	}
}

// A job from a host that said nothing about objects crosses with neither field,
// which is how an older host answers.
func TestAJobWithNoObjectsCarriesNeitherField(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(deviceapi.Job{ID: "x", Started: time.Unix(0, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{`"object_count"`, `"objects"`} {
		if strings.Contains(string(encoded), key) {
			t.Errorf("a job with no objects carries %s: %s", key, encoded)
		}
	}
}

// A client refuses a job over either bound, on both reads that answer jobs.
func TestAJobOverTheObjectBoundsIsRefused(t *testing.T) {
	t.Parallel()

	tooMany := make([]deviceapi.JobObject, deviceapi.MaxJobObjects+1)
	for index := range tooMany {
		tooMany[index].ID = index
	}

	longName := []deviceapi.JobObject{{ID: 0, Name: strings.Repeat("a", deviceapi.MaxJobObjectNameBytes+1)}}

	for name, objects := range map[string][]deviceapi.JobObject{"too many": tooMany, "a long name": longName} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			job := deviceapi.Job{ID: "x", Objects: objects}

			_, err := deviceapi.NewClient(answering{answer: deviceapi.Jobs{Current: &job}}).
				Jobs(t.Context(), deviceapi.JobsParams{ID: "p1"})
			if err == nil {
				t.Error("the jobs read took a job over the bound")
			}

			_, err = deviceapi.NewClient(answering{answer: deviceapi.JobPage{Items: []deviceapi.Job{job}, Limit: 1}}).
				JobHistory(t.Context(), deviceapi.JobHistoryParams{ID: "p1"})
			if err == nil {
				t.Error("the history read took a job over the bound")
			}
		})
	}
}
