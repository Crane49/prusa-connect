package files_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/fractal/files"
)

// hqEntry is the files entry of the example in fractal-files.md, Join Data.
func hqEntry() files.JoinEntry {
	return files.JoinEntry{Version: 1, HTTP: &files.HTTPEntry{Version: 1, BaseURL: "https://hq.example/fractal"}}
}

func TestJoinEntryMarshal(t *testing.T) {
	t.Parallel()
	raw, err := hqEntry().Marshal()
	want := `{"version":1,"http":{"version":1,"baseUrl":"https://hq.example/fractal"}}`
	if err != nil || string(raw) != want {
		t.Fatalf("Marshal = %s, %v, want %s", raw, err, want)
	}
	back, err := files.ParseJoinEntry(raw, false)
	if err != nil || !reflect.DeepEqual(back, hqEntry()) {
		t.Fatalf("ParseJoinEntry(%s) = %+v, %v, want %+v", raw, back, err, hqEntry())
	}
}

func TestParseJoinEntryIgnoresUnknownBinding(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"version":1,"quic":{"version":3},` +
		`"http":{"version":1,"baseUrl":"https://hq.example/fractal","extra":true}}`)
	got, err := files.ParseJoinEntry(raw, false)
	if err != nil || !reflect.DeepEqual(got, hqEntry()) {
		t.Fatalf("ParseJoinEntry = %+v, %v, want %+v", got, err, hqEntry())
	}
}

func TestParseJoinEntryRefuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		is   error
	}{
		{name: "empty", raw: ``, is: files.ErrMalformed},
		{name: "not an object", raw: `[1]`, is: files.ErrMalformed},
		{name: "negative version", raw: `{"version":-1}`, is: files.ErrMalformed},
		{
			name: "no version",
			raw:  `{"http":{"version":1,"baseUrl":"https://hq.example"}}`,
			is:   files.ErrUnsupportedVersion,
		},
		{
			name: "version 2",
			raw:  `{"version":2,"http":{"version":1,"baseUrl":"https://hq.example"}}`,
			is:   files.ErrUnsupportedVersion,
		},
		{name: "no http part", raw: `{"version":1}`, is: files.ErrUnsupportedVersion},
		{name: "null http part", raw: `{"version":1,"http":null}`, is: files.ErrUnsupportedVersion},
		{
			name: "http version 2",
			raw:  `{"version":1,"http":{"version":2,"baseUrl":"https://hq.example"}}`,
			is:   files.ErrUnsupportedVersion,
		},
		{
			name: "cleartext",
			raw:  `{"version":1,"http":{"version":1,"baseUrl":"http://hq.example"}}`,
			is:   files.ErrMalformed,
		},
		{name: "no base URL", raw: `{"version":1,"http":{"version":1}}`, is: files.ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := files.ParseJoinEntry(json.RawMessage(tc.raw), false); !errors.Is(err, tc.is) {
				t.Fatalf("ParseJoinEntry(%s) = %v, want %v", tc.raw, err, tc.is)
			}
		})
	}
}

func TestParseJoinEntryAllowsCleartext(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"version":1,"http":{"version":1,"baseUrl":"http://hq.lan:8443/fractal"}}`)
	got, err := files.ParseJoinEntry(raw, true)
	want := files.JoinEntry{Version: 1, HTTP: &files.HTTPEntry{Version: 1, BaseURL: "http://hq.lan:8443/fractal"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseJoinEntry = %+v, %v, want %+v", got, err, want)
	}
}

func TestJoinEntryMarshalRefuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		entry files.JoinEntry
		is    error
	}{
		{name: "no http part", entry: files.JoinEntry{Version: 1}, is: files.ErrUnsupportedVersion},
		{name: "version 0", entry: files.JoinEntry{HTTP: hqEntry().HTTP}, is: files.ErrUnsupportedVersion},
		{
			name:  "query",
			entry: files.JoinEntry{Version: 1, HTTP: &files.HTTPEntry{Version: 1, BaseURL: "https://hq.example/?a=1"}},
			is:    files.ErrMalformed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := tc.entry.Marshal(); !errors.Is(err, tc.is) {
				t.Fatalf("Marshal(%+v) = %v, want %v", tc.entry, err, tc.is)
			}
		})
	}
}

func TestCheckBaseURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw       string
		cleartext bool
		ok        bool
	}{
		{raw: "https://hq.example/fractal", ok: true},
		{raw: "https://hq.example:8443", ok: true},
		{raw: "HTTPS://hq.example/fractal", ok: true},
		{raw: "https://[2001:db8::1]:8443/fractal", ok: true},
		{raw: "http://hq.example/fractal", cleartext: true, ok: true},
		{raw: "http://hq.example/fractal"},
		{raw: "ftp://hq.example/fractal", cleartext: true},
		{raw: "https://hq.example/fractal?a=1"},
		{raw: "https://hq.example/fractal?"},
		{raw: "https://hq.example/fractal#top"},
		{raw: "https://hq.example/fractal#"},
		{raw: "/fractal"},
		{raw: "hq.example/fractal"},
		{raw: "https:///fractal"},
		{raw: "https://:8443/fractal"},
		{raw: "https://u:p@hq.example/fractal"},
		{raw: "https://u@hq.example/fractal"},
		{raw: "https:hq.example"},
		{raw: ""},
		{raw: "https://hq.example/%zz"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			err := files.CheckBaseURL(tc.raw, tc.cleartext)
			if tc.ok != (err == nil) || err != nil && !errors.Is(err, files.ErrMalformed) {
				t.Fatalf("CheckBaseURL(%q, %v) = %v, want ok %v or ErrMalformed", tc.raw, tc.cleartext, err, tc.ok)
			}
		})
	}
}
