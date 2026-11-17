package arch_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/wire"
	"github.com/prusa3d/prusa-connect/sdk/wire/wiretest"
)

// The plugin wire protocol has two published forms and one source. The prose
// specification is written by hand, because a byte layout needs an argument
// around it that no generator writes. The artefact beside it is generated from
// sdk/wire and sdk/wire/wiretest.
//
// A hand-written specification drifts from the code it specifies. These gates
// are what stop that: every number the page states is read out of the page and
// compared against the constant it names, so a size changed in Go and not on
// the page fails here rather than in somebody's implementation.
//
// What they cannot check is the prose. Whether the page explains the wire is a
// judgement over two texts, and a reviewer holds that.
const (
	wirePagePath     = "docs/src/content/docs/plugin-wire-protocol.md"
	wireArtefactPath = "docs/wire/plugin-wire-protocol.json"
)

// wirePage reads the specification.
func wirePage(t *testing.T) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(wirePagePath)))
	if err != nil {
		t.Fatalf("read %s: %v", wirePagePath, err)
	}

	return string(body)
}

// wireTableRows returns every markdown table row on the page, as cells with
// their backticks and their surrounding space removed. A header row and a
// separator row are dropped, because neither carries a value.
func wireTableRows(page string) [][]string {
	var rows [][]string

	for line := range strings.SplitSeq(page, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}

		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i, cell := range cells {
			cells[i] = strings.Trim(strings.TrimSpace(cell), "`")
		}

		if len(cells) > 0 && strings.Trim(cells[0], "-: ") == "" {
			continue
		}

		rows = append(rows, cells)
	}

	return rows
}

// wireCellFor finds the row whose first cell is label and returns the cell at
// index. A label the page does not carry is a failure, which is what makes a
// page that lost its table fail rather than pass over nothing.
func wireCellFor(t *testing.T, rows [][]string, label string, index int) string {
	t.Helper()

	for _, row := range rows {
		if row[0] == label && len(row) > index {
			return row[index]
		}
	}

	t.Fatalf("%s carries no table row for %q, so nothing on the page states it", wirePagePath, label)

	return ""
}

// wireLeadingNumber reads the number a cell opens with, so that "1048576
// bytes" and "1048576" are both read.
func wireLeadingNumber(t *testing.T, cell string) int {
	t.Helper()

	fields := strings.Fields(cell)
	if len(fields) == 0 {
		t.Fatalf("cell %q carries no number", cell)
	}

	value, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("cell %q does not open with a number: %v", cell, err)
	}

	return value
}

// TestThePublishedWireSizesAreTheOnesTheCodeHolds is the gate on the numbers a
// foreign implementation copies off the page.
func TestThePublishedWireSizesAreTheOnesTheCodeHolds(t *testing.T) {
	t.Parallel()

	rows := wireTableRows(wirePage(t))

	for _, size := range []struct {
		label string
		want  int
	}{
		{label: "header", want: wire.HeaderBytes},
		{label: "default payload cap", want: wire.DefaultMaxPayloadBytes},
		{label: "data payload", want: wire.ChunkBytes},
		{label: "reset reason", want: wire.MaxResetBytes},
		{label: "handshake line", want: wire.MaxHandshakeBytes},
	} {
		if got := wireLeadingNumber(t, wireCellFor(t, rows, size.label, 1)); got != size.want {
			t.Errorf("%s states %s is %d, and sdk/wire holds %d", wirePagePath, size.label, got, size.want)
		}
	}

	if got := wireCellFor(t, rows, "credit payload", 1); !strings.Contains(got, strconv.Itoa(wire.CreditBytes)) {
		t.Errorf("%s states the credit payload as %q, and sdk/wire holds %d bytes",
			wirePagePath, got, wire.CreditBytes)
	}
}

// TestThePublishedWireKindsAreTheOnesTheCodeHolds holds the closed set on the
// page against the closed set in Go, in both directions.
func TestThePublishedWireKindsAreTheOnesTheCodeHolds(t *testing.T) {
	t.Parallel()

	kinds := wire.Kinds()
	if len(kinds) != 5 {
		t.Fatalf("sdk/wire declares %d kinds, and wire major %d has five", len(kinds), wire.ProtocolMajor)
	}

	rows := wireTableRows(wirePage(t))

	published := map[string]string{}

	for _, row := range rows {
		if len(row) < 2 {
			continue
		}

		if _, err := strconv.Atoi(row[0]); err != nil {
			continue
		}

		if _, taken := published[row[0]]; !taken {
			published[row[0]] = row[1]
		}
	}

	for _, kind := range kinds {
		code := strconv.Itoa(int(kind.Kind))

		name, found := published[code]
		if !found {
			t.Errorf("%s names no kind with code %s, and sdk/wire declares %s", wirePagePath, code, kind.Name)

			continue
		}

		if name != kind.Name {
			t.Errorf("%s calls kind %s %q, and sdk/wire calls it %q", wirePagePath, code, name, kind.Name)
		}
	}
}

// TestThePublishedWireRefusalReasonsAreTheOnesTheVectorsUse holds the reason
// names on the page against the ones the vectors publish. A foreign
// implementation reports these names, so a name only one side knows is a
// conformance run that cannot be compared.
func TestThePublishedWireRefusalReasonsAreTheOnesTheVectorsUse(t *testing.T) {
	t.Parallel()

	page := wirePage(t)

	frameReasons := []wiretest.Reason{}

	for _, vector := range wiretest.Refusals() {
		frameReasons = append(frameReasons, vector.Reason)
	}

	if len(frameReasons) == 0 {
		t.Fatal("sdk/wire/wiretest publishes no refusal vector, so this gate would check nothing")
	}

	for _, reason := range frameReasons {
		if !strings.Contains(page, "`"+string(reason)+"`") {
			t.Errorf("%s does not name the refusal reason %s, which a vector reports", wirePagePath, reason)
		}
	}
}

// TestThePublishedWireVectorCountsMatchTheArtefact holds the counts the page
// quotes against the file it quotes them from.
func TestThePublishedWireVectorCountsMatchTheArtefact(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(wireArtefactPath)))
	if err != nil {
		t.Fatalf("read %s: %v", wireArtefactPath, err)
	}

	var artefact struct {
		Frames       []json.RawMessage `json:"frames"`
		Refusals     []json.RawMessage `json:"refusals"`
		Handshakes   []json.RawMessage `json:"handshakes"`
		Negotiations []json.RawMessage `json:"negotiations"`
	}

	if decoded := json.Unmarshal(body, &artefact); decoded != nil {
		t.Fatalf("decode %s: %v", wireArtefactPath, decoded)
	}

	sections := []struct {
		name  string
		count int
	}{
		{name: "frames", count: len(artefact.Frames)},
		{name: "refusals", count: len(artefact.Refusals)},
		{name: "handshakes", count: len(artefact.Handshakes)},
		{name: "negotiations", count: len(artefact.Negotiations)},
	}

	total := 0
	rows := wireTableRows(wirePage(t))

	for _, section := range sections {
		if section.count == 0 {
			t.Fatalf("%s holds no %s, so this gate would check nothing", wireArtefactPath, section.name)
		}

		total += section.count

		if got := wireLeadingNumber(t, wireCellFor(t, rows, section.name, 1)); got != section.count {
			t.Errorf("%s says the artefact holds %d %s entries, and it holds %d",
				wirePagePath, got, section.name, section.count)
		}
	}

	if page := wirePage(t); !strings.Contains(page, fmt.Sprintf("%d vectors", total)) {
		t.Errorf("%s does not say the artefact holds %d vectors, which is what it holds",
			wirePagePath, total)
	}
}
