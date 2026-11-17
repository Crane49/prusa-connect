package app

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
)

// The objects of a print, from the state tree a driver filled to the answer a
// module reads.

func TestTheObjectsAreReadOutOfTheStateTree(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  string
	}{
		{name: "a machine that said nothing", value: "", want: ""},
		{name: "a driver that cleared the path", value: `null`, want: ""},
		{name: "a value that is not an array", value: `{"id":1}`, want: ""},
		{name: "a print with no objects", value: `[]`, want: `{"count":0,"objects":[]}`},
		{
			name:  "one cancelled out of three, out of order and repeated",
			value: `[{"id":2,"cancelled":false},{"id":0,"cancelled":false},{"id":1,"cancelled":true},{"id":2,"cancelled":true}]`,
			want: `{"count":3,"objects":[{"id":0,"cancelled":false},{"id":1,"cancelled":true},` +
				`{"id":2,"cancelled":true}]}`,
		},
		{
			name:  "a name",
			value: `[{"id":0,"name":"Benchy","cancelled":false}]`,
			want:  `{"count":1,"objects":[{"id":0,"name":"Benchy","cancelled":false}]}`,
		},
	}

	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			t.Parallel()

			pairs := map[string]string{"/state": `"PRINTING"`, "/job/id": `7`}
			if one.value != "" {
				pairs[connectorapi.JobObjectsPath] = one.value
			}

			got := jobFactsOf(treeOf(pairs)).objects
			if one.want == "" {
				require.Empty(t, got)

				return
			}

			require.JSONEq(t, one.want, got)
		})
	}
}

// TestAPrintWithMoreObjectsThanTheBoundKeepsTheCancelledOnes is the bound. The
// cancelled objects are the answer a reader wants, so they are the ones kept.
func TestAPrintWithMoreObjectsThanTheBoundKeepsTheCancelledOnes(t *testing.T) {
	t.Parallel()

	const total = 1024

	cancelled := map[int]bool{3: true, 700: true, 1023: true}

	reported := make([]connectorapi.JobObject, 0, total)
	for id := range total {
		reported = append(reported, connectorapi.JobObject{ID: id, Cancelled: cancelled[id]})
	}

	bounded := boundJobObjects(reported)

	require.Equal(t, total, bounded.Count, "the count is the machine's, not the bound's")
	require.Len(t, bounded.Objects, deviceapi.MaxJobObjects)

	kept := map[int]bool{}

	for index, object := range bounded.Objects {
		if index > 0 {
			require.Less(t, bounded.Objects[index-1].ID, object.ID, "the list is not ordered by id")
		}

		if object.Cancelled {
			kept[object.ID] = true
		}
	}

	require.Equal(t, cancelled, kept, "a cancelled object was dropped to make room for one nobody touched")
}

// TestALongNameIsCutToTheBound holds a name to the bound, marker included, and
// never cuts through a character.
func TestALongNameIsCutToTheBound(t *testing.T) {
	t.Parallel()

	name := strings.Repeat("Žluťoučký kůň ", 10)

	bounded := boundJobObjects([]connectorapi.JobObject{{ID: 0, Name: name}})

	got := bounded.Objects[0].Name
	require.LessOrEqual(t, len(got), deviceapi.MaxJobObjectNameBytes)
	require.True(t, utf8.ValidString(got), "the cut went through a character: %q", got)
	require.True(t, strings.HasSuffix(got, "…"), "a cut name carries no marker: %q", got)
}

// TestAFinishedPrintKeepsItsObjectsOnTheWayToAModule is the round trip: the
// reading that ends a print writes the row with its objects, and the row
// reaches a module through the HTTP render and the contract's own shape.
func TestAFinishedPrintKeepsItsObjectsOnTheWayToAModule(t *testing.T) {
	t.Parallel()

	tree := treeOf(map[string]string{
		"/state":                    `"FINISHED"`,
		"/job/id":                   `7`,
		"/job/state":                `"FINISHED"`,
		connectorapi.JobObjectsPath: `[{"id":0,"cancelled":false},{"id":1,"cancelled":true},{"id":2,"cancelled":false}]`,
	})

	facts := jobFactsOf(tree)
	record := devicestore.DeviceRecord{DeviceID: "p-alpha", LastSeen: startedAtInApp, State: tree}

	row, held := jobRowOf(record, facts, "print-1")
	require.True(t, held)
	require.False(t, row.Running(), "the reading that ends the print did not end the row")

	rendered := jobRender(row)
	require.NotNil(t, rendered.ObjectCount)
	require.Equal(t, 3, *rendered.ObjectCount)

	encoded, err := json.Marshal(moduleJob(&rendered))
	require.NoError(t, err)

	var read deviceapi.Job
	require.NoError(t, json.Unmarshal(encoded, &read))

	require.NotNil(t, read.Ended)
	require.NotNil(t, read.ObjectCount)
	require.Equal(t, 3, *read.ObjectCount)

	var skipped []int

	for _, object := range read.Objects {
		if object.Cancelled {
			skipped = append(skipped, object.ID)
		}
	}

	require.Equal(t, []int{1}, skipped, "the module read %s", encoded)

	// The device render carries the same set, out of the state tree.
	book := newPrintBook()
	book.put("p-alpha", latestPrint{id: "print-1", number: facts.machineID})

	summary := jobOf(tree, book, "p-alpha")
	require.NotNil(t, summary)
	require.Equal(t, rendered.Objects, summary.Objects)
}

// TestAMachineThatSaidNothingAboutObjectsCarriesNone keeps the fields absent,
// so a module can tell a print with no objects from a machine that does not
// report them.
func TestAMachineThatSaidNothingAboutObjectsCarriesNone(t *testing.T) {
	t.Parallel()

	tree := treeOf(map[string]string{"/state": `"PRINTING"`, "/job/id": `7`})

	row, held := jobRowOf(devicestore.DeviceRecord{DeviceID: "p-alpha", LastSeen: startedAtInApp, State: tree},
		jobFactsOf(tree), "print-1")
	require.True(t, held)

	rendered := jobRender(row)

	encoded, err := json.Marshal(moduleJob(&rendered))
	require.NoError(t, err)

	for _, key := range []string{`"object_count"`, `"objects"`} {
		require.NotContainsf(t, string(encoded), key, "a machine that said nothing carries %s", key)
	}
}
