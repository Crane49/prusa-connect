package storageapi_test

import (
	"context"
	"io"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"
)

// nopProvider is a do-nothing Provider that exercises the registry. It
// implements the mandatory ten methods and none of the optional interfaces,
// which is the shape a store with nothing to offer beyond the contract has.
type nopProvider struct{}

func (nopProvider) Name() string        { return "nop" }
func (nopProvider) DisplayName() string { return "nop" }

func (nopProvider) List(context.Context, string) ([]storageapi.FileInfo, error) { return nil, nil }

func (nopProvider) Stat(context.Context, string) (storageapi.FileInfo, error) {
	return storageapi.FileInfo{}, nil
}

//nolint:nilnil // test stub
func (nopProvider) Open(context.Context, string) (io.ReadCloser, error) { return nil, nil }

//nolint:nilnil // test stub
func (nopProvider) Create(context.Context, string) (storageapi.Writer, error) { return nil, nil }

func (nopProvider) Delete(context.Context, string) error       { return nil }
func (nopProvider) DeleteMany(context.Context, []string) error { return nil }
func (nopProvider) Mkdir(context.Context, string) error        { return nil }
func (nopProvider) Capabilities() storageapi.Caps              { return storageapi.Caps{} }

// registrations names a type nothing has registered yet.
//
// A registration cannot be taken out of the gathering list again, so a fixed
// name is registered once and the run after that starts from a list that
// already holds it. Under -count=2 the first line below failed, which is the
// trap somebody chasing a flake with -count falls into first.
//
//nolint:gochecknoglobals // the counter has to outlive the case that reads it.
var registrations atomic.Uint64

func doc() extension.Doc {
	return extension.Doc{
		Summary:     "A store for a test.",
		Description: "It answers the contract and holds nothing.",
		Version:     "1.0.0",
		Contract:    storageapi.Contract,
		Stability:   extension.StabilityStable,
	}
}

func TestRegistry(t *testing.T) {
	t.Parallel()

	typ := "storageapi-test-type-" + strconv.FormatUint(registrations.Add(1), 10)

	empty, err := storageapi.NewRegistryFrom()
	if err != nil {
		t.Fatalf("NewRegistryFrom: %v", err)
	}

	if _, built := empty.BuildProvider(storageapi.ProviderSpec{ID: "x", Type: typ}); built == nil {
		t.Fatal("BuildProvider should error for an unregistered type")
	}

	called := false

	registration, err := storageapi.Declare(typ, doc(),
		func(_ storageapi.ProviderSpec) (storageapi.Provider, error) {
			called = true

			return nopProvider{}, nil
		})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	registry, err := storageapi.NewRegistryFrom(registration)
	if err != nil {
		t.Fatalf("NewRegistryFrom: %v", err)
	}

	if _, built := registry.BuildProvider(storageapi.ProviderSpec{ID: "x", Type: typ}); built != nil {
		t.Fatalf("BuildProvider for registered type: %v", built)
	}

	if !called {
		t.Fatal("registered factory was not invoked")
	}
}

func TestValidateExtension(t *testing.T) {
	allowed := []string{".gcode", ".STL"} // case-insensitive
	for _, ok := range []string{"a.gcode", "A.GCODE", "b.stl", "c.Stl"} {
		if err := storageapi.ValidateExtension(ok, allowed); err != nil {
			t.Errorf("ValidateExtension(%q) = %v, want nil", ok, err)
		}
	}
	if err := storageapi.ValidateExtension("evil.sh", allowed); err == nil {
		t.Error("expected ErrExtensionNotAllowed for .sh")
	}
	// Empty allowlist permits anything.
	if err := storageapi.ValidateExtension("anything.xyz", nil); err != nil {
		t.Errorf("empty allowlist should permit all, got %v", err)
	}
}

func TestExtensionClassifiers(t *testing.T) {
	if !storageapi.IsPrintableExtension("a.gcode") || !storageapi.IsPrintableExtension("a.bgcode") {
		t.Error("g-code family should be printable")
	}
	if err := storageapi.ValidateExtension("firmware.BBF", storageapi.DefaultAllowedExtensions); err != nil {
		t.Errorf("a .bbf firmware file should be uploadable, got %v", err)
	}
	if storageapi.IsPrintableExtension("firmware.bbf") {
		t.Error("a firmware file is sent to a printer, never printed")
	}
	if storageapi.IsPrintableExtension("a.stl") {
		t.Error("stl is not directly printable")
	}
	if !storageapi.IsSliceableExtension("a.stl") || !storageapi.IsSliceableExtension("a.3mf") ||
		!storageapi.IsSliceableExtension("a.obj") {
		t.Error("stl/3mf/obj should be sliceable")
	}
	if storageapi.IsSliceableExtension("a.gcode") {
		t.Error("gcode is not sliceable")
	}
}

func TestNopProviderDeclaresNothing(t *testing.T) {
	var p storageapi.Provider = nopProvider{}
	if got := p.Capabilities(); got != (storageapi.Caps{}) {
		t.Fatalf("nopProvider.Capabilities() = %+v, want the zero Caps", got)
	}
	// Probe agrees, because a provider that implements none of the optional
	// interfaces cannot have one attributed to it.
	if got := storageapi.Probe(p); got != (storageapi.Caps{}) {
		t.Fatalf("Probe(nopProvider) = %+v, want the zero Caps", got)
	}
}
