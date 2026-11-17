package collect_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/diagnosticsapi"

	"github.com/prusa3d/buddy-debug-logs/internal/collect"
	"github.com/prusa3d/buddy-debug-logs/internal/hostfake"
	"github.com/prusa3d/buddy-debug-logs/internal/query"
	"github.com/prusa3d/buddy-debug-logs/internal/sources"
	"github.com/prusa3d/buddy-debug-logs/internal/store"
)

func book(t *testing.T) query.Book {
	t.Helper()

	at := time.Date(2026, time.October, 19, 14, 0, 0, 0, time.UTC)
	now := func() time.Time { return at }

	held, err := store.Open(t.TempDir(), now, store.Limits{
		Hours: 24, Bytes: map[store.Stream]int64{store.Syslog: 64 << 20, store.Metrics: 64 << 20},
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

// Many lines go out over several pages, each under the page limit, every
// line exactly once, newest first, and the metrics after the lines.
func TestEveryLineGoesOutOnceNewestFirst(t *testing.T) {
	t.Parallel()

	b := book(t)
	base := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	severity := 6

	const lines = 4000

	for i := range lines {
		mac := []string{"aa:aa:aa:aa:aa:aa", "bb:bb:bb:bb:bb:bb"}[i%2]
		record := store.Record{
			At: base.Add(time.Duration(i) * time.Second), MAC: mac, Severity: &severity,
			Component: "GUI", Text: fmt.Sprintf("line %05d %s", i, strings.Repeat("x", 150)),
		}

		if err := b.Store.Append(store.Syslog, record); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	if err := b.Store.Append(
		store.Metrics,
		store.Record{At: base, MAC: "aa:aa:aa:aa:aa:aa", Line: "temp_bed v=60 0", Name: "temp_bed"},
	); err != nil {
		t.Fatalf("append: %v", err)
	}

	collector := collect.Collector{Book: b, Status: func() any { return map[string]string{"ok": "yes"} }}

	files := map[string]string{}
	cursor := ""
	pages := 0

	for {
		page, err := collector.Page(cursor)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}

		if err = page.Validate(); err != nil {
			t.Fatalf("page %d would be refused: %v", pages, err)
		}

		for _, file := range page.Files {
			files[file.Name] += file.Text
		}

		pages++

		if page.Cursor == "" {
			break
		}

		cursor = page.Cursor

		if pages > 100 {
			t.Fatal("the pages never end")
		}
	}

	if pages < 4 {
		t.Errorf(
			"%d pages for about 700 KiB of lines; a page holds at most %d bytes",
			pages,
			diagnosticsapi.MaxPageBytes,
		)
	}

	// Neither MAC is matched to a printer, so each is named by its alias.
	firstName := b.Sources.Alias("aa:aa:aa:aa:aa:aa")
	first := files["syslog/"+firstName+".log"]
	second := files["syslog/"+b.Sources.Alias("bb:bb:bb:bb:bb:bb")+".log"]

	for name := range files {
		if strings.Contains(name, "aa-aa") || strings.Contains(name, "bb-bb") {
			t.Errorf("the file %s is named by a MAC", name)
		}
	}

	if got := strings.Count(first, "\n") + strings.Count(second, "\n"); got != lines {
		t.Fatalf("%d lines went out, want %d", got, lines)
	}

	if !strings.HasPrefix(second, "2026-10-19T13:06:39.000Z INFO GUI: line 03999") {
		t.Errorf("the newest line is not first: %q", second[:80])
	}

	if !strings.Contains(files["metrics/"+firstName+".lp"], "temp_bed v=60 0") {
		t.Errorf("the metrics did not go out: %v", files["metrics/"+firstName+".lp"])
	}

	if files["status.json"] == "" || files["sources.json"] == "" {
		t.Error("the first page carried no status or sources")
	}
}

// A store with nothing kept answers the first page and no cursor.
func TestAnEmptyStoreIsOnePage(t *testing.T) {
	t.Parallel()

	page, err := collect.Collector{Book: book(t), Status: func() any { return nil }}.Page("")
	if err != nil || page.Cursor != "" || len(page.Files) != 2 {
		t.Fatalf("page = %+v, %v", page, err)
	}

	if _, err = (collect.Collector{Book: book(t)}).Page("nonsense"); err == nil {
		t.Error("a cursor this module did not write was read")
	}
}

// A source matched to a printer is named by the printer's id, one matched to
// none by its alias, which sources.json lists beside its MAC, and a datagram
// in neither format by unreadable. No file name holds a MAC or an address.
func TestAFileIsNamedByItsPrinterOrAnAlias(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 19, 14, 0, 0, 0, time.UTC)
	devices := &hostfake.Devices{}
	devices.Add("mk4", "MK4", "10:9c:70:2a:bb:cd", "", "")

	b := book(t)

	table, err := sources.Open("", devices, func() time.Time { return at })
	if err != nil {
		t.Fatalf("sources: %v", err)
	}

	if err = table.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	b.Sources = table
	table.Resolve("10:9c:70:2a:bb:cd", "192.168.1.20", true)
	table.Resolve("02:00:00:00:00:01", "192.168.1.21", true)

	severity := 6

	for _, record := range []store.Record{
		{At: at, MAC: "10:9c:70:2a:bb:cd", IP: "192.168.1.20", Severity: &severity, Text: "matched"},
		{At: at, MAC: "02:00:00:00:00:01", IP: "192.168.1.21", Severity: &severity, Text: "stranger"},
		{At: at, IP: "192.168.1.22", Raw: "nonsense"},
	} {
		if err = b.Store.Append(store.Syslog, record); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	collector := collect.Collector{Book: b, Status: func() any { return nil }}

	first, err := collector.Page("")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}

	page, err := collector.Page(first.Cursor)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}

	alias := table.Alias("02:00:00:00:00:01")
	names := map[string]bool{}

	for _, file := range page.Files {
		names[file.Name] = true

		if strings.Contains(file.Name, "192") || strings.Contains(file.Name, "00-00") {
			t.Errorf("the file %s is named by an address or a MAC", file.Name)
		}
	}

	for _, want := range []string{"syslog/mk4.log", "syslog/" + alias + ".log", "syslog/unreadable.log"} {
		if !names[want] {
			t.Errorf("no file %s in %v", want, names)
		}
	}

	if !strings.Contains(first.Files[1].Text, `"alias": "`+alias+`"`) {
		t.Errorf("sources.json does not list the alias:\n%s", first.Files[1].Text)
	}
}

// A page ends before it names more files than the host takes, and the next
// page goes on from there, so a fleet of many senders still sends every line.
func TestAPageNamesNoMoreFilesThanTheHostTakes(t *testing.T) {
	t.Parallel()

	b := book(t)
	at := time.Date(2026, time.October, 19, 13, 0, 0, 0, time.UTC)
	severity := 6
	senders := diagnosticsapi.MaxPageFiles + 10

	for i := range senders {
		record := store.Record{
			At: at, Device: fmt.Sprintf("printer-%04d", i), MAC: "aa:aa:aa:aa:aa:aa", Severity: &severity, Text: "x",
		}

		if err := b.Store.Append(store.Syslog, record); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	collector := collect.Collector{Book: b, Status: func() any { return nil }}
	names := map[string]bool{}
	cursor := ""

	for pages := 0; ; pages++ {
		page, err := collector.Page(cursor)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}

		if err = page.Validate(); err != nil {
			t.Fatalf("page %d would be refused: %v", pages, err)
		}

		for _, file := range page.Files {
			names[file.Name] = true
		}

		if page.Cursor == "" || pages > 10 {
			break
		}

		cursor = page.Cursor
	}

	if got := len(names) - 2; got != senders {
		t.Errorf("%d log files went out, want %d", got, senders)
	}
}
