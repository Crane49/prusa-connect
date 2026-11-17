package builtin_test

import (
	"encoding/json"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/builtin"
	"github.com/prusa3d/prusa-connect/internal/extension/host"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/authapi"
	"github.com/prusa3d/prusa-connect/sdk/eventsapi"
)

// linked is what an operator's resources block can name in this artefact,
// written out rather than read from the registry it is checking. A test that
// asked the registry what it holds and then agreed with the answer would pass
// against a binary that links no adapter at all, which is the defect this file
// exists for.
//
// cmd/prusa-connect's TestTheShippingArtefactLinksTheAdaptersItPublishes
// holds the same list against the linker. An adapter added to the composition
// root belongs in both, in the same change.
//
//nolint:gochecknoglobals // the expectation this file is written against.
var linked = []struct {
	kind      ref.Kind
	name      string
	qualified string
}{
	{ref.KindStorage, "filesystem", "core/filesystem"},
	{ref.KindStorage, "local", "core/local"},
	{ref.KindStorage, "temporary", "core/temporary"},
	{ref.KindSink, "amqp", "core/amqp"},
	{ref.KindSink, "kafka", "core/kafka"},
	{ref.KindSink, "mqtt", "core/mqtt"},
	{ref.KindSink, "webhook", "core/webhook"},
	{ref.KindAuth, "oidc", "core/oidc"},
	{ref.KindAuth, "prusa-account", "core/prusa-account"},
}

// TestEveryLinkedAdapterIsDeclarable is the property an operator depends on:
// the type name they write under resources: resolves to something this build
// can construct. It fails when a blank import in adapters.go is removed,
// because the name then answers to nothing.
func TestEveryLinkedAdapterIsDeclarable(t *testing.T) {
	t.Parallel()

	types, err := builtin.Registry()
	require.NoError(t, err)

	for _, adapter := range linked {
		t.Run(adapter.kind.String()+"/"+adapter.qualified, func(t *testing.T) {
			t.Parallel()

			qualified, lookupErr := types.Lookup(adapter.kind, adapter.qualified)
			require.NoError(t, lookupErr)
			require.Equal(t, adapter.qualified, qualified.Qualified())

			bare, bareErr := types.Lookup(adapter.kind, adapter.name)
			require.NoError(t, bareErr)
			require.Equal(t, adapter.qualified, bare.Qualified(),
				"a bare name is what an operator writes, so it has to reach the same type")
		})
	}
}

// TestTheAdapterKeepsTheDescriptorItsAuthorWrote holds the bridge to the
// documentation the published reference carries. A type declared here with a
// descriptor of the composition root's own invention would publish one thing
// and run another.
func TestTheAdapterKeepsTheDescriptorItsAuthorWrote(t *testing.T) {
	t.Parallel()

	types, err := builtin.Registry()
	require.NoError(t, err)

	found, err := types.Lookup(ref.KindSink, "core/webhook")
	require.NoError(t, err)

	published := found.Descriptor().Doc()
	require.Equal(t, eventsapi.Contract, published.Contract)
	require.NotEmpty(
		t,
		published.Publishes,
		"the webhook sink publishes to its own transport and the reference lists it",
	)
	require.NotEmpty(t, published.Options, "the webhook sink reads config keys and the reference lists them")

	identity, err := types.Lookup(ref.KindAuth, "core/oidc")
	require.NoError(t, err)
	require.Equal(t, authapi.Contract, identity.Descriptor().Doc().Contract)
	require.NotEmpty(t, identity.Descriptor().Doc().Options)
}

// TestAConfiguredAdapterBecomesAnInstance is the other half: a name that
// resolves is worth nothing until the settings behind it build something. Each
// case is the smallest configuration its adapter accepts.
func TestAConfiguredAdapterBecomesAnInstance(t *testing.T) {
	t.Parallel()

	dir := filepath.ToSlash(t.TempDir())

	cases := map[string]struct {
		kind   ref.Kind
		typ    string
		config string
	}{
		"a webhook sink takes an endpoint": {
			kind: ref.KindSink, typ: "webhook", config: `{"url":"http://127.0.0.1:9/hook"}`,
		},
		"an mqtt sink takes a broker it has not reached yet": {
			kind: ref.KindSink, typ: "mqtt", config: `{"url":"tcp://127.0.0.1:1883","client_id":"proof"}`,
		},
		"an amqp sink takes a broker it has not reached yet": {
			kind: ref.KindSink, typ: "amqp", config: `{"url":"amqp://guest:guest@127.0.0.1:5672/"}`,
		},
		"a kafka sink takes a seed broker": {
			kind: ref.KindSink, typ: "kafka", config: `{"brokers":["127.0.0.1:9092"],"topic":"proof"}`,
		},
		"an identity provider takes an issuer it does not contact": {
			kind: ref.KindAuth, typ: "oidc",
			config: `{"issuer":"https://issuer.invalid","client_id":"prusa-connect"}`,
		},
		"the built-in storage type is still reachable beside them": {
			kind: ref.KindStorage, typ: "filesystem", config: `{"root":"` + dir + `/built-in"}`,
		},
		"a local disk store takes a path": {
			kind: ref.KindStorage, typ: "local", config: `{"path":"` + dir + `/disk"}`,
		},
		"a temporary store takes a path": {
			kind: ref.KindStorage, typ: "temporary", config: `{"path":"` + dir + `/staging"}`,
		},
	}

	types, err := builtin.Registry()
	require.NoError(t, err)

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			instance, refErr := ref.New(tc.kind, "proof")
			require.NoError(t, refErr)

			built, buildErr := registry.Construct[any](t.Context(), types, tc.kind, host.Discard(),
				registry.Spec{Ref: instance, Type: tc.typ, Config: json.RawMessage(tc.config)})
			require.NoError(t, buildErr)
			require.NotNil(t, built)

			if closer, closes := built.(io.Closer); closes {
				require.NoError(t, closer.Close())
			}
		})
	}
}

// TestTheContractInterfaceIsWhatTheAdapterBuilds is what the supervision
// wrappers need. internal/extension/supervised asks the registry for an
// eventsapi.Processor by name, so a bridge that built anything else would fail
// at the type assertion rather than at construction.
func TestTheContractInterfaceIsWhatTheAdapterBuilds(t *testing.T) {
	t.Parallel()

	types, err := builtin.Registry()
	require.NoError(t, err)

	sink, err := ref.New(ref.KindSink, "proof")
	require.NoError(t, err)

	processor, err := registry.Construct[eventsapi.Processor](t.Context(), types, ref.KindSink, host.Discard(),
		registry.Spec{Ref: sink, Type: "webhook", Config: json.RawMessage(`{"url":"http://127.0.0.1:9/hook"}`)})
	require.NoError(t, err)
	require.NoError(t, processor.Close())

	identity, err := ref.New(ref.KindAuth, "proof")
	require.NoError(t, err)

	provider, err := registry.Construct[authapi.IdentityProvider](t.Context(), types, ref.KindAuth, host.Discard(),
		registry.Spec{Ref: identity, Type: "oidc", Config: json.RawMessage(
			`{"issuer":"https://issuer.invalid","client_id":"proof"}`)})
	require.NoError(t, err)
	require.True(t, provider.Capabilities().Redirect)
}

// TestAnUnknownTypeNamesWhatThisBuildCarries is the failure an operator meets
// when they name a type this artefact was not built with. It has to say what is
// here, because the answer to an unknown name is a list.
func TestAnUnknownTypeNamesWhatThisBuildCarries(t *testing.T) {
	t.Parallel()

	types, err := builtin.Registry()
	require.NoError(t, err)

	_, err = types.Lookup(ref.KindSink, "nats")
	require.ErrorIs(t, err, registry.ErrUnknownType)
	require.Contains(t, err.Error(), `"nats"`)
	require.Contains(t, err.Error(), "core/mqtt")
	require.Contains(t, err.Error(), "core/webhook")
}

// TestASinkSettingsBlockIsRefusedByItsAdapter holds the split: the host names
// no key inside a sink's or a provider's config, so the refusal is the
// adapter's and it reaches the operator through the bridge unchanged.
func TestASinkSettingsBlockIsRefusedByItsAdapter(t *testing.T) {
	t.Parallel()

	types, err := builtin.Registry()
	require.NoError(t, err)

	cases := map[string]struct {
		kind    ref.Kind
		typ     string
		config  string
		refused string
	}{
		"a webhook with no endpoint is refused":          {ref.KindSink, "webhook", `{}`, "url is required"},
		"an mqtt sink with no broker is refused":         {ref.KindSink, "mqtt", `{}`, "url is required"},
		"an amqp sink with no broker is refused":         {ref.KindSink, "amqp", `{}`, "url is required"},
		"a kafka sink with no brokers is refused":        {ref.KindSink, "kafka", `{}`, "brokers"},
		"an identity provider with no issuer is refused": {ref.KindAuth, "oidc", `{}`, "issuer"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			instance, refErr := ref.New(tc.kind, "proof")
			require.NoError(t, refErr)

			_, buildErr := registry.Construct[any](t.Context(), types, tc.kind, host.Discard(),
				registry.Spec{Ref: instance, Type: tc.typ, Config: json.RawMessage(tc.config)})
			require.Error(t, buildErr)
			require.Contains(t, buildErr.Error(), tc.refused)
		})
	}
}
