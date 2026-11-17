package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The manifest is read here the way the host reads it out of the bundle, so an
// author who mistypes a key finds out from `go test` rather than from a page
// that draws nothing.

func TestTheManifestIsOneTheHostAccepts(t *testing.T) {
	t.Parallel()

	document, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}

	held, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
	if err != nil {
		t.Fatalf("the host would refuse this manifest: %v", err)
	}

	if held.UI == nil {
		t.Fatal("the manifest carries no ui block, so the module would draw nothing")
	}

	if held.UI.Entry != "ui/index.js" {
		t.Errorf("the entry is %q, and the build writes ui/index.js", held.UI.Entry)
	}

	if len(held.UI.Contributes.Pages) != 1 {
		t.Fatalf("the pages read as %+v", held.UI.Contributes.Pages)
	}

	page := held.UI.Contributes.Pages[0]
	if page.Path != "spools" || page.Title != "Spools" || page.Icon != "layers" {
		t.Errorf("the page reads as %+v", page)
	}

	if page.Hidden {
		t.Error("the page is hidden, so no sidebar entry follows from it")
	}
}

// TestTheLibraryRangeHoldsTheVersionTheLibraryIs is the gate on ADR-20260911-4p6 rule
// 24 from the module's side. The app refuses a bundle whose range its own
// version falls outside, and this example is built against the library in this
// checkout, so a range that no longer covers it would ship a module nothing
// loads.
func TestTheLibraryRangeHoldsTheVersionTheLibraryIs(t *testing.T) {
	t.Parallel()

	document, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}

	held, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
	if err != nil {
		t.Fatalf("parse the manifest: %v", err)
	}

	wanted, err := manifest.ParseRange(held.UI.Library)
	if err != nil {
		t.Fatalf("the library range is not one the host reads: %v", err)
	}

	packaged, err := os.ReadFile(filepath.Join("..", "..", "packages", "svelte", "package.json"))
	if err != nil {
		t.Fatalf("read the library's package.json: %v", err)
	}

	var library struct {
		Version string `json:"version"`
	}

	if err = json.Unmarshal(packaged, &library); err != nil {
		t.Fatalf("read the library's version: %v", err)
	}

	inside, err := wanted.Includes(extension.Version(library.Version))
	if err != nil {
		t.Fatalf("the library's version is %q: %v", library.Version, err)
	}

	if !inside {
		t.Errorf("the manifest asks for %q and the library in this checkout is %s",
			held.UI.Library, library.Version)
	}
}
