package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/prusa3d/prusa-connect/internal/extension/host"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
	"github.com/prusa3d/prusa-connect/sdk/cameraapi"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
	"github.com/prusa3d/prusa-connect/sdk/extension"
	"github.com/prusa3d/prusa-connect/sdk/historyapi"
	"github.com/prusa3d/prusa-connect/sdk/stateapi"
	"github.com/prusa3d/prusa-connect/sdk/storageapi"

	// The adapters this artefact carries. A blank import runs the adapter's init
	// function, and the adoption lists below are what the resource plane then
	// lets an operator declare. Both are needed: the import links the code, the
	// entry admits the name.
	//
	// adapters/connector/echo registers with sdk/connectorapi and not with the
	// resource plane, so it takes no adoption entry below. It authenticates
	// nobody and is mounted only where core.connectors names it.
	_ "github.com/prusa3d/prusa-connect/adapters/auth/oidc"
	_ "github.com/prusa3d/prusa-connect/adapters/auth/prusaaccount"
	_ "github.com/prusa3d/prusa-connect/adapters/camera/generic"
	_ "github.com/prusa3d/prusa-connect/adapters/camera/mjpeg"
	_ "github.com/prusa3d/prusa-connect/adapters/camera/push"
	_ "github.com/prusa3d/prusa-connect/adapters/camera/rtsp"
	_ "github.com/prusa3d/prusa-connect/adapters/connector/echo"
	_ "github.com/prusa3d/prusa-connect/adapters/db/postgres"
	_ "github.com/prusa3d/prusa-connect/adapters/db/sqlite"
	_ "github.com/prusa3d/prusa-connect/adapters/events/amqp"
	_ "github.com/prusa3d/prusa-connect/adapters/events/kafka"
	_ "github.com/prusa3d/prusa-connect/adapters/events/mqtt"
	_ "github.com/prusa3d/prusa-connect/adapters/events/webhook"
	_ "github.com/prusa3d/prusa-connect/adapters/storage/local"
	_ "github.com/prusa3d/prusa-connect/adapters/storage/temporary"
)

// adaptedSinks, adaptedIdentities, adaptedCameras and adaptedStorages are the
// qualified names this composition root lifts out of the published contract
// registries and onto the resource plane.
//
// They are inclusion lists. A name this plane admits is a name an operator may
// write and the published reference documents, so each core name is written
// down here. A plane that took everything a contract registry held would
// publish whatever a blank import linked under a core name.
var (
	//nolint:gochecknoglobals // the composition root's inventory, never written to.
	adaptedSinks = []string{"core/amqp", "core/kafka", "core/mqtt", "core/webhook"}
	//nolint:gochecknoglobals // the composition root's inventory, never written to.
	adaptedIdentities = []string{"core/oidc", "core/prusa-account"}
	//nolint:gochecknoglobals // the composition root's inventory, never written to.
	adaptedCameras = []string{"core/generic", "core/mjpeg", "core/push", "core/rtsp"}
	//nolint:gochecknoglobals // the composition root's inventory, never written to.
	adaptedStorages = []string{"core/local", "core/temporary"}
	//nolint:gochecknoglobals // the composition root's inventory, never written to.
	adaptedHistories = []string{"core/sqlite", "core/postgres"}
	//nolint:gochecknoglobals // the composition root's inventory, never written to.
	adaptedDatabases = []string{"core/sqlite", "core/postgres"}
)

// Errors reported while the adapter registries are folded into the resource
// type table. Callers inspect them with errors.Is.
var (
	// ErrAdapterConfig reports a settings block an adapter's kind cannot read.
	ErrAdapterConfig = errors.New("builtin: invalid adapter settings")
	// ErrForeignNamespace reports an adapter whose name the resource plane
	// cannot declare truthfully. registry.Declare mints a namespace for the
	// package that calls it, so an adapter registered from another module would
	// be republished under this module's name.
	ErrForeignNamespace = errors.New("builtin: an adapter outside the main module cannot be declared here")
)

// Registry is the resource type table this build serves: the types registered
// here, and the adapters registered through the published contracts in sdk/.
//
// The two registries are separate. An adapter compiles against sdk/ alone, so
// it registers with a contract registry in sdk/ and never with the
// application's own registry. This function joins the two.
func Registry() (*registry.Registry, error) {
	declared, err := registry.New()
	if err != nil {
		return nil, err
	}

	adapted, err := adapters()
	if err != nil {
		return nil, err
	}

	types, err := registry.NewFrom(append(declared.All(), adapted...)...)
	if err != nil {
		return nil, err
	}

	return types, nil
}

// contracts are the six published contract registries a build's adapters
// register into. Each one is read twice: once for the reference build's own
// inventory and once for whatever else a composed build linked.
type contracts struct {
	sinks      *eventsapi.Registry
	identities *authapi.Registry
	cameras    *cameraapi.Registry
	storages   *storageapi.Registry
	histories  *historyapi.Registry
	databases  *stateapi.Registry
}

// newContracts builds all six. A registry that will not build is a fault in
// this artefact and not in an operator's configuration, so each one names
// itself in the error.
func newContracts() (contracts, error) {
	var (
		all contracts
		err error
	)

	if all.sinks, err = eventsapi.NewRegistry(); err != nil {
		return contracts{}, fmt.Errorf("build the event sink registry: %w", err)
	}

	if all.identities, err = authapi.NewRegistry(); err != nil {
		return contracts{}, fmt.Errorf("build the identity provider registry: %w", err)
	}

	if all.cameras, err = cameraapi.NewRegistry(); err != nil {
		return contracts{}, fmt.Errorf("build the camera registry: %w", err)
	}

	if all.storages, err = storageapi.NewRegistry(); err != nil {
		return contracts{}, fmt.Errorf("build the storage registry: %w", err)
	}

	if all.histories, err = historyapi.NewRegistry(); err != nil {
		return contracts{}, fmt.Errorf("build the history provider registry: %w", err)
	}

	if all.databases, err = stateapi.NewRegistry(); err != nil {
		return contracts{}, fmt.Errorf("build the record backend registry: %w", err)
	}

	return all, nil
}

// shipped adopts the names the six inventories above hold. They are the
// reference build's own list.
func (c contracts) shipped(collect func(registry.Type, error)) {
	for _, qualified := range adaptedSinks {
		collect(sinkType(c.sinks, qualified))
	}

	for _, qualified := range adaptedIdentities {
		collect(identityType(c.identities, qualified))
	}

	for _, qualified := range adaptedCameras {
		collect(cameraType(c.cameras, qualified))
	}

	for _, qualified := range adaptedStorages {
		collect(storageType(c.storages, qualified))
	}

	for _, qualified := range adaptedHistories {
		collect(historyType(c.histories, qualified))
	}

	for _, qualified := range adaptedDatabases {
		collect(databaseType(c.databases, qualified))
	}
}

// linked adopts everything the binary carries from outside the core module. A
// composed adapter is admitted because it is linked: whoever composed the
// build chose to trust it.
func (c contracts) linked(collect func(registry.Type, error)) {
	for _, sink := range composed(c.sinks.Types()) {
		collect(adoptSink(sink))
	}

	for _, identity := range composed(c.identities.Types()) {
		collect(adoptIdentity(identity))
	}

	for _, camera := range composed(c.cameras.Types()) {
		collect(adoptCamera(camera))
	}

	for _, store := range composed(c.storages.Types()) {
		collect(adoptStore(store))
	}

	for _, provider := range composed(c.histories.Types()) {
		collect(adoptHistory(provider))
	}

	for _, backend := range composed(c.databases.Types()) {
		collect(adoptDatabase(backend))
	}
}

// adapters is every adopted registration as a resource type. A name this
// artefact admits and does not link is an error. Without it the operator would
// meet an unknown type at start-up.
//
// Every fault is collected, so an operator fixing a build reads the whole list
// once.
func adapters() ([]registry.Type, error) {
	all, err := newContracts()
	if err != nil {
		return nil, err
	}

	var (
		types  []registry.Type
		faults []error
	)

	collect := func(declared registry.Type, err error) {
		if err != nil {
			faults = append(faults, err)

			return
		}

		types = append(types, declared)
	}

	all.shipped(collect)
	all.linked(collect)

	if len(faults) > 0 {
		return nil, errors.Join(faults...)
	}

	return types, nil
}

// registration is what every published contract's registration answers: the
// namespace the host derived for the module that registered it.
type registration interface{ Namespace() extension.Namespace }

// composed is the registrations that came from outside the core module.
//
// The six lists above name what the shipped reference build carries, and every
// one of those names is core's. Anything else in a contract registry is there
// because a build linked another module. Linking is the admission: there is no
// second list to add it to and nothing for an operator to enable.
func composed[R registration](all []R) []R {
	var out []R

	for _, one := range all {
		if !one.Namespace().IsCore() {
			out = append(out, one)
		}
	}

	return out
}

// sinkType is one event sink adapter as a resource type.
func sinkType(sinks *eventsapi.Registry, qualified string) (registry.Type, error) {
	registered, err := sinks.Lookup(qualified)
	if err != nil {
		return registry.Type{}, fmt.Errorf("adopt the %s sink: %w", qualified, err)
	}

	return adoptSink(registered)
}

// adoptSink declares one event sink registration as a resource type. It is
// split from the lookup so a caller holding a registration can adopt it
// directly.
func adoptSink(registered eventsapi.Registration) (registry.Type, error) {
	return adopt(ref.KindSink, registered.Qualified(), registered.Descriptor(),
		func(_ context.Context, _ host.Handle, spec registry.Spec) (eventsapi.Processor, error) {
			settings, decodeErr := openSettings(spec)
			if decodeErr != nil {
				return nil, decodeErr
			}

			return registered.Build(eventsapi.ProviderSpec{Type: spec.Type, Config: settings})
		})
}

// historyType is one history provider adapter as a resource type.
//
// The settings reach the adapter as raw JSON, not as the open map a sink is
// handed. The host names no key inside a history provider's block: where the
// rows land, on what engine and with what retention is the adapter's business.
func historyType(histories *historyapi.Registry, qualified string) (registry.Type, error) {
	registered, err := histories.Lookup(qualified)
	if err != nil {
		return registry.Type{}, fmt.Errorf("adopt the %s history provider: %w", qualified, err)
	}

	return adoptHistory(registered)
}

// adoptHistory declares one history provider registration as a resource type.
// It is split from the lookup so a caller holding a registration can adopt it
// directly. A composed provider from outside the core module reaches the plane
// this way.
func adoptHistory(registered historyapi.Registration) (registry.Type, error) {
	return adopt(ref.KindHistory, registered.Qualified(), registered.Descriptor(),
		func(_ context.Context, handle host.Handle, spec registry.Spec) (historyapi.Provider, error) {
			return registered.Build(historyapi.ProviderSpec{
				ID: spec.Ref.ID(), Type: spec.Type, Config: spec.Config, Logger: handle.Logger,
			})
		})
}

// databaseType is one record backend adapter as a resource type.
//
// The settings reach the adapter as raw JSON for the reason a history
// provider's do: the host names no key inside a backend's block, because where
// a module's records land and on what engine is the adapter's business.
func databaseType(databases *stateapi.Registry, qualified string) (registry.Type, error) {
	registered, err := databases.Lookup(qualified)
	if err != nil {
		return registry.Type{}, fmt.Errorf("adopt the %s record backend: %w", qualified, err)
	}

	return adoptDatabase(registered)
}

// adoptDatabase declares one record backend registration as a resource type. It
// is split from the lookup for the reason adoptHistory is.
func adoptDatabase(registered stateapi.Registration) (registry.Type, error) {
	return adopt(ref.KindDB, registered.Qualified(), registered.Descriptor(),
		func(_ context.Context, handle host.Handle, spec registry.Spec) (stateapi.Backend, error) {
			return registered.Build(stateapi.ProviderSpec{
				ID: spec.Ref.ID(), Type: spec.Type, Config: spec.Config, Logger: handle.Logger,
			})
		})
}

// identityType is one identity provider adapter as a resource type.
func identityType(identities *authapi.Registry, qualified string) (registry.Type, error) {
	registered, err := identities.Lookup(qualified)
	if err != nil {
		return registry.Type{}, fmt.Errorf("adopt the %s identity provider: %w", qualified, err)
	}

	return adoptIdentity(registered)
}

// adoptIdentity declares one identity provider registration as a resource type.
// It is split from the lookup so a caller holding a registration can adopt it
// directly.
func adoptIdentity(registered authapi.Registration) (registry.Type, error) {
	return adopt(ref.KindAuth, registered.Qualified(), registered.Descriptor(),
		func(_ context.Context, _ host.Handle, spec registry.Spec) (authapi.IdentityProvider, error) {
			settings, decodeErr := openSettings(spec)
			if decodeErr != nil {
				return nil, decodeErr
			}

			return registered.Build(authapi.ProviderSpec{Type: spec.Type, Config: settings})
		})
}

// storageType is one storage adapter as a resource type.
func storageType(stores *storageapi.Registry, qualified string) (registry.Type, error) {
	registered, err := stores.Lookup(qualified)
	if err != nil {
		return registry.Type{}, fmt.Errorf("adopt the %s storage: %w", qualified, err)
	}

	return adoptStore(registered)
}

// adoptStore declares one storage registration as a resource type. It is split
// from the lookup so a caller holding a registration can adopt it directly.
func adoptStore(store storageapi.Registration) (registry.Type, error) {
	return adopt(ref.KindStorage, store.Qualified(), store.Descriptor(),
		func(_ context.Context, _ host.Handle, spec registry.Spec) (storageapi.Provider, error) {
			settings, decodeErr := storageSpecOf(spec)
			if decodeErr != nil {
				return nil, decodeErr
			}

			return store.Build(settings)
		})
}

// adopt declares one adapter as a resource type, and refuses one whose name the
// resource plane cannot reproduce.
//
// registry.Declare mints a descriptor for the package that calls it, so the
// name it returns is this module's whatever module registered the adapter. A
// core/* name is declared again here and the two names are compared. A
// mismatch is refused with the module the entry came from.
//
// An adapter from another module has a descriptor this package cannot mint, so
// declaring it again would always fail. It is adopted with the descriptor the
// host already derived for it.
func adopt[T any](
	kind ref.Kind, qualified string, descriptor extension.Descriptor,
	construct func(context.Context, host.Handle, registry.Spec) (T, error),
) (registry.Type, error) {
	if !descriptor.Namespace().IsCore() {
		adopted, err := registry.Adopt(kind, descriptor, construct)
		if err != nil {
			return registry.Type{}, fmt.Errorf("adopt %s %s: %w", kind, qualified, err)
		}

		return adopted, nil
	}

	// The declaration below is a check and not the result. Its doc is the
	// author's with the contract replaced by registry.Contract.
	// registry.Declare validates the contract it is handed against
	// registry.Contract, and the one the author wrote names the adapter's own
	// SDK contract, for example sdk/eventsapi 1.1.0. That contract was checked
	// where the adapter registered.
	check := descriptor.Doc()
	check.Contract = registry.Contract

	declared, err := registry.Declare(kind, descriptor.Name(), check, construct)
	if err != nil {
		return registry.Type{}, fmt.Errorf("declare %s %s: %w", kind, qualified, err)
	}

	if declared.Qualified() != qualified {
		return registry.Type{}, fmt.Errorf("%w: %s is registered by %q and the resource plane can only "+
			"declare it as %s", ErrForeignNamespace, qualified, descriptor.Module(), declared.Qualified())
	}

	// The author's own descriptor is kept, so the published reference says
	// which version of which SDK contract this adapter was built against.
	adopted, err := registry.Adopt(kind, descriptor, construct)
	if err != nil {
		return registry.Type{}, fmt.Errorf("adopt %s %s: %w", kind, qualified, err)
	}

	return adopted, nil
}

// storageSettings is what an operator writes under a storage instance's config
// block. The keys are the host's, because storageapi.ProviderSpec names the
// root, the label and the upload allowlist itself and a disk-backed store reads
// no private block.
type storageSettings struct {
	// Path is the directory the instance owns.
	Path string `json:"path"`
	// Name is the label a UI shows. An empty one takes the adapter's default.
	Name string `json:"name"`
	// AllowedExtensions is the per-instance upload allowlist. An absent key
	// takes the contract's default list; an explicit empty list allows any
	// extension.
	AllowedExtensions []string `json:"allowed_extensions"`
}

// storageSpecOf reads one instance's settings as the storage contract takes
// them. An unknown key is refused: a typo that left an allowlist unset is a
// permission the operator thinks they set.
func storageSpecOf(spec registry.Spec) (storageapi.ProviderSpec, error) {
	settings := storageSettings{}

	if len(bytes.TrimSpace(spec.Config)) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(spec.Config))
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(&settings); err != nil {
			return storageapi.ProviderSpec{}, fmt.Errorf("%w: %s: %w", ErrAdapterConfig, spec.Ref, err)
		}
	}

	// A missing path is not refused here. A disk-backed store refuses an empty
	// path itself, and a store that keeps its objects in memory or at a remote
	// has no directory.

	return storageapi.ProviderSpec{
		ID:                spec.Ref.ID(),
		Type:              spec.Type,
		Name:              settings.Name,
		Path:              settings.Path,
		AllowedExtensions: settings.AllowedExtensions,
	}, nil
}

// openSettings reads one instance's settings as the open map the sink and
// identity contracts take. The host names no key inside it, so the adapter
// refuses an unknown one.
func openSettings(spec registry.Spec) (map[string]any, error) {
	if len(bytes.TrimSpace(spec.Config)) == 0 {
		return map[string]any{}, nil
	}

	var settings map[string]any
	if err := json.Unmarshal(spec.Config, &settings); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrAdapterConfig, spec.Ref, err)
	}

	return settings, nil
}
