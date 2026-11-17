package connectorapi_test

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// testDoc is a complete descriptor, so a case is testing the registration rule
// rather than the documentation rule.
func testDoc() extension.Doc {
	return extension.Doc{
		Summary:     "A driver used only by this package's tests.",
		Description: "It builds nothing that talks to a device and exists to exercise the registry.",
		Version:     "1.0.0",
		Contract:    connectorapi.Contract,
		Stability:   extension.StabilityExperimental,
	}
}

// declared counts the names this package has taken. See freshName.
//
//nolint:gochecknoglobals // the counter has to outlive the case that reads it.
var declared atomic.Uint64

// freshName is a dialect name nothing has declared yet.
//
// connectorapi.Declare writes into the process's own name table and nothing
// takes an entry out again, so a fixed name is declared once and every run
// after that is refused. Under -count=2 the cases here failed on their first
// registration, which is the trap somebody chasing a flake with -count falls
// into first. A case that needs the same name twice holds on to what this
// returns rather than calling it again.
func freshName(stem string) connectorapi.TypeName {
	return connectorapi.TypeName(stem + "-" + strconv.FormatUint(declared.Add(1), 10))
}

func TestADuplicateRegistrationIsRefused(t *testing.T) {
	t.Parallel()

	var declarations connectorapi.Declarations

	name := freshName("duplicate-dialect")

	first := mustDeclare(t, name)
	if err := declarations.Add(first); err != nil {
		t.Fatalf("first registration: %v", err)
	}

	// The same name from the same module is the same qualified name, which is
	// the case a registry that replaces silently gets wrong: the second driver
	// takes over the first with nobody asking.
	second := mustDeclare(t, name)
	if err := declarations.Add(second); !errors.Is(err, connectorapi.ErrDuplicateType) {
		t.Fatalf("a second registration of %s was accepted: %v", second.Qualified(), err)
	}

	if got := len(declarations.Drain()); got != 1 {
		t.Errorf("the gathering list holds %d registrations after one was refused", got)
	}
}

func TestARegistrationAfterTheDrainIsRefused(t *testing.T) {
	t.Parallel()

	var declarations connectorapi.Declarations

	if err := declarations.Add(mustDeclare(t, freshName("before-the-drain"))); err != nil {
		t.Fatalf("registering before the drain: %v", err)
	}

	drained := declarations.Drain()

	if !declarations.Sealed() {
		t.Error("the gathering list is not sealed after a drain")
	}

	if err := declarations.Add(mustDeclare(t, freshName("after-the-drain"))); !errors.Is(err, connectorapi.ErrSealed) {
		t.Fatalf("a registration after the drain was accepted: %v", err)
	}

	// Draining twice answers the same, so a second registry is the same
	// registry rather than a smaller one.
	if second := declarations.Drain(); len(second) != len(drained) {
		t.Errorf("a second drain returned %d registrations and the first returned %d", len(second), len(drained))
	}
}

func TestTheNamespaceIsDerivedAndNotChosen(t *testing.T) {
	t.Parallel()

	registration := mustDeclare(t, freshName("host-derived"))

	// The test module is the sdk module, which is not the main module of this
	// build, so a name registered here is owned by its module path and not by
	// the core.
	if registration.Namespace().IsZero() {
		t.Fatal("the registration carries no namespace")
	}

	want := connectorapi.TypeName(registration.Namespace().String() + "/" + registration.Name().String())
	if registration.Qualified() != want {
		t.Errorf("the qualified name %q is not the namespace and the name", registration.Qualified())
	}
}

func TestAnInvalidTypeNameIsRefused(t *testing.T) {
	t.Parallel()

	for _, name := range []connectorapi.TypeName{
		"",
		"Uppercase",
		"under_score",
		"-leading",
		"trailing-",
		"with/slash",
		connectorapi.TypeName(strings.Repeat("a", 64)),
	} {
		t.Run(string(name), func(t *testing.T) {
			t.Parallel()

			_, err := connectorapi.Declare(name, testDoc(), func(connectorapi.Spec) (connectorapi.Driver, error) {
				return dialledInto{}, nil
			})
			if !errors.Is(err, connectorapi.ErrInvalidTypeName) {
				t.Errorf("%q was accepted as a type name: %v", name, err)
			}
		})
	}
}

func TestAFactorylessRegistrationIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := connectorapi.Declare("no-factory", testDoc(), nil); !errors.Is(err, connectorapi.ErrNoFactory) {
		t.Errorf("a type with no factory was declared: %v", err)
	}
}

func TestAnUnknownTypeNamesWhatTheBuildHas(t *testing.T) {
	t.Parallel()

	registry, err := connectorapi.NewRegistryFrom(mustDeclare(t, freshName("present-dialect")))
	if err != nil {
		t.Fatalf("NewRegistryFrom: %v", err)
	}

	_, err = registry.Build(connectorapi.Spec{Ref: "printer-1", Type: "absent-dialect"})
	if !errors.Is(err, connectorapi.ErrUnknownType) {
		t.Fatalf("an unregistered type built something: %v", err)
	}
}

func TestASpecCarriesADriverPrivateConfigBlock(t *testing.T) {
	t.Parallel()

	var got struct {
		Listen  string `json:"listen"`
		Devices int    `json:"devices"`
	}

	registration, err := connectorapi.Declare("configured", testDoc(),
		func(spec connectorapi.Spec) (connectorapi.Driver, error) {
			if decodeErr := spec.DecodeConfig(&got); decodeErr != nil {
				return nil, decodeErr
			}

			return dialledInto{}, nil
		})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	registry, err := connectorapi.NewRegistryFrom(registration)
	if err != nil {
		t.Fatalf("NewRegistryFrom: %v", err)
	}

	spec := connectorapi.Spec{
		Ref:    "printer-1",
		Type:   "configured",
		Config: json.RawMessage(`{"listen":":9000","devices":4}`),
	}

	if _, built := registry.Build(spec); built != nil {
		t.Fatalf("Build: %v", built)
	}

	if got.Listen != ":9000" || got.Devices != 4 {
		t.Errorf("the factory read %+v out of the spec", got)
	}
}

func TestAnAbsentConfigBlockLeavesTheDriverDefaults(t *testing.T) {
	t.Parallel()

	settings := struct {
		Listen string `json:"listen"`
	}{Listen: ":8080"}

	if err := (connectorapi.Spec{Ref: "printer-1"}).DecodeConfig(&settings); err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}

	if settings.Listen != ":8080" {
		t.Errorf("an absent config block overwrote the driver's default with %q", settings.Listen)
	}
}

func mustDeclare(t *testing.T, name connectorapi.TypeName) connectorapi.Registration {
	t.Helper()

	registration, err := connectorapi.Declare(name, testDoc(), func(connectorapi.Spec) (connectorapi.Driver, error) {
		return dialledInto{}, nil
	})
	if err != nil {
		t.Fatalf("Declare(%q): %v", name, err)
	}

	return registration
}
