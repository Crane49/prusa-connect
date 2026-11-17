package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/fractal"
	fractalfiles "github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// sharing is the smallest settings of a server that runs the file extension.
const sharing = `{
	"node_id": "hq",
	"data_dir": "/var/lib/prusa-connect/fractal",
	"dedup_window": "5m",
	"files": {"base_url": "https://hq.example:8443/fractal"}
}`

// TestFilesBlockDefaults: a files block with only base_url takes the default
// of every other key.
func TestFilesBlockDefaults(t *testing.T) {
	t.Parallel()

	held, err := ParseConfig([]byte(sharing))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}

	want := FilesConfig{
		BaseURL:        "https://hq.example:8443/fractal",
		Rescan:         60 * time.Second,
		RelayTimeout:   30 * time.Second,
		ProofTolerance: 60 * time.Second,
		MaxResumes:     5,
		FetchAttempts:  3,
		StartDeadline:  10 * time.Minute,
		CacheMaxBytes:  10 << 30,
	}
	if held.Files == nil || *held.Files != want {
		t.Errorf("the files block is %+v, want %+v", held.Files, want)
	}
}

// TestFilesBlockRefusals: ParseConfig refuses a files block the module cannot
// run with, and names the key.
func TestFilesBlockRefusals(t *testing.T) {
	t.Parallel()

	for _, row := range []struct {
		name  string
		files string
		want  error
	}{
		{name: "no base_url", files: `{}`, want: ErrNoBaseURL},
		{name: "a query", files: `{"base_url":"https://hq.example/f?x=1"}`, want: ErrBadBaseURL},
		{name: "cleartext", files: `{"base_url":"http://hq.example/f"}`, want: ErrBadBaseURL},
		{name: "a bad duration", files: `{"base_url":"https://hq.example/f","rescan":"soon"}`, want: ErrBadDuration},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			record := `{"node_id":"hq","data_dir":"/d","dedup_window":"5m","files":` + row.files + `}`
			if _, err := ParseConfig([]byte(record)); !errors.Is(err, row.want) {
				t.Errorf("ParseConfig returned %v, want %v", err, row.want)
			}
		})
	}
}

// TestCleartextBaseURLWhenAllowed: http passes when the operator turned TLS
// off with allow_cleartext_parent.
func TestCleartextBaseURLWhenAllowed(t *testing.T) {
	t.Parallel()

	record := `{"node_id":"hq","data_dir":"/d","dedup_window":"5m",
		"files":{"base_url":"http://hq.example/f","allow_cleartext_parent":true}}`
	if _, err := ParseConfig([]byte(record)); err != nil {
		t.Errorf("ParseConfig refused a cleartext base_url that was allowed: %v", err)
	}
}

// TestFilesEntryInEveryGrant: the transport offers the files entry with the
// base URL and runs the extension as a joiner.
func TestFilesEntryInEveryGrant(t *testing.T) {
	t.Parallel()

	held, err := ParseConfig([]byte(sharing))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}

	cfg, err := held.Binding()
	if err != nil {
		t.Fatalf("build the transport configuration: %v", err)
	}

	entry, err := fractalfiles.ParseJoinEntry(cfg.Extensions[fractalfiles.ExtensionName], false)
	if err != nil {
		t.Fatalf("read the offered entry: %v", err)
	}

	got := struct {
		Entry   fractalfiles.JoinEntry
		Offered int
		Runs    []fractal.Identifier
	}{entry, len(cfg.Extensions), cfg.JoinExtensions}
	want := struct {
		Entry   fractalfiles.JoinEntry
		Offered int
		Runs    []fractal.Identifier
	}{
		Entry: fractalfiles.JoinEntry{
			Version: 1,
			HTTP:    &fractalfiles.HTTPEntry{Version: 1, BaseURL: "https://hq.example:8443/fractal"},
		},
		Offered: 1,
		Runs:    []fractal.Identifier{fractalfiles.ExtensionName},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the transport configuration carries %+v, want %+v", got, want)
	}
}

// TestGrantCheck: the check refuses a grant with no files entry, an entry of
// another version, and an http parent unless cleartext is allowed.
func TestGrantCheck(t *testing.T) {
	t.Parallel()

	entry := func(text string) map[fractal.Identifier]json.RawMessage {
		return map[fractal.Identifier]json.RawMessage{fractalfiles.ExtensionName: json.RawMessage(text)}
	}

	for _, row := range []struct {
		name      string
		cleartext bool
		grant     map[fractal.Identifier]json.RawMessage
		refused   bool
	}{
		{name: "no entry", grant: nil, refused: true},
		{name: "version 2", grant: entry(`{"version":2,"http":{"version":1,"baseUrl":"https://hq/f"}}`), refused: true},
		{name: "no http part", grant: entry(`{"version":1}`), refused: true},
		{name: "http parent", grant: entry(`{"version":1,"http":{"version":1,"baseUrl":"http://hq/f"}}`), refused: true},
		{
			name: "http parent allowed", cleartext: true,
			grant: entry(`{"version":1,"http":{"version":1,"baseUrl":"http://hq/f"}}`),
		},
		{name: "https parent", grant: entry(`{"version":1,"http":{"version":1,"baseUrl":"https://hq/f"}}`)},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()

			cfg := Config{Files: &FilesConfig{BaseURL: "https://brno/f", AllowCleartextParent: row.cleartext}}

			binding, err := cfg.Binding()
			if err != nil {
				t.Fatalf("build the transport configuration: %v", err)
			}

			if refused := binding.CheckExtensions(row.grant) != nil; refused != row.refused {
				t.Errorf("the check refused the grant: %t, want %t", refused, row.refused)
			}
		})
	}
}

// TestNoFilesBlockNoExtension: without a files block the transport offers no
// extension and checks no grant.
func TestNoFilesBlockNoExtension(t *testing.T) {
	t.Parallel()

	held, err := ParseConfig([]byte(smallest))
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}

	cfg, err := held.Binding()
	if err != nil {
		t.Fatalf("build the transport configuration: %v", err)
	}

	if cfg.Extensions != nil || cfg.JoinExtensions != nil || cfg.CheckExtensions != nil {
		t.Errorf("the transport runs an extension the settings did not turn on: %+v %v",
			cfg.Extensions, cfg.JoinExtensions)
	}
}
