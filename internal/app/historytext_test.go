package app

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
)

// The paths a printer reports as words rather than as numbers.
//
// A printer's own state is the single most interesting series it publishes, and
// until BUG-008 it was the one series no store held: the sample row carried a
// float, so every frame of /state and /material was counted as untyped on the
// way in and dropped. The question "what was this printer doing at 14:35" could
// not be asked of the store at all, on either engine.
//
// These cases ask it through the published API rather than by reading the
// table, because the answer has to survive the whole path: the feed that reads
// the frame, the store that keeps the row, and the read model that renders it.

// materialPath is the second path the simulated printer reports as a word,
// beside statePath, which sinks.go already names. They are the printer's own
// vocabulary and not this file's: /state carries IDLE, PRINTING and the rest,
// and /material carries the filament's name.
const materialPath = "/tools/0/material_package_instance/package/material/abbreviation"

// samplesOf reads one page of stored measurements through the published API,
// narrowed to one metric.
func samplesOf(t *testing.T, running *process, device, metric string) httpapi.HistorySamplePage {
	t.Helper()

	read := running.get(t, "/api/v1/history/samples?origins=device:"+device+
		"&metrics="+url.QueryEscape(metric)+"&limit=1000")
	require.Equal(t, http.StatusOK, read.status, read.body)

	var page httpapi.HistorySamplePage
	require.NoError(t, json.Unmarshal([]byte(read.body), &page))

	return page
}

// TestAPrintersOwnStateIsStoredAndReadBackAsText is BUG-008.
//
// The printer reports /state as one of a closed set of words, the SSE stream
// carries every one of them, and before this fix the store held none: a query
// naming the metric answered an empty page while nine numeric paths of the same
// printer answered thousands of rows.
//
//nolint:tparallel // the subtests are one core and one simulated printer each; running the engines side by side doubles the machine's work for no more evidence, and forEachHistoryEngine says so.
func TestAPrintersOwnStateIsStoredAndReadBackAsText(t *testing.T) {
	t.Parallel()

	forEachHistoryEngine(t, func(t *testing.T, e historyEngine) {
		textSampleCase(t, e)
	})
}

// textSampleCase is the body above, per engine.
func textSampleCase(t *testing.T, e historyEngine) {
	t.Helper()

	dir := t.TempDir()
	running := storingPastOn(t, dir, e)

	device := dialInImmediately(t, running, "SIM-TEXTSAMPLE")

	// The flush is on a timer, so this waits for the first write of the series
	// rather than for the frames that fill it.
	var page httpapi.HistorySamplePage

	require.Eventually(t, func() bool {
		page = samplesOf(t, running, device, statePath)

		return len(page.Items) > 0
	}, settle, 250*time.Millisecond,
		"no reading of %s reached the store while a printer was reporting it", statePath)

	one := page.Items[0]

	require.Equal(t, "device:"+device, one.Origin)
	require.Equal(t, statePath, one.Metric)
	require.NotNil(t, one.Text, "a state the printer reported as a word came back as no text at all")
	require.NotEmpty(t, *one.Text)
	require.Nil(t, one.Value, "a state the printer reported as a word was coerced to a number")
	require.False(t, one.At.IsZero())

	t.Logf("observed: on %s, %s held %d readings of %s, the first of them %q at %s",
		e.Name, "device:"+device, len(page.Items), statePath, *one.Text,
		one.At.Format(time.RFC3339Nano))

	// Every row of the series is text, and none of them carries a number
	// beside it: a sample holds one value or the other and never both.
	for _, item := range page.Items {
		require.NotNil(t, item.Text)
		require.Nil(t, item.Value)
	}

	// The other word-valued path the printer reports, so that what is fixed is
	// the shape of a sample and not one metric name written into the feed.
	material := samplesOf(t, running, device, materialPath)
	require.NotEmpty(t, material.Items, "no reading of %s reached the store", materialPath)
	require.NotNil(t, material.Items[0].Text)

	// And the numeric half is unchanged by the column beside it: a temperature
	// still comes back as a number and carries no text.
	nozzle := samplesOf(t, running, device, "/tools/0/nozzle_temp")
	require.NotEmpty(t, nozzle.Items, "no reading of the nozzle temperature reached the store")
	require.NotNil(t, nozzle.Items[0].Value, "a measurement came back as no number")
	require.Nil(t, nozzle.Items[0].Text, "a measurement came back carrying text as well")

	t.Logf("observed: on %s, the nozzle series holds %d numeric readings beside the %d text ones",
		e.Name, len(nozzle.Items), len(page.Items))
}

// TestAWordLongerThanTheBoundIsStoredShort is the bound on a text sample.
//
// A state path carrying a string is a series here, and every state-tree patch
// becomes a telemetry frame: a driver that reports a filename at a path, a
// moonraker /job/name, for one, writes it on every report, at the device's own
// rate. The reading is kept and clipped rather than refused, because the record
// is not improved by dropping what /state said, and the clip is counted so a
// store filling with truncated filenames is visible as the misconfigured path
// it is rather than as silence.
func TestAWordLongerThanTheBoundIsStoredShort(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", maxSampleText+64)

	word, clipped, ok := textValue(json.RawMessage(`"` + long + `"`))
	require.True(t, ok, "a long word stopped being a word")
	require.True(t, clipped, "a word over the bound was not reported as clipped")
	require.Len(t, word, maxSampleText)

	t.Logf("observed: a %d byte word was stored as %d bytes", len(long), len(word))

	// A word inside the bound is untouched and is not counted as clipped: the
	// paths this exists for report a handful of characters and must go through
	// unchanged.
	for _, short := range []string{"PRINTING", "", "PLA", strings.Repeat("b", maxSampleText)} {
		encoded, err := json.Marshal(short)
		require.NoError(t, err)

		word, clipped, ok = textValue(encoded)
		require.True(t, ok)
		require.False(t, clipped, "a word inside the bound was reported as clipped")
		require.Equal(t, short, word)
	}
}

// TestAClipNeverCutsACharacterInHalf is the one thing the bound must not do. A
// cut landing inside a multi-byte rune stores bytes that are not text, and
// everything in front of this column reads the value back as a string.
func TestAClipNeverCutsACharacterInHalf(t *testing.T) {
	t.Parallel()

	// Three bytes per character, so the bound falls inside one: 256 is not a
	// multiple of 3, and word[:256] would end on two thirds of a character.
	long := strings.Repeat("…", maxSampleText)

	word, clipped, ok := textValue(json.RawMessage(`"` + long + `"`))
	require.True(t, ok)
	require.True(t, clipped)
	require.True(t, utf8.ValidString(word), "the clip landed inside a character")
	require.LessOrEqual(t, len(word), maxSampleText)

	require.Less(t, len(word), maxSampleText, "the clip did not step back to a character boundary")

	t.Logf("observed: a %d byte word of three-byte characters was stored as %d valid bytes",
		len(long), len(word))

	// And a value that is not a word at all is still no sample, clipped or
	// otherwise: an object, an array or a null at a state path is a document.
	for _, notAWord := range []string{`{"a":1}`, `[1,2]`, `null`, ``} {
		_, clipped, ok = textValue(json.RawMessage(notAWord))
		require.Falsef(t, ok, "%s was read as a word", notAWord)
		require.False(t, clipped)
	}
}
