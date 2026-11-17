package registry_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/extension/host"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// testDoc is a complete descriptor, so a case tests the registration rule and
// not the documentation rule.
func testDoc() extension.Doc {
	return extension.Doc{
		Summary:     "A resource type used only by this package's tests.",
		Description: "It builds a value that does nothing and exists to exercise the registry.",
		Version:     "1.0.0",
		Contract:    registry.Contract,
		Stability:   extension.StabilityExperimental,
	}
}

// store is the contract a registered type serves in these tests.
type store interface {
	Root() string
}

type localStore struct{ root string }

func (l localStore) Root() string { return l.root }

type localConfig struct {
	Root string `json:"root"`
}

func localFactory(_ context.Context, _ host.Handle, spec registry.Spec) (store, error) {
	var cfg localConfig
	if err := spec.DecodeConfig(&cfg); err != nil {
		return nil, err
	}

	return localStore{root: cfg.Root}, nil
}

func testSpec(t *testing.T, typeName, config string) registry.Spec {
	t.Helper()

	instance, err := ref.New(ref.KindStorage, "files")
	require.NoError(t, err)

	return registry.Spec{Ref: instance, Type: typeName, Config: json.RawMessage(config)}
}

func TestRegisteredTypeIsNamespacedByItsModule(t *testing.T) {
	t.Parallel()

	declared, err := registry.Declare(ref.KindStorage, "local", testDoc(), localFactory)
	require.NoError(t, err)

	// The namespace comes from the build's module graph, not from anything the
	// author writes, so two modules cannot claim one name.
	assert.True(t, declared.Namespace().IsCore(), "a type declared in the main module is core")
	assert.Equal(t, "core/local", declared.Qualified())
	assert.Equal(t, "local", declared.Name())
	assert.Equal(t, ref.KindStorage, declared.Kind())
}

func TestConstructBuildsTheContract(t *testing.T) {
	t.Parallel()

	declared, err := registry.Declare(ref.KindStorage, "local", testDoc(), localFactory)
	require.NoError(t, err)

	reg, err := registry.NewFrom(declared)
	require.NoError(t, err)

	built, err := registry.Construct[store](t.Context(), reg, ref.KindStorage, host.Discard(),
		testSpec(t, "local", `{"root":"/srv/files"}`))
	require.NoError(t, err)

	assert.Equal(t, "/srv/files", built.Root())
}

func TestConstructResolvesAQualifiedName(t *testing.T) {
	t.Parallel()

	declared, err := registry.Declare(ref.KindStorage, "local", testDoc(), localFactory)
	require.NoError(t, err)

	reg, err := registry.NewFrom(declared)
	require.NoError(t, err)

	built, err := registry.Construct[store](t.Context(), reg, ref.KindStorage, host.Discard(),
		testSpec(t, "core/local", `{"root":"/srv"}`))
	require.NoError(t, err)

	assert.Equal(t, "/srv", built.Root())
}

func TestUnknownTypeNamesWhatTheBuildHas(t *testing.T) {
	t.Parallel()

	declared, err := registry.Declare(ref.KindStorage, "local", testDoc(), localFactory)
	require.NoError(t, err)

	reg, err := registry.NewFrom(declared)
	require.NoError(t, err)

	_, err = registry.Construct[store](t.Context(), reg, ref.KindStorage, host.Discard(), testSpec(t, "s3", ""))

	require.ErrorIs(t, err, registry.ErrUnknownType)
	assert.Contains(t, err.Error(), "core/local", "the error names the types this build has")
}

func TestConstructReportsAContractMismatch(t *testing.T) {
	t.Parallel()

	declared, err := registry.Declare(ref.KindStorage, "local", testDoc(), localFactory)
	require.NoError(t, err)

	reg, err := registry.NewFrom(declared)
	require.NoError(t, err)

	type sink interface{ Deliver() }

	_, err = registry.Construct[sink](t.Context(), reg, ref.KindStorage, host.Discard(), testSpec(t, "local", ""))

	require.ErrorIs(t, err, registry.ErrTypeMismatch)
}

func TestDeclareRejectsWhatCannotBeAddressed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		kind      ref.Kind
		typeName  string
		construct func(context.Context, host.Handle, registry.Spec) (store, error)
	}{
		{name: "a kind outside the closed set", kind: "printer", typeName: "local", construct: localFactory},
		{name: "an upper-case type name", kind: ref.KindStorage, typeName: "Local", construct: localFactory},
		{name: "a type name with a slash", kind: ref.KindStorage, typeName: "acme/s3", construct: localFactory},
		{name: "an empty type name", kind: ref.KindStorage, typeName: "", construct: localFactory},
		{name: "no constructor", kind: ref.KindStorage, typeName: "local", construct: nil},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := registry.Declare(testCase.kind, testCase.typeName, testDoc(), testCase.construct)
			require.Error(t, err)
		})
	}
}

// gathered is registered during package initialisation, the same way a build
// registers a type through a blank import.
//
//nolint:gochecknoglobals // registration happens at init time.
var gathered = registerAtInit()

func registerAtInit() error {
	return registry.TryRegister(ref.KindSink, "gathered-sink", testDoc(), localFactory)
}

func TestInitTimeRegistrationReachesTheRegistry(t *testing.T) {
	t.Parallel()

	require.NoError(t, gathered)

	reg, err := registry.New()
	require.NoError(t, err)

	found, err := reg.Lookup(ref.KindSink, "gathered-sink")
	require.NoError(t, err)
	assert.Equal(t, "core/gathered-sink", found.Qualified())
}

func TestDrainingTwiceAnswersTheSame(t *testing.T) {
	t.Parallel()

	// Two registries built from one set of declarations hold the same types.
	first, err := registry.New()
	require.NoError(t, err)

	second, err := registry.New()
	require.NoError(t, err)

	assert.Len(t, second.All(), len(first.All()))
}

func TestRegisteringAfterTheListIsDrainedIsRefused(t *testing.T) {
	t.Parallel()

	var declarations registry.Declarations

	declared, err := registry.Declare(ref.KindStorage, "local", testDoc(), localFactory)
	require.NoError(t, err)

	require.NoError(t, declarations.Add(declared))
	require.False(t, declarations.Sealed())

	assert.Len(t, declarations.Drain(), 1)
	assert.True(t, declarations.Sealed())

	// Without the refusal, a registration made after the registry was built
	// would do nothing and say nothing.
	late, err := registry.Declare(ref.KindSink, "too-late", testDoc(), localFactory)
	require.NoError(t, err)

	require.ErrorIs(t, declarations.Add(late), registry.ErrSealed)
	assert.Len(t, declarations.Drain(), 1, "and draining again answers the same")
}

func TestDeclarationsRefuseOneQualifiedNameTwice(t *testing.T) {
	t.Parallel()

	var declarations registry.Declarations

	// A name no other case uses. One qualified name is unique across every
	// gathering list in the process, so two cases sharing one would collide.
	declared, err := registry.Declare(ref.KindStorage, "local-twice", testDoc(), localFactory)
	require.NoError(t, err)

	require.NoError(t, declarations.Add(declared))
	require.ErrorIs(t, declarations.Add(declared), registry.ErrDuplicateType)
}

func TestNewFromRejectsOneQualifiedNameTwice(t *testing.T) {
	t.Parallel()

	declared, err := registry.Declare(ref.KindStorage, "local", testDoc(), localFactory)
	require.NoError(t, err)

	_, err = registry.NewFrom(declared, declared)
	require.ErrorIs(t, err, registry.ErrDuplicateType)
}

func TestTypesAreListedPerKind(t *testing.T) {
	t.Parallel()

	first, err := registry.Declare(ref.KindStorage, "local", testDoc(), localFactory)
	require.NoError(t, err)

	second, err := registry.Declare(ref.KindStorage, "usb", testDoc(), localFactory)
	require.NoError(t, err)

	other, err := registry.Declare(ref.KindSink, "webhook", testDoc(), localFactory)
	require.NoError(t, err)

	reg, err := registry.NewFrom(first, second, other)
	require.NoError(t, err)

	names := make([]string, 0, 2)
	for _, found := range reg.Types(ref.KindStorage) {
		names = append(names, found.Qualified())
	}

	assert.Equal(t, []string{"core/local", "core/usb"}, names)
	assert.Len(t, reg.All(), 3)
}

// foreignDescriptor is a descriptor from a module outside this build. It is
// decoded from JSON, because Declare derives the namespace from the calling
// package and every package in this tree is the core's. Reading one back the
// way the published catalogue stores it is the only way to get another
// module's descriptor here.
func foreignDescriptor(t *testing.T, kind extension.Kind, namespace, name string) extension.Descriptor {
	t.Helper()

	record := map[string]any{
		"kind":      kind,
		"name":      name,
		"namespace": namespace,
		"module":    "github.com/acme/shelf",
		"summary":   "A resource type an out-of-tree module registered.",
		"description": "It stands for anything a composed build links that the core " +
			"module never heard of.",
		"version":   "1.0.0",
		"contract":  registry.Contract,
		"stability": extension.StabilityExperimental,
	}

	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("encode the descriptor: %v", err)
	}

	var descriptor extension.Descriptor
	if err = json.Unmarshal(encoded, &descriptor); err != nil {
		t.Fatalf("decode the descriptor: %v", err)
	}

	return descriptor
}

// TestAdoptKeepsTheDescriptorItWasGiven checks that a registration from another
// module keeps the namespace the host derived from the build graph. If Adopt
// minted a new descriptor, every composed adapter would be published under
// core/.
func TestAdoptKeepsTheDescriptorItWasGiven(t *testing.T) {
	t.Parallel()

	descriptor := foreignDescriptor(t, extension.KindEventSink, "com.github.aux.acme.shelf", "ledger")

	adopted, err := registry.Adopt(ref.KindSink, descriptor,
		func(_ context.Context, _ host.Handle, _ registry.Spec) (string, error) { return "built", nil })
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	if got, want := adopted.Qualified(), "com.github.aux.acme.shelf/ledger"; got != want {
		t.Errorf("Adopt().Qualified() = %q, want %q", got, want)
	}

	if got := adopted.Name(); got != "ledger" {
		t.Errorf("Adopt().Name() = %q, want ledger", got)
	}
}

// TestAdoptRefusesADescriptorThatIsNotTheKindsOwn keeps adoption from becoming
// a way to file a camera under storage. The descriptor is trusted for its
// namespace, which the host derived, and for nothing else.
func TestAdoptRefusesADescriptorThatIsNotTheKindsOwn(t *testing.T) {
	t.Parallel()

	descriptor := foreignDescriptor(t, extension.KindCamera, "com.github.aux.acme.shelf", "lens")

	_, err := registry.Adopt(ref.KindStorage, descriptor,
		func(_ context.Context, _ host.Handle, _ registry.Spec) (string, error) { return "built", nil })
	if !errors.Is(err, registry.ErrForeignDescriptor) {
		t.Errorf("Adopt with a camera descriptor under storage = %v, want ErrForeignDescriptor", err)
	}
}

// TestAdoptRefusesAnEmptyDescriptorAndAMissingConstructor covers a caller that
// gives Adopt no descriptor, and one that gives it no constructor.
func TestAdoptRefusesAnEmptyDescriptorAndAMissingConstructor(t *testing.T) {
	t.Parallel()

	if _, err := registry.Adopt[string](ref.KindSink, extension.Descriptor{}, nil); !errors.Is(
		err, registry.ErrForeignDescriptor) {
		t.Errorf("Adopt with no descriptor = %v, want ErrForeignDescriptor", err)
	}

	descriptor := foreignDescriptor(t, extension.KindEventSink, "com.github.aux.acme.shelf", "ledger")
	if _, err := registry.Adopt[string](ref.KindSink, descriptor, nil); !errors.Is(err, registry.ErrNoConstructor) {
		t.Errorf("Adopt with no constructor = %v, want ErrNoConstructor", err)
	}
}
