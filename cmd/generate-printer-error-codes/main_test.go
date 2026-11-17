package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const testCommit = "0123456789abcdef0123456789abcdef01234567"

// rendered is what render writes for the entries, as the lines of the file.
func rendered(t *testing.T, entries ...sourceEntry) map[string]string {
	t.Helper()

	data, err := render(testCommit, entries)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	lines := map[string]string{}
	if decodeErr := json.Unmarshal(data, &lines); decodeErr != nil {
		t.Fatalf("the generated file is not a JSON object of strings: %v", decodeErr)
	}

	return lines
}

func TestParseSourceReadsTheEntriesUnderErrors(t *testing.T) {
	t.Parallel()

	entries, err := parseSource("buddy.yaml", []byte(
		"Errors:\n  - code: \"XX807\"\n    title: \"File error\"\n    text: \"File error\"\n    printers: [MK4]\n",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(entries) != 1 || entries[0].Code != "XX807" || entries[0].Printers[0] != "MK4" {
		t.Errorf("read %+v", entries)
	}

	if _, emptyErr := parseSource("empty.yaml", []byte("Errors: []\n")); emptyErr == nil {
		t.Error("a file with no error code was accepted")
	}
}

func TestRenderKeysTheWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []sourceEntry
		want    map[string]string
		absent  []string
	}{
		{
			name: "an XX code alone keeps XX, whatever its list",
			entries: []sourceEntry{
				{Code: "XX807", Printers: []string{"MK4"}, Title: "File error", Text: "Read it again."},
			},
			want: map[string]string{
				"printerCode.xbuddy.XX807.title": "File error",
				"printerCode.xbuddy.XX807.text":  "Read it again.",
			},
			absent: []string{"printerCode.xbuddy.13807.title"},
		},
		{
			name: "the entry for the most printers keeps XX, the others name their printers",
			entries: []sourceEntry{
				{Code: "XX204", Printers: []string{"XL"}, Title: "Extruder", Text: "x"},
				{Code: "XX204", Printers: []string{"MINI", "MK4"}, Title: "Hotend", Text: "h"},
				{Code: "XX204", Printers: []string{"mk3.5"}, Title: "Bed", Text: "b"},
			},
			want: map[string]string{
				// The XL's firmware runs on the XLP, and the MK3.5's on the MK3.5S.
				"printerCode.xbuddy.XX204.p17_41.title": "Extruder",
				"printerCode.xbuddy.XX204.title":        "Hotend",
				"printerCode.xbuddy.XX204.p23_28.title": "Bed",
			},
			absent: []string{
				"printerCode.xbuddy.17204.title", "printerCode.xbuddy.41204.title", "printerCode.xbuddy.12204.title",
			},
		},
		{
			name: "the first of two entries for as many printers keeps XX",
			entries: []sourceEntry{
				{Code: "XX204", Printers: []string{"MINI"}, Title: "First", Text: "f"},
				{Code: "XX204", Printers: []string{"IX"}, Title: "Second", Text: "s"},
			},
			want: map[string]string{
				"printerCode.xbuddy.XX204.title":     "First",
				"printerCode.xbuddy.XX204.p16.title": "Second",
			},
		},
		{
			name: "a numbered code keeps its number beside an XX one",
			entries: []sourceEntry{
				{Code: "04101", Title: "Finda", Text: "f"},
				{Code: "XX101", Title: "Stuck", Text: "s"},
			},
			want: map[string]string{
				"printerCode.xbuddy.04101.title": "Finda",
				"printerCode.xbuddy.XX101.title": "Stuck",
			},
		},
		{
			name: "a text the firmware fills in is left out",
			entries: []sourceEntry{
				{Code: "XX103", Title: "Dock", Text: "Dock %d is at %.3f"},
				{Code: "XX104", Title: "Half", Text: "50% done"},
			},
			want: map[string]string{
				"printerCode.xbuddy.XX103.title": "Dock",
				"printerCode.xbuddy.XX104.text":  "50% done",
			},
			absent: []string{"printerCode.xbuddy.XX103.text"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			lines := rendered(t, test.entries...)

			if lines[commitKey] != testCommit {
				t.Errorf("the file records commit %q", lines[commitKey])
			}

			for key, want := range test.want {
				if got := lines[key]; got != want {
					t.Errorf("%s is %q, want %q", key, got, want)
				}
			}

			for _, key := range test.absent {
				if got, held := lines[key]; held {
					t.Errorf("%s is %q, want it left out", key, got)
				}
			}
		})
	}
}

func TestRenderRefusesWhatItCannotKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []sourceEntry
		want    string
	}{
		{
			name: "two entries for one printer and one code",
			entries: []sourceEntry{
				{Code: "XX807", Title: "One"},
				{Code: "13807", Title: "Two"},
			},
			want: "defined twice",
		},
		{
			name:    "a printer with no number",
			entries: []sourceEntry{{Code: "XX807", Printers: []string{"MK9"}, Title: "T"}},
			want:    "has no number",
		},
		{
			name:    "a code of another shape",
			entries: []sourceEntry{{Code: "X807", Title: "T"}},
			want:    "is not two digits or XX",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := render(testCommit, test.entries)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("render answered %v, want an error saying %q", err, test.want)
			}
		})
	}
}
