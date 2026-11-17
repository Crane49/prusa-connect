package query_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/buddy-debug-logs/internal/hostfake"
	"github.com/prusa3d/buddy-debug-logs/internal/query"
	"github.com/prusa3d/buddy-debug-logs/internal/sources"
	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

var start = time.Date(2026, time.October, 19, 14, 0, 0, 0, time.UTC)

func book(t *testing.T) query.Book {
	t.Helper()

	now := func() time.Time { return start.Add(time.Hour) }

	held, err := store.Open(t.TempDir(), now, store.Limits{
		Hours: 24, Bytes: map[store.Stream]int64{store.Syslog: 8 << 20, store.Metrics: 8 << 20},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = held.Close() })

	table, err := sources.Open("", &hostfake.Devices{}, now)
	if err != nil {
		t.Fatalf("sources: %v", err)
	}

	return query.Book{Store: held, Sources: table, Now: now, Viewer: query.Everyone()}
}

// A series averages each bucket, keeps its range, counts an event as 1 and
// leaves a text value out.
func TestASeriesAveragesEachBucket(t *testing.T) {
	t.Parallel()

	b := book(t)

	for i, value := range []any{10.0, 20.0, "text", true, 30.0} {
		record := store.Record{
			At: start.Add(time.Duration(i) * time.Minute), MAC: "aa:aa:aa:aa:aa:aa", Device: "mk4",
			Name: "temp_bed", Fields: map[string]any{"v": value},
		}

		if err := b.Store.Append(store.Metrics, record); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	points, err := b.Series(query.SeriesQuery{
		Device: "mk4", Name: "temp_bed",
		Window: store.Window{Since: start, Until: start.Add(10 * time.Minute)}, MaxPoints: 2,
	})
	if err != nil {
		t.Fatalf("series: %v", err)
	}

	// The first of two five-minute buckets holds 10, 20, 1 and 30.
	if len(points) != 1 || points[0].Value != (10+20+1+30)/4.0 || points[0].Min != 1 || points[0].Max != 30 {
		t.Fatalf("points = %+v", points)
	}

	if _, err = b.Series(query.SeriesQuery{Name: "", MaxPoints: 1}); err == nil {
		t.Error("a series with no metric was answered")
	}
}

// The names are read from the last hour unless a start is named.
func TestTheNamesAreReadFromTheLastHour(t *testing.T) {
	t.Parallel()

	b := book(t)

	for _, record := range []store.Record{
		{At: start.Add(-time.Hour), MAC: "m", Name: "old", Fields: map[string]any{"v": 1.0}},
		{At: start.Add(30 * time.Minute), MAC: "m", Name: "fan", Fields: map[string]any{"pwm": 100.0, "state": 1.0}},
	} {
		if err := b.Store.Append(store.Metrics, record); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	names, truncated, err := b.Names("", store.Window{})
	if err != nil || truncated || len(names) != 1 || names[0].Name != "fan" || len(names[0].Fields) != 2 {
		t.Fatalf("names = %+v, %v", names, err)
	}
}

// A line is written for a download with its time, its source and its level.
func TestALineIsFormattedForADownload(t *testing.T) {
	t.Parallel()

	severity := 4
	got := query.Format(store.Record{
		At: start, MAC: "m", Severity: &severity, Component: "Marlin", Text: "two\nlines",
	}, "mk4")

	if got != "2026-10-19T14:00:00.000Z mk4 WARNING Marlin: two\\x0alines\n" {
		t.Errorf("got %q", got)
	}
}

// A viewer reads the records of the printers it may see, by the device a
// record was matched to or the one its MAC is matched to now. A record matched
// to no printer goes only to a viewer of the whole fleet.
func TestAViewerReadsOnlyItsPrinters(t *testing.T) {
	t.Parallel()

	b := book(t)
	severity := 6

	for _, record := range []store.Record{
		{At: start, Device: "mk4", MAC: "aa:aa:aa:aa:aa:aa", Severity: &severity, Text: "mk4"},
		{At: start, Device: "xl", MAC: "bb:bb:bb:bb:bb:bb", Severity: &severity, Text: "xl"},
		{At: start, MAC: "cc:cc:cc:cc:cc:cc", Severity: &severity, Text: "stranger"},
		{At: start, IP: "10.0.0.9", Raw: "nonsense"},
	} {
		if err := b.Store.Append(store.Syslog, record); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	for name, tc := range map[string]struct {
		viewer query.Viewer
		want   int
	}{
		"nobody":     {query.Viewer{}, 0},
		"one":        {query.ViewerOf(map[string]bool{"mk4": true}, false), 1},
		"two":        {query.ViewerOf(map[string]bool{"mk4": true, "xl": true}, false), 2},
		"everything": {query.ViewerOf(map[string]bool{"mk4": true, "xl": true}, true), 4},
	} {
		b.Viewer = tc.viewer

		page, err := b.Logs(query.LogFilter{}, "", "", 10)
		if err != nil || len(page.Lines) != tc.want {
			t.Errorf("%s reads %d lines, want %d (%v)", name, len(page.Lines), tc.want, err)
		}

		export, err := b.Export(store.Syslog, "", store.Window{}, "")
		if err != nil || strings.Count(export.Text, "\n") != tc.want {
			t.Errorf("%s downloads %q, want %d lines", name, export.Text, tc.want)
		}
	}
}

// A viewer whose filter matches no line gets no cursor, because a cursor of
// another printer's line would tell how much that printer sent. The next
// page for the filter finds the viewer's line when it comes.
func TestAViewerWithNoLinesGetsNoCursor(t *testing.T) {
	t.Parallel()

	b := book(t)
	severity := 6

	if err := b.Store.Append(store.Syslog, store.Record{
		At: start, Device: "xl", MAC: "bb:bb:bb:bb:bb:bb", Severity: &severity, Text: "xl",
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	b.Viewer = query.ViewerOf(map[string]bool{"mk4": true}, false)

	page, err := b.Logs(query.LogFilter{}, "", "", 10)
	if err != nil || len(page.Lines) != 0 || page.Newest != "" || page.Older != "" {
		t.Fatalf("page = %+v, %v", page, err)
	}

	if err = b.Store.Append(store.Syslog, store.Record{
		At: start, Device: "mk4", MAC: "aa:aa:aa:aa:aa:aa", Severity: &severity, Text: "mk4",
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	page, err = b.Logs(query.LogFilter{}, "", "", 10)
	if err != nil || len(page.Lines) != 1 || page.Lines[0].Text != "mk4" || page.Newest != page.Lines[0].Cursor {
		t.Fatalf("page = %+v, %v", page, err)
	}
}

// A record that arrived after the end of the window is left out of a series
// with no until, so the buckets never number more than MaxPoints.
func TestASeriesLeavesOutWhatCameAfterItsEnd(t *testing.T) {
	t.Parallel()

	b := book(t)

	// The book's clock is an hour after start, which is the series' end.
	for _, at := range []time.Time{start.Add(30 * time.Minute), start.Add(3 * time.Hour)} {
		if err := b.Store.Append(store.Metrics, store.Record{
			At: at, MAC: "aa:aa:aa:aa:aa:aa", Device: "mk4", Name: "temp_bed", Fields: map[string]any{"v": 50.0},
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	points, err := b.Series(query.SeriesQuery{
		Device: "mk4", Name: "temp_bed", Window: store.Window{Since: start}, MaxPoints: 1,
	})
	if err != nil || len(points) != 1 {
		t.Fatalf("points = %+v, %v; want one", points, err)
	}
}

// A span that does not divide into the buckets puts what is left into the
// last one, so there are never more than MaxPoints of them.
func TestASeriesNeverHasMoreThanMaxPoints(t *testing.T) {
	t.Parallel()

	b := book(t)
	since := start.Add(time.Hour).Add(-999 * time.Nanosecond)

	for i := range 999 {
		if err := b.Store.Append(store.Metrics, store.Record{
			At: since.Add(time.Duration(i) * time.Nanosecond), MAC: "aa:aa:aa:aa:aa:aa", Device: "mk4",
			Name: "temp_bed", Fields: map[string]any{"v": float64(i)},
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	points, err := b.Series(query.SeriesQuery{
		Device: "mk4", Name: "temp_bed", Window: store.Window{Since: since}, MaxPoints: 300,
	})
	if err != nil || len(points) > 300 {
		t.Fatalf("%d points, %v; want at most 300", len(points), err)
	}
}

// The names are capped, and the answer says some were left out.
func TestTheNamesAreCapped(t *testing.T) {
	t.Parallel()

	b := book(t)

	fields := map[string]any{}
	for i := range query.MaxNameFields + 5 {
		fields[fmt.Sprintf("f%d", i)] = 1.0
	}

	for i := range query.MaxNames + 5 {
		record := store.Record{At: start.Add(30 * time.Minute), MAC: "m", Name: fmt.Sprintf("n%04d", i), Fields: fields}
		if err := b.Store.Append(store.Metrics, record); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	names, truncated, err := b.Names("", store.Window{})
	if err != nil || !truncated || len(names) != query.MaxNames || len(names[0].Fields) != query.MaxNameFields {
		t.Errorf("%d names of %d fields, truncated %v, %v", len(names), len(names[0].Fields), truncated, err)
	}
}

// A control character in a kept record is escaped in a download.
func TestADownloadEscapesControlCharacters(t *testing.T) {
	t.Parallel()

	got := query.Format(store.Record{At: start, IP: "10.0.0.9", Raw: "a\x1b[2J\rb"}, "")
	if got != "2026-10-19T14:00:00.000Z UNREADABLE from 10.0.0.9: a\\x1b[2J\\x0db\n" {
		t.Errorf("got %q", got)
	}

	got = query.Format(store.Record{At: start, Line: "temp\nfake 1"}, "")
	if strings.Count(got, "\n") != 1 {
		t.Errorf("a point became %q", got)
	}
}
