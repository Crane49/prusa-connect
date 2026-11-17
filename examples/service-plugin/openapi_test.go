package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// TestTheFragmentIsOneTheHostAccepts is what a module author runs before the
// host does: the embedded document passes the rule the host holds it to, and
// it rebases under a mount without losing a path.
func TestTheFragmentIsOneTheHostAccepts(t *testing.T) {
	t.Parallel()

	info, err := routesapi.Describe(wirerpc.Info{Name: name, Version: release, Contract: wirerpc.Contract}, openAPI)
	if err != nil {
		t.Fatalf("the host would refuse this module's fragment: %v", err)
	}

	fragment, err := routesapi.ParseFragment(info.OpenAPI)
	if err != nil {
		t.Fatalf("parse what plugin.info carries: %v", err)
	}

	for _, path := range []string{"/spools", "/records", "/records/{key}", "/devices", "/events", "/call"} {
		if !slices.Contains(fragment.Paths(), path) {
			t.Errorf("the fragment does not describe %s, and the module serves it", path)
		}
	}

	rebased, err := fragment.Rebase("/api/v1/modules/com.github.aux.acme.example")
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}

	if !strings.Contains(string(rebased), `"/api/v1/modules/com.github.aux.acme.example/spools"`) {
		t.Errorf("the rebased document does not carry the spools route under the mount")
	}
}
