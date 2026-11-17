package main

import (
	"slices"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// TestTheFragmentIsOneTheHostAccepts holds the embedded document to the rule
// the host applies at launch.
func TestTheFragmentIsOneTheHostAccepts(t *testing.T) {
	t.Parallel()

	info, err := routesapi.Describe(
		wirerpc.Info{Name: name, Version: extension.Version(version), Contract: wirerpc.Contract},
		openAPI,
	)
	if err != nil {
		t.Fatalf("the host would refuse this module's fragment: %v", err)
	}

	fragment, err := routesapi.ParseFragment(info.OpenAPI)
	if err != nil {
		t.Fatalf("parse what plugin.info carries: %v", err)
	}

	if !slices.Contains(fragment.Paths(), "/notes") {
		t.Errorf("the fragment does not describe /notes, and the module serves it")
	}
}
