package sources_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prusa3d/buddy-debug-logs/internal/hostfake"
	"github.com/prusa3d/buddy-debug-logs/internal/sources"
)

func now() time.Time { return time.Date(2026, time.October, 19, 14, 0, 0, 0, time.UTC) }

func fleet() *hostfake.Devices {
	devices := &hostfake.Devices{}
	devices.Add("mk4", "MK4 left", "10:9c:70:2a:bb:cd", "192.168.1.20", "")
	devices.Add("xl", "XL", "", "", "192.168.1.30")

	return devices
}

func opened(t *testing.T, dir string, devices *hostfake.Devices) *sources.Table {
	t.Helper()

	table, err := sources.Open(dir, devices, now)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err = table.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	return table
}

// A printer on Ethernet is matched by its MAC, and one on Wi-Fi alone by the
// address the datagram came from.
func TestADatagramIsMatchedByMACOrByAddress(t *testing.T) {
	t.Parallel()

	table := opened(t, t.TempDir(), fleet())

	if got := table.Resolve("10:9c:70:2a:bb:cd", "192.168.1.99", true); got != "mk4" {
		t.Errorf("the Ethernet printer matched %q", got)
	}

	if got := table.Resolve("aa:aa:aa:aa:aa:aa", "192.168.1.30", false); got != "xl" {
		t.Errorf("the Wi-Fi printer matched %q", got)
	}

	if got := table.Resolve("bb:bb:bb:bb:bb:bb", "10.0.0.1", true); got != "" {
		t.Errorf("a stranger matched %q", got)
	}

	list := table.List()
	if len(list) != 3 || list[0].MAC != "10:9c:70:2a:bb:cd" || list[0].DeviceName != "MK4 left" || list[0].Syslog != 1 {
		t.Errorf("sources = %+v", list)
	}
}

// A pair matched by address is kept by the next read of the fleet, so it
// survives a new address and a restart.
func TestAMatchedPairSurvivesANewAddressAndARestart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	table := opened(t, dir, fleet())

	table.Resolve("aa:aa:aa:aa:aa:aa", "192.168.1.30", true)

	if err := table.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	again := opened(t, dir, &hostfake.Devices{})

	if got := again.Resolve("aa:aa:aa:aa:aa:aa", "192.168.1.77", true); got != "xl" {
		t.Errorf("after a restart the MAC matched %q", got)
	}

	if err := again.SaveError(); err != nil {
		t.Errorf("save: %v", err)
	}
}

// A person assigns an unknown MAC by hand, and a refresh does not undo it.
func TestAPersonAssignsASourceByHand(t *testing.T) {
	t.Parallel()

	devices := fleet()
	table := opened(t, t.TempDir(), devices)

	table.Resolve("bb:bb:bb:bb:bb:bb", "10.0.0.1", true)

	if _, err := table.Assign("bb:bb:bb:bb:bb:bb", "xl"); err != nil {
		t.Fatalf("assign: %v", err)
	}

	if err := table.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if got := table.DeviceOf("bb:bb:bb:bb:bb:bb"); got != "xl" {
		t.Errorf("after a refresh the MAC is on %q", got)
	}

	if macs := table.MACsOf("xl"); len(macs) != 1 || macs[0] != "bb:bb:bb:bb:bb:bb" {
		t.Errorf("MACs of xl = %v", macs)
	}

	if _, err := table.Assign("cc:cc:cc:cc:cc:cc", "xl"); !errors.Is(err, sources.ErrUnknownSource) {
		t.Errorf("an unseen MAC: %v", err)
	}

	if _, err := table.Assign("bb:bb:bb:bb:bb:bb", "nope"); !errors.Is(err, sources.ErrUnknownDevice) {
		t.Errorf("an unknown device: %v", err)
	}
}

// A miss asks for a read of the fleet, and the next miss soon after does not.
func TestAMissAsksForOneReadOfTheFleet(t *testing.T) {
	t.Parallel()

	devices := fleet()

	table, err := sources.Open("", devices, func() time.Time { return now().Add(time.Hour) })
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	table.Resolve("bb:bb:bb:bb:bb:bb", "10.0.0.1", true)
	table.Resolve("bb:bb:bb:bb:bb:bb", "10.0.0.1", true)

	if devices.Lists() != 0 {
		t.Fatalf("Resolve read the fleet itself %d times; it must leave that to Run", devices.Lists())
	}
}

// strangerMAC is the i-th MAC of a sender no printer reports.
func strangerMAC(i int) string { return fmt.Sprintf("02:00:00:00:%02x:%02x", i>>8, i&0xff) }

// fillTable sends one datagram from each of n strangers, one second apart.
func fillTable(table *sources.Table, clock *time.Time, n int) {
	for i := range n {
		*clock = clock.Add(time.Second)
		table.Resolve(strangerMAC(i), "10.0.0.1", true)
	}
}

// A full table makes room for a new MAC by forgetting the unassigned source
// seen longest ago, so strangers cannot keep a printer out of it.
func TestAFullTableForgetsTheOldestStranger(t *testing.T) {
	t.Parallel()

	at := now()
	table, err := sources.Open("", fleet(), func() time.Time { return at })
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err = table.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	fillTable(table, &at, 1024)

	if got := table.Resolve("10:9c:70:2a:bb:cd", "192.168.1.20", true); got != "mk4" {
		t.Fatalf("the printer matched %q in a full table", got)
	}

	list := table.List()
	if len(list) != 1024 {
		t.Errorf("the table holds %d sources, want 1024", len(list))
	}

	if table.DeviceOf("10:9c:70:2a:bb:cd") != "mk4" {
		t.Error("the printer's source was not kept")
	}

	for _, source := range list {
		if source.MAC == strangerMAC(0) {
			t.Error("the stranger seen longest ago is still kept")
		}
	}
}

// A table full of matched sources keeps them all, and still answers the
// printer a new datagram matches.
func TestAFullTableOfMatchedSourcesStillMatches(t *testing.T) {
	t.Parallel()

	at := now()
	table, err := sources.Open("", fleet(), func() time.Time { return at })
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err = table.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	fillTable(table, &at, 1024)

	for i := range 1024 {
		if _, err = table.Assign(strangerMAC(i), "xl"); err != nil {
			t.Fatalf("assign: %v", err)
		}
	}

	if got := table.Resolve("10:9c:70:2a:bb:cd", "192.168.1.20", true); got != "mk4" {
		t.Errorf("the printer matched %q in a table of matched sources", got)
	}

	if got := table.Resolve("cc:cc:cc:cc:cc:cc", "192.168.1.30", true); got != "xl" {
		t.Errorf("the Wi-Fi printer matched %q in a table of matched sources", got)
	}

	if n := len(table.List()); n != 1024 {
		t.Errorf("the table holds %d sources, want 1024", n)
	}
}

// A pair matched by its address alone is not written when its datagram
// arrives, so a burst of datagrams does not write the file each time. The
// next read of the fleet writes it. A pair matched by MAC is written at once.
func TestAPairMatchedByAddressIsWrittenAtTheNextRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	table := opened(t, dir, fleet())
	path := filepath.Join(dir, "sources.json")

	table.Resolve("aa:aa:aa:aa:aa:aa", "192.168.1.30", true)

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a match by address wrote the file at once: %v", err)
	}

	table.Resolve("10:9c:70:2a:bb:cd", "192.168.1.99", true)

	if raw, err := os.ReadFile(path); err != nil || !strings.Contains(string(raw), "10:9c:70:2a:bb:cd") {
		t.Fatalf("a match by MAC was not written: %s, %v", raw, err)
	}

	if err := table.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	raw, err := os.ReadFile(path)

	written := string(raw)
	if err != nil || !strings.Contains(written, "aa:aa:aa:aa:aa:aa") || !strings.Contains(written, "by_address") {
		t.Fatalf("the next read did not write the match by address: %s, %v", raw, err)
	}
}

// A sender can forge the address of a printer on Wi-Fi alone. A table full
// of made-up MACs matched by that address still makes room for a printer, by
// forgetting the one seen longest ago, and a burst of them writes no file.
func TestAFullTableForgetsAPairMatchedByAddress(t *testing.T) {
	t.Parallel()

	at := now()
	dir := t.TempDir()

	table, err := sources.Open(dir, fleet(), func() time.Time { return at })
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err = table.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	for i := range 1024 {
		at = at.Add(time.Second)

		if got := table.Resolve(strangerMAC(i), "192.168.1.30", true); got != "xl" {
			t.Fatalf("a datagram from the Wi-Fi printer's address matched %q", got)
		}
	}

	if _, err = os.Stat(filepath.Join(dir, "sources.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the burst wrote the file: %v", err)
	}

	if got := table.Resolve("10:9c:70:2a:bb:cd", "192.168.1.20", true); got != "mk4" {
		t.Fatalf("the printer matched %q in a full table", got)
	}

	if table.DeviceOf("10:9c:70:2a:bb:cd") != "mk4" {
		t.Error("the printer's source was not kept")
	}

	if table.DeviceOf(strangerMAC(0)) != "" {
		t.Error("the pair matched by address seen longest ago is still kept")
	}

	if n := len(table.List()); n != 1024 {
		t.Errorf("the table holds %d sources, want 1024", n)
	}
}

// A printer that is away is matched by its MAC, because its link to the
// server may be down while it still sends. Its address matches nothing, and a
// connected printer that reports the same MAC or address wins.
func TestAPrinterThatIsAwayIsMatchedByItsMACAlone(t *testing.T) {
	t.Parallel()

	devices := &hostfake.Devices{}
	devices.AddAway("gone", "Gone", "10:9c:70:00:00:01", "", "192.168.1.40")
	devices.Add("new", "New", "10:9c:70:00:00:01", "", "192.168.1.40")
	devices.AddAway("away", "Away", "10:9c:70:00:00:02", "192.168.1.41", "")
	devices.AddAway("twin1", "Twin", "10:9c:70:00:00:03", "", "")
	devices.AddAway("twin2", "Twin", "10:9c:70:00:00:03", "", "")

	table := opened(t, "", devices)

	for _, tc := range []struct{ name, mac, ip, want string }{
		{"the connected printer wins a MAC", "10:9c:70:00:00:01", "10.0.0.1", "new"},
		{"the connected printer wins an address", "aa:aa:aa:aa:aa:aa", "192.168.1.40", "new"},
		{"a printer that is away is matched by its MAC", "10:9c:70:00:00:02", "10.0.0.1", "away"},
		{"the address of a printer that is away matches nothing", "bb:bb:bb:bb:bb:bb", "192.168.1.41", ""},
		{"a MAC two printers that are away report matches neither", "10:9c:70:00:00:03", "10.0.0.1", ""},
	} {
		if got := table.Resolve(tc.mac, tc.ip, true); got != tc.want {
			t.Errorf("%s: matched %q, want %q", tc.name, got, tc.want)
		}
	}
}

// A MAC or an address two connected printers report matches neither of them.
func TestANetworkTwoPrintersReportMatchesNeither(t *testing.T) {
	t.Parallel()

	devices := &hostfake.Devices{}
	devices.Add("one", "One", "10:9c:70:00:00:01", "192.168.1.50", "")
	devices.Add("two", "Two", "10:9c:70:00:00:01", "192.168.1.50", "")

	table := opened(t, "", devices)

	if got := table.Resolve("10:9c:70:00:00:01", "192.168.1.50", true); got != "" {
		t.Errorf("a MAC and an address two printers report matched %q", got)
	}
}

// An alias names a MAC the same way each time, and never shows it.
func TestAnAliasHidesTheMAC(t *testing.T) {
	t.Parallel()

	table := opened(t, "", fleet())
	other := opened(t, "", fleet())

	first := table.Alias("10:9c:70:2a:bb:cd")

	if !strings.HasPrefix(first, "unassigned-") || strings.Contains(first, ":") {
		t.Errorf("alias = %q", first)
	}

	if table.Alias("10:9c:70:2a:bb:cd") != first {
		t.Error("one MAC got two aliases")
	}

	if table.Alias("10:9c:70:2a:bb:ce") == first {
		t.Error("two MACs got one alias")
	}

	if other.Alias("10:9c:70:2a:bb:cd") == first {
		t.Error("two processes gave one MAC the same alias, so it can be looked up")
	}
}

// Visible answers what one person may see, and Covers whether that is the
// whole fleet the module reads.
func TestAPersonSeesTheirOwnPrinters(t *testing.T) {
	t.Parallel()

	devices := fleet()
	devices.Sees("user:bob", "mk4")

	table, err := sources.Open("", devices, now)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	all, err := sources.Visible(t.Context(), devices, "user:alice")
	if err != nil || !all["mk4"] || !all["xl"] {
		t.Fatalf("alice sees %v, %v", all, err)
	}

	if table.Covers(all) {
		t.Error("the table covers a fleet it has not read")
	}

	if err = table.Refresh(t.Context()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	some, err := sources.Visible(t.Context(), devices, "user:bob")
	if err != nil || len(some) != 1 || !some["mk4"] {
		t.Fatalf("bob sees %v, %v", some, err)
	}

	if !table.Covers(all) || table.Covers(some) {
		t.Errorf("covers alice %v, bob %v", table.Covers(all), table.Covers(some))
	}
}

// A read of the fleet that found no device covers nobody, so nobody sees the
// records matched to no printer while the module knows no printer.
func TestAnEmptyFleetIsCoveredByNobody(t *testing.T) {
	t.Parallel()

	table := opened(t, "", &hostfake.Devices{})

	if table.Covers(map[string]bool{}) {
		t.Error("an empty fleet is covered by a person who sees no printer")
	}
}
