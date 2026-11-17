package main

import (
	"os"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
)

// The manifest is read here the way the host reads it out of the release, so
// an author who mistypes a key finds out from `go test` rather than from an
// install that writes a declaration nobody meant.

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

	if held.Name != name {
		t.Errorf("the manifest names %q and the process reports %q", held.Name, name)
	}

	if held.Version != string(release) {
		t.Errorf("the manifest says version %q and the process reports %q", held.Version, release)
	}

	if held.OpenAPI != "openapi.json" {
		t.Errorf("the manifest points at %q for its fragment", held.OpenAPI)
	}

	if held.UI != nil {
		t.Error("the manifest carries a ui block, and this module draws no page")
	}

	// This module does nothing as itself, so it asks for no ceiling. Every
	// call it makes is for the person the host authorized, and that call is
	// not held to a ceiling.
	if len(held.Permissions) != 0 {
		t.Errorf("the manifest asks for the ceiling %v, and this module does no work of its own",
			held.Permissions)
	}
}

// TestTheManifestAsksForEveryWireMethodTheResolversCall is ADR-20260910-cqq rule 13
// from the module's side. A host serving none of these would start a module
// whose every field answers a refusal, and the requires list is what says so
// before it starts.
func TestTheManifestAsksForEveryWireMethodTheResolversCall(t *testing.T) {
	t.Parallel()

	document, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}

	held, err := manifest.Parse("module.yaml", document, yaml.Unmarshal)
	if err != nil {
		t.Fatalf("parse the manifest: %v", err)
	}

	asked := make([]string, 0, len(held.Requires))
	for _, requirement := range held.Requires {
		asked = append(asked, requirement.Capability)
	}

	for _, method := range []string{
		"device.list", "device.get", "device.jobs",
		"device.files", "device.command", "device.approve",
	} {
		if !slices.Contains(asked, method) {
			t.Errorf("a resolver calls %s and the manifest does not require it: %v", method, asked)
		}
	}
}
