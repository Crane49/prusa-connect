package app

import (
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/platform/devicestore"
)

// printIDPattern is the shape of a print's id: a version 7 UUID in the
// canonical lowercase form.
var printIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// TestAReadingIsMatchedToThePrintItBelongsTo is the matching rule, one row per
// case it decides. A reading is compared with the print the device began last,
// never with older ones.
func TestAReadingIsMatchedToThePrintItBelongsTo(t *testing.T) {
	t.Parallel()

	const old = "11111111-1111-4111-8111-111111111111"

	printing := map[string]string{"/state": `"PRINTING"`, "/job/id": `7`}
	finished := map[string]string{"/state": `"FINISHED"`, "/job/id": `7`, "/job/state": `"FINISHED"`}
	idleAfter := map[string]string{"/state": `"IDLE"`, "/job/id": `7`, "/job/state": `"FINISHED"`}

	cases := []struct {
		name   string
		latest latestPrint
		known  bool
		tree   map[string]string
		// quiet is whether the take saw the machine idle or ready.
		quiet bool
		// opens is whether the reading begins a print of its own, and
		// abandons is the id of the open print it replaces.
		opens    bool
		abandons string
	}{
		{
			name:  "a device that has never printed",
			tree:  printing,
			opens: true,
		},
		{
			name:   "the print that is running",
			latest: latestPrint{id: old, number: "7"},
			known:  true,
			tree:   printing,
		},
		{
			name:     "a different number while a print is open",
			latest:   latestPrint{id: old, number: "6"},
			known:    true,
			tree:     printing,
			opens:    true,
			abandons: old,
		},
		{
			name:   "a different number after a print ended",
			latest: latestPrint{id: old, number: "6", ended: true},
			known:  true,
			tree:   printing,
			opens:  true,
		},
		{
			name:   "the number again, after the machine was seen idle",
			latest: latestPrint{id: old, number: "7", ended: true, quiet: true},
			known:  true,
			tree:   printing,
			opens:  true,
		},
		{
			name:   "the number again, with the machine seen idle in the same take",
			latest: latestPrint{id: old, number: "7", ended: true},
			known:  true,
			tree:   printing,
			quiet:  true,
			opens:  true,
		},
		{
			// A report that arrives right after the end has no idle reading
			// before it. It is about the print that ended.
			name:   "the number again, with no idle reading since the end",
			latest: latestPrint{id: old, number: "7", ended: true},
			known:  true,
			tree:   printing,
		},
		{
			// A print-preview dialog names the number of the print before it,
			// because the counter moves only when a print starts.
			name:   "a print preview under the number, after the machine was seen idle",
			latest: latestPrint{id: old, number: "7", ended: true, quiet: true},
			known:  true,
			tree:   map[string]string{"/state": `"ATTENTION"`, "/job/id": `7`},
			quiet:  true,
		},
		{
			// The machine stays idle with the ended print's number in its
			// tree. That is the print that ended and not a new one.
			name:   "an idle machine still naming the print that ended",
			latest: latestPrint{id: old, number: "7", ended: true, quiet: true},
			known:  true,
			tree:   idleAfter,
			quiet:  true,
		},
		{
			name:   "the reading that ends the print",
			latest: latestPrint{id: old, number: "7"},
			known:  true,
			tree:   finished,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := matchPrint(c.latest, c.known, jobFactsOf(treeOf(c.tree)), c.quiet, func() string { return "minted" })

			require.Equal(t, c.opens, got.opens)
			require.Equal(t, c.abandons, got.abandons)

			if c.opens {
				require.Equal(t, "minted", got.id)
			} else {
				require.Equal(t, old, got.id)
			}
		})
	}
}

// TestTheReadingThatEndsAPrintIsNotQuiet is the half of the rule a late report
// depends on. FINISHED and STOPPED are the print ending, and a machine that
// reports them has not been seen idle since: counting them would let the next
// PRINTING report from the same print open a print of its own.
func TestTheReadingThatEndsAPrintIsNotQuiet(t *testing.T) {
	t.Parallel()

	for _, state := range []string{"FINISHED", "STOPPED", "BUSY", "PRINTING", "PAUSED", "ATTENTION"} {
		require.False(t, quietState(state), "%s counted as the machine seen idle", state)
	}

	for _, state := range []string{"IDLE", "READY"} {
		require.True(t, quietState(state), "%s did not count as the machine seen idle", state)
	}
}

// TestALatestPrintIsReadOffTheStoredRow is how the book starts: the stored row
// says whether the print ended, and the device's last record says whether the
// machine was seen idle after it and when it was last heard from.
func TestALatestPrintIsReadOffTheStoredRow(t *testing.T) {
	t.Parallel()

	seen := time.Date(2026, 10, 13, 14, 0, 0, 0, time.UTC)

	ended := storedLatestPrint(jobRowEnded("7"), devicestore.DeviceRecord{MachineState: "IDLE", LastSeen: seen})
	require.Equal(t, latestPrint{id: "row", number: "7", ended: true, quiet: true, seen: seen}, ended)

	stale := storedLatestPrint(jobRowEnded("7"), devicestore.DeviceRecord{MachineState: "PRINTING"})
	require.False(t, stale.quiet,
		"a restored tree that still says PRINTING counted as the machine seen idle")
}

// jobRowEnded is a stored print that has ended, as LatestDeviceJobs reads it.
func jobRowEnded(number string) devicestore.PrinterJob {
	return devicestore.PrinterJob{
		JobID:       "row",
		DeviceID:    "p-alpha",
		DeviceJobID: number,
		State:       "FINISHED",
		Started:     time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
		Ended:       time.Date(2026, 10, 13, 13, 0, 0, 0, time.UTC),
	}
}

// TestTheListingNamesNoPrintTheFlushHasNotMatched is the job summary on the
// device listing between a reading and the flush that matches it. A print that
// reuses the number of a print that ended, after the machine was seen idle, is
// a new print: until the flush mints its id it has none, and it never carries
// the id of the print before it.
func TestTheListingNamesNoPrintTheFlushHasNotMatched(t *testing.T) {
	t.Parallel()

	const old = "11111111-1111-4111-8111-111111111111"

	book := newPrintBook()
	book.put("p-alpha", latestPrint{id: old, number: "7", ended: true, quiet: true})

	running := jobFactsOf(treeOf(map[string]string{"/state": `"PRINTING"`, "/job/id": `7`}))
	idle := jobFactsOf(treeOf(map[string]string{"/state": `"IDLE"`, "/job/id": `7`}))

	require.Empty(t, book.idOf("p-alpha", running), "a new print under a reused number carried the old print's id")
	require.Equal(t, old, book.idOf("p-alpha", idle), "the print that ended lost its id on the listing")
}

// TestAPrintWrittenAfterTheFactLeadsWithItsStart is the id of a print the demo
// writes into the past: a version 7 UUID whose time is the print's start, so it
// sorts among the prints recorded as they ran.
func TestAPrintWrittenAfterTheFactLeadsWithItsStart(t *testing.T) {
	t.Parallel()

	began := time.Date(2026, 10, 13, 12, 0, 0, 500_000_000, time.UTC)
	id := printIDAt(began, [10]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})

	require.Regexp(t, printIDPattern, id)

	parsed, err := uuid.Parse(id)
	require.NoError(t, err)

	seconds, nanos := parsed.Time().UnixTime()
	require.Equal(t, began, time.Unix(seconds, nanos).UTC())
	require.Less(t, printIDAt(began, [10]byte{}), printIDAt(began.Add(time.Millisecond), [10]byte{}))
}
