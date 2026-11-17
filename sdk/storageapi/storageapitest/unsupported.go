package storageapitest

// The subtests for what a provider does not serve. A store that declares
// neither half of the mandatory surface still has to refuse every call rather
// than answer one with a zero value, and a store that presigns has to resolve
// the URL it says it can.

import (
	"net/url"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// A provider that does not declare Browse must report failure from every read
// method, and must not return a usable value alongside the error.
func testBrowseUnsupported(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	entries, err := p.List(ctx, "")
	if err == nil {
		t.Error("List must return an error when Browse is not declared")
	}
	if entries != nil {
		t.Errorf("List must return a nil slice with its error, got %+v", entries)
	}

	info, err := p.Stat(ctx, "a.gcode")
	if err == nil {
		t.Error("Stat must return an error when Browse is not declared")
	}
	if info != (storageapi.FileInfo{}) {
		t.Errorf("Stat must return a zero FileInfo with its error, got %+v", info)
	}

	rc, err := p.Open(ctx, "a.gcode")
	if err == nil {
		t.Error("Open must return an error when Browse is not declared")
	}
	if rc != nil {
		_ = rc.Close()
		t.Error("Open must return a nil ReadCloser with its error")
	}
}

// A provider that does not declare Write must report failure from every mutating
// method rather than silently accepting the call.
func testWriteUnsupported(t *testing.T, p storageapi.Provider) {
	ctx := t.Context()
	w, err := p.Create(ctx, "a.gcode")
	if err == nil {
		t.Error("Create must return an error when Write is not declared")
	}
	if w != nil {
		t.Error("Create must return a nil Writer with its error")
	}
	if delErr := p.Delete(ctx, "a.gcode"); delErr == nil {
		t.Error("Delete must return an error when Write is not declared")
	}
	if delManyErr := p.DeleteMany(ctx, []string{"a.gcode"}); delManyErr == nil {
		t.Error("DeleteMany must return an error when Write is not declared")
	}
	if mkdirErr := p.Mkdir(ctx, "d"); mkdirErr == nil {
		t.Error("Mkdir must return an error when Write is not declared")
	}
	if renamer, ok := p.(storageapi.Renamer); ok {
		if renameErr := renamer.Rename(ctx, "a.gcode", "b.gcode"); renameErr == nil {
			t.Error("Rename must return an error when Write is not declared")
		}
	}
}

// A Presigner must resolve Subject.ResolvablePath to an absolute, fetchable
// URL, and must never pair a URL with ok=false or an empty URL with ok=true.
func testDownloadURLResolves(t *testing.T, s Subject) {
	ctx := t.Context()
	presigner := presignerOf(t, s.Provider)
	if s.ResolvablePath == "" {
		t.Fatal("Subject.ResolvablePath is required from a storageapi.Presigner")
	}
	raw, ok := presigner.DownloadURL(ctx, s.ResolvablePath)
	if !ok {
		t.Fatalf(
			"DownloadURL(%q) returned ok=false; a storageapi.Presigner must resolve it",
			s.ResolvablePath,
		)
	}
	if raw == "" {
		t.Fatal("DownloadURL returned ok=true with an empty URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("DownloadURL returned an unparseable URL %q: %v", raw, err)
	}
	if u.Scheme == "" || u.Host == "" {
		t.Errorf("DownloadURL must return an absolute, fetchable URL; got %q", raw)
	}
	if got, gotOK := presigner.DownloadURL(ctx, ""); gotOK || got != "" {
		t.Errorf(`DownloadURL("") = %q, %v; want "", false`, got, gotOK)
	}
}
