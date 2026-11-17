package resourcehost

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/internal/extension/host"
	"github.com/prusa3d/prusa-connect/internal/extension/registry"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/sdk/extension"
)

// brokerDoc is a compiled-in type that declares one required address, one
// optional credential, one nested block and one list.
func brokerDoc() extension.Doc {
	return extension.Doc{
		Summary:     "A resource type used only by this file's cases.",
		Description: "It declares settings so the check against them has something to check.",
		Version:     "1.0.0",
		Contract:    registry.Contract,
		Stability:   extension.StabilityExperimental,
		Options: []extension.Option{
			{Key: "url", Type: extension.OptionURL, Required: true, Summary: "The broker."},
			{Key: "password", Type: extension.OptionSecret, Summary: "The broker password."},
			{Key: "tls", Type: extension.OptionObject, Summary: "The transport settings."},
			{Key: "routes", Type: extension.OptionList, Summary: "The rewrite rules."},
		},
	}
}

// brokerRegistry is a registry holding one sink type, declared as the
// application declares one.
func brokerRegistry(t *testing.T) *registry.Registry {
	t.Helper()

	declared, err := registry.Declare(ref.KindSink, "broker", brokerDoc(),
		func(context.Context, host.Handle, registry.Spec) (*struct{}, error) { return &struct{}{}, nil })
	if err != nil {
		t.Fatalf("declare the type: %v", err)
	}

	types, err := registry.NewFrom(declared)
	if err != nil {
		t.Fatalf("build the registry: %v", err)
	}

	return types
}

// TestABuiltinsBlockIsHeldToWhatItsTypeDeclares is the rule a module's block is
// held to, asked of a type this binary compiles in.
func TestABuiltinsBlockIsHeldToWhatItsTypeDeclares(t *testing.T) {
	t.Parallel()

	types := brokerRegistry(t)

	cases := map[string]struct {
		block string
		want  error
		names string
	}{
		"a key the type does not declare": {
			block: `{"url":"tcp://b:1883","passwrod":"x"}`,
			want:  ErrSettingsUnknown,
			names: `"passwrod"`,
		},
		"a required key nobody wrote": {
			block: `{"password":"x"}`,
			want:  ErrSettingsMissing,
			names: `"url"`,
		},
		"a block where one value belongs": {
			block: `{"url":{"host":"b"}}`,
			want:  ErrSettingsShape,
			names: `"url"`,
		},
		"one value where a block belongs": {
			block: `{"url":"tcp://b:1883","tls":"on"}`,
			want:  ErrSettingsShape,
			names: `"tls"`,
		},
		"a block that is not a mapping": {
			block: `["url"]`,
			want:  ErrSettingsShape,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := builtinSettings(types, nil, ref.KindSink, "broker", json.RawMessage(tc.block))
			if !errors.Is(err, tc.want) {
				t.Fatalf("the refusal is %v, want %v", err, tc.want)
			}

			if tc.names != "" && !strings.Contains(err.Error(), tc.names) {
				t.Errorf("the refusal does not name %s: %v", tc.names, err)
			}
		})
	}
}

// TestABuiltinsBlockTheTypeReadsIsAccepted: every key declared, the required one
// written, and a list and a nested block in the shapes their types say.
func TestABuiltinsBlockTheTypeReadsIsAccepted(t *testing.T) {
	t.Parallel()

	block := `{"url":"tcp://b:1883","password":"x","tls":{"skip_verify":true},"routes":[{"to":"a"}]}`

	if err := builtinSettings(brokerRegistry(t), nil, ref.KindSink, "broker", json.RawMessage(block)); err != nil {
		t.Fatalf("a block the type reads was refused: %v", err)
	}
}

// TestAKeyTheHostReadsIsLegalInEveryBlockOfItsKind: a camera's printer binding
// is the deployment's decision and no driver declares it, so the check is given
// the host's own keys beside the type's.
func TestAKeyTheHostReadsIsLegalInEveryBlockOfItsKind(t *testing.T) {
	t.Parallel()

	hostKeys := []extension.Option{
		{Key: "printer", Type: extension.OptionString, Summary: "The printer this camera watches."},
	}
	block := json.RawMessage(`{"url":"tcp://b:1883","printer":"p1"}`)

	if err := builtinSettings(brokerRegistry(t), hostKeys, ref.KindSink, "broker", block); err != nil {
		t.Fatalf("a key the host reads was refused: %v", err)
	}

	if err := builtinSettings(brokerRegistry(t), nil, ref.KindSink, "broker", block); !errors.Is(
		err, ErrSettingsUnknown,
	) {
		t.Fatalf("the refusal without the host's keys is %v, want the one for an undeclared key", err)
	}
}

// TestATypeThisBuildDoesNotHaveIsLeftToTheRegistry: the lookup that resolves the
// type is the one that reports an unknown name, so this check does not answer
// the same question in words of its own.
func TestATypeThisBuildDoesNotHaveIsLeftToTheRegistry(t *testing.T) {
	t.Parallel()

	err := builtinSettings(brokerRegistry(t), nil, ref.KindSink, "nothing", json.RawMessage(`{"url":"x"}`))
	if err != nil {
		t.Fatalf("an unknown type was refused here: %v", err)
	}
}
