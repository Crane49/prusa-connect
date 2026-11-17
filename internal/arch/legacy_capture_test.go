package arch_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The captures of what the retired server did. A capture is a committed data
// file with a test that fails on divergence. Capturing a behaviour was the
// precondition for deleting the code that held it, and an enumerable thing was
// extracted into a file like these and never transcribed into prose.
//
// Every row in every file under testdata/ here came off a running retired
// server rather than out of its source. The header of each file names the run
// that produced it and the command that reproduces it. Reading the source is
// what these gates then do, so a capture and its authority never share an
// origin.
//
// The gate dies with the code it holds, and that is the design. A capture is
// how a behaviour survives the deletion; the divergence test is how the capture
// is kept honest while the behaviour is still there to compare against.

// captureDirectory is where every fixture on this page lives.
const captureDirectory = "testdata"

// legacyCaptures is every capture file here, against the number of columns each
// row carries, where its rows came from, and the gate that still compares it
// against something. A file with no entry is a file no gate reads, which is the
// way a capture dies quietly.
//
// Three of these have no gate left. Their second source was Go source under
// legacy/, the gate held that source equal to the rows, and neither side had
// anything to do with the core. Reading the quarantine to hold a fixture
// against itself buys nothing and dies with the quarantine, so the reads went
// and the rows stayed. What is left for those three is the capture alone.
var legacyCaptures = map[string]captureFile{
	"legacy-metric-names.tsv": {
		columns: 6,
		source:  "legacy/platform/observability/metrics.go",
		holds:   "the metric names a dashboard was built on",
	},
	"legacy-account-surface.tsv": {
		columns: 8,
		source:  "internal/arch/testdata/retired-openapi3.json",
		gate:    "TestEveryPublishedSurfaceOperationIsCaptured",
		holds:   "the human account and token wire surface",
	},
	"legacy-label-surface.tsv": {
		columns: 8,
		source:  "internal/arch/testdata/retired-openapi3.json",
		gate:    "TestEveryPublishedSurfaceOperationIsCaptured",
		holds:   "the label wire surface and the store behind it",
	},
	"legacy-queue-retention.tsv": {
		columns: 4,
		source:  "legacy/printer/queue/sqlite.go",
		holds:   "what the durable command queue keeps and what it sweeps",
	},
	"legacy-listener-topology.tsv": {
		columns: 6,
		source:  "legacy/app/config.go",
		holds:   "the sockets the retired server binds and the keys that place them",
	},
	"legacy-sse-event-types.tsv": {
		columns: 2,
		source:  "legacy/printer/state/broadcaster.go",
		gate:    "TestTheSSEDocumentDescribesEveryEventTypeTheServerPublishes",
		holds:   "the event types a client subscribes on",
	},
}

// captureFile is one fixture's shape, where its rows were read from, and the
// gate that compares it against a second file. An empty gate is a capture with
// no second source left.
type captureFile struct {
	columns int
	source  string
	gate    string
	holds   string
}

// captureRows reads one fixture into rows of cells. A line opening with # is a
// comment and a blank line is skipped, so the header of each file carries the
// run that produced it without costing a row.
func captureRows(t *testing.T, name string) [][]string {
	t.Helper()

	path := filepath.Join(captureDirectory, name)

	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var rows [][]string

	for raw := range strings.SplitSeq(string(content), "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		rows = append(rows, strings.Split(line, "\t"))
	}

	return rows
}

// TestEveryLegacyCaptureIsWellFormed is the floor under every gate below. Two
// empty sets agree, so a fixture that stopped parsing would otherwise turn each
// divergence test green.
func TestEveryLegacyCaptureIsWellFormed(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	for name, shape := range legacyCaptures {
		rows := captureRows(t, name)

		if len(rows) == 0 {
			t.Errorf("%s holds no row, so every gate reading it compares two empty sets", name)

			continue
		}

		for index, row := range rows {
			if len(row) != shape.columns {
				t.Errorf("%s row %d has %d cells and the format is %d, so a tab is missing or spare",
					name, index+1, len(row), shape.columns)

				continue
			}

			for cell, value := range row {
				if strings.TrimSpace(value) == "" {
					t.Errorf("%s row %d cell %d is empty. Write - where a column does not apply",
						name, index+1, cell+1)
				}
			}
		}

		if shape.gate == "" {
			t.Logf("%s holds %s and no gate compares it any more, so what is left is the capture alone",
				name, shape.holds)
		}

		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(shape.source))); err != nil {
			t.Logf("%s was read from %s and that file is gone", name, shape.source)
		}
	}
}

// TestEveryCaptureUnderTestdataIsListed refuses a fixture nothing reads. A file
// that lost its gate looks exactly like one that never had a gate, and both
// stop failing on drift.
func TestEveryCaptureUnderTestdataIsListed(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(captureDirectory)
	if err != nil {
		t.Fatalf("read %s: %v", captureDirectory, err)
	}

	var unlisted []string

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tsv") {
			continue
		}

		if _, ok := legacyCaptures[entry.Name()]; !ok {
			unlisted = append(unlisted, entry.Name())
		}
	}

	sort.Strings(unlisted)

	for _, name := range unlisted {
		t.Errorf("%s/%s is not in legacyCaptures, so no gate compares it against anything",
			captureDirectory, name)
	}

	for name := range legacyCaptures {
		if _, err = os.Stat(filepath.Join(captureDirectory, name)); err != nil {
			t.Errorf("legacyCaptures names %s and the file is not there: %v", name, err)
		}
	}
}
