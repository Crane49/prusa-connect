package build_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/build"
)

// TestTheShippedAirlockLoadsAndCoversTheShippedTypes is the assertion that
// stops this suite passing on an empty file. Every other case here builds its
// own catalogue, and a catalogue that maps nothing agrees with every one of
// them.
func TestTheShippedAirlockLoadsAndCoversTheShippedTypes(t *testing.T) {
	t.Parallel()

	catalogue := shippedCatalogue(t)
	require.False(t, catalogue.IsZero())

	for _, declared := range []struct{ kind, name string }{
		{"storage", "filesystem"},
		{"storage", "core/filesystem"},
		{"sink", "mqtt"},
		{"sink", "webhook"},
		{"driver", "echo"},
		{"driver", "xbuddy"},
	} {
		_, held := catalogue.TagForType(declared.kind, declared.name)
		require.True(t, held, "the airlock does not carry %s %s", declared.kind, declared.name)
	}

	_, held := catalogue.TagForType("storage", "acme/s3")
	require.False(t, held, "the airlock carries a type this tree does not")
}

// TestAMissingAirlockIsAnError keeps a deleted or moved file from reading as a
// configuration that asked for no optional tags.
func TestAMissingAirlockIsAnError(t *testing.T) {
	t.Parallel()

	_, err := build.LoadCatalogue(filepath.Join(t.TempDir(), "absent.json"))
	require.ErrorIs(t, err, build.ErrCatalogue)
	require.Contains(t, err.Error(), "absent.json")
}

// TestAnAirlockThatMapsNothingIsRefused is the same refusal one step in: a file
// that parses and carries no entry produces the same empty output as a file
// that is not there.
func TestAnAirlockThatMapsNothingIsRefused(t *testing.T) {
	t.Parallel()

	_, err := build.ParseCatalogue("fixture", []byte(`{"version": 1, "resources": [], "conditions": []}`))
	require.ErrorIs(t, err, build.ErrCatalogue)
	require.Contains(t, err.Error(), "maps nothing")
}

// TestAnAirlockOfAnotherFormatIsRefused keeps a newer file from being read as
// though it were this one, which would silently drop whatever it added.
func TestAnAirlockOfAnotherFormatIsRefused(t *testing.T) {
	t.Parallel()

	_, err := build.ParseCatalogue("fixture", []byte(`{"version": 2, "conditions": [
	  {"condition": "mqtt.enabled", "tag": "mqtt", "minimal": false}]}`))
	require.ErrorIs(t, err, build.ErrCatalogue)
	require.Contains(t, err.Error(), "format 2")
}

// TestAnUnknownKeyInTheAirlockIsRefused catches the typo that would otherwise
// leave a tag unmapped with no message.
func TestAnUnknownKeyInTheAirlockIsRefused(t *testing.T) {
	t.Parallel()

	_, err := build.ParseCatalogue("fixture", []byte(`{"version": 1, "conditions": [
	  {"condition": "mqtt.enabled", "tags": "mqtt"}]}`))
	require.ErrorIs(t, err, build.ErrCatalogue)
}

// TestADuplicateEntryIsRefused keeps one type or one condition from having two
// answers, where whichever came last would win by accident.
func TestADuplicateEntryIsRefused(t *testing.T) {
	t.Parallel()

	_, err := build.ParseCatalogue("fixture", []byte(`{"version": 1, "resources": [
	  {"kind": "driver", "type": "core/serial", "tag": "a"},
	  {"kind": "driver", "type": "core/serial", "tag": "b"}], "conditions": []}`))
	require.ErrorIs(t, err, build.ErrCatalogue)
	require.Contains(t, err.Error(), "twice")

	_, err = build.ParseCatalogue("fixture", []byte(`{"version": 1, "resources": [], "conditions": [
	  {"condition": "mqtt.enabled", "tag": "a"},
	  {"condition": "mqtt.enabled", "tag": "b"}]}`))
	require.ErrorIs(t, err, build.ErrCatalogue)
	require.Contains(t, err.Error(), "twice")
}

// TestABareNameTwoNamespacesAnswerResolvesToNothing keeps a guess out of the
// compiler's arguments. Two authors with one name is the registry's ambiguity
// rule, and picking one here would compile whichever the file listed first.
func TestABareNameTwoNamespacesAnswerResolvesToNothing(t *testing.T) {
	t.Parallel()

	catalogue, err := build.ParseCatalogue("fixture", []byte(`{"version": 1, "resources": [
	  {"kind": "driver", "type": "core/serial", "tag": "a"},
	  {"kind": "driver", "type": "acme/serial", "tag": "b"}], "conditions": []}`))
	require.NoError(t, err)

	_, held := catalogue.TagForType("driver", "serial")
	require.False(t, held)

	tag, held := catalogue.TagForType("driver", "core/serial")
	require.True(t, held)
	require.Equal(t, "a", tag)
}
