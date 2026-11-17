package manifest_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// withNeeds is the smallest manifest carrying a needs block.
func withNeeds(needs string) string {
	return `{"name": "buddy-debug-logs", "kind": "module","license":"MIT", "needs": ` + needs + `}`
}

func TestANeedsBlockReadsPortsAndTheDataDirectory(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withNeeds(`{
		"listen": [
			{"name": "syslog", "udp": 13514, "why": "printer log lines"},
			{"name": "api", "tcp": 8080, "why": "a protocol of its own"}
		],
		"data": true
	}`)), decode)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	want := []manifest.Listen{
		{Name: "syslog", Network: manifest.NetworkUDP, Port: 13514, Why: "printer log lines"},
		{Name: "api", Network: manifest.NetworkTCP, Port: 8080, Why: "a protocol of its own"},
	}

	if len(held.Needs.Listen) != len(want) {
		t.Fatalf("listen = %+v, want %+v", held.Needs.Listen, want)
	}

	for i := range want {
		if held.Needs.Listen[i] != want[i] {
			t.Errorf("listen[%d] = %+v, want %+v", i, held.Needs.Listen[i], want[i])
		}
	}

	if !held.Needs.Data {
		t.Error("data was asked for and not read")
	}

	if got := want[0].Request(); got != "listen udp 13514 as syslog (printer log lines)" {
		t.Errorf("request line = %q", got)
	}
}

// A database of its own is a switch, like the data directory (ADR-20261021-bqc rule 1).
func TestANeedsBlockAsksForADatabase(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(withNeeds(`{"database": true}`)), decode)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !held.Needs.Database || held.Needs.Data {
		t.Errorf("needs = %+v, want a database and no data directory", held.Needs)
	}
}

func TestANeedsBlockRecommendsAMemoryBudget(t *testing.T) {
	t.Parallel()

	for needs, want := range map[string]int{
		`{"memory_mib": 192}`:  192,
		`{"memory_mib": 0}`:    0,
		`{"memory_mib": 16}`:   16,
		`{"memory_mib": 8192}`: 8192,
		`{"data": true}`:       0,
	} {
		held, err := manifest.Parse("module.yaml", []byte(withNeeds(needs)), decode)
		if err != nil {
			t.Fatalf("parse %s: %v", needs, err)
		}

		if held.Needs.MemoryMiB != want {
			t.Errorf("%s: MemoryMiB = %d, want %d", needs, held.Needs.MemoryMiB, want)
		}
	}
}

func TestAManifestWithoutNeedsAsksForNothing(t *testing.T) {
	t.Parallel()

	held, err := manifest.Parse("module.yaml", []byte(`{"name": "x", "kind": "module","license":"MIT"}`), decode)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(held.Needs.Listen) != 0 || held.Needs.Data || held.Needs.Database {
		t.Errorf("needs = %+v, want none", held.Needs)
	}
}

func TestANeedsBlockIsRefused(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		needs string
		says  string
	}{
		{"an unknown key", `{"dial": []}`, "dial"},
		{"a port of zero", `{"listen": [{"name": "a", "udp": 0, "why": "x"}]}`, "expected 1 to 65535"},
		{"a port too high", `{"listen": [{"name": "a", "tcp": 70000, "why": "x"}]}`, "expected 1 to 65535"},
		{"a port that is not whole", `{"listen": [{"name": "a", "udp": 1.5, "why": "x"}]}`, "whole number"},
		{"both networks", `{"listen": [{"name": "a", "udp": 1, "tcp": 1, "why": "x"}]}`, "both udp and tcp"},
		{"no network", `{"listen": [{"name": "a", "why": "x"}]}`, "names no network"},
		{"no reason", `{"listen": [{"name": "a", "udp": 1}]}`, "why is empty"},
		{"a name in capitals", `{"listen": [{"name": "Syslog", "udp": 1, "why": "x"}]}`, "lower case"},
		{"a name twice", `{"listen": [{"name": "a", "udp": 1, "why": "x"}, {"name": "a", "udp": 2, "why": "y"}]}`, "twice"},
		{"a port twice", `{"listen": [{"name": "a", "udp": 1, "why": "x"}, {"name": "b", "udp": 1, "why": "y"}]}`, "udp/1 twice"},
		{"an unknown entry key", `{"listen": [{"name": "a", "udp": 1, "why": "x", "host": "y"}]}`, "host"},
		{"a memory budget too small", `{"memory_mib": 8}`,
			"needs.memory_mib is 8, expected 16 to 8192, or no key for the server default"},
		{"a memory budget too large", `{"memory_mib": 9000}`,
			"needs.memory_mib is 9000, expected 16 to 8192, or no key for the server default"},
		{"a memory budget that is text", `{"memory_mib": "a lot"}`, "whole number"},
		{"data that is not a switch", `{"data": "yes"}`, "true or false"},
		{"a database that is not a switch", `{"database": "sqlite"}`, "true or false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := manifest.Parse("module.yaml", []byte(withNeeds(tc.needs)), decode)
			if !errors.Is(err, manifest.ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}

			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("err = %q, want it to say %q", err, tc.says)
			}
		})
	}
}

func TestAListenNameIsHeldToOneSpelling(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"syslog":                true,
		"metrics-2":             true,
		"":                      false,
		"2fast":                 false,
		"-a":                    false,
		"a_b":                   false,
		"a b":                   false,
		strings.Repeat("a", 33): false,
	} {
		if got := manifest.IsListenName(name); got != want {
			t.Errorf("IsListenName(%q) = %v, want %v", name, got, want)
		}
	}
}
