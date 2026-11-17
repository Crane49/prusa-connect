package profile_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/printer/profile"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// profilesJSON is the same bytes the package embeds, read from the file beside
// it, so a benchmark can price one parse against one Builtin call.
func profilesJSON(tb testing.TB) []byte {
	tb.Helper()

	data, err := os.ReadFile("profiles.json")
	require.NoError(tb, err)

	return data
}

// TestTwoCallersGetTheirOwnRegistry is the property the cache must not cost.
// A resolver, the simulator's catalog and the vocabulary endpoint each hold a
// registry for the life of the process, and a Registry is a slice of profiles
// with a capability map inside each one, so a shared value is state any of them
// can write through.
func TestTwoCallersGetTheirOwnRegistry(t *testing.T) {
	t.Parallel()

	first, err := profile.Builtin()
	require.NoError(t, err)
	require.NotEmpty(t, first.Profiles)

	second, err := profile.Builtin()
	require.NoError(t, err)

	model := second.Profiles[0].Model
	keys := second.Profiles[0].Capabilities.Keys()
	require.NotEmpty(t, keys, "the first profile carries no capability, so writing into it proves nothing")

	original := second.Profiles[0].Capabilities[keys[0]]

	first.Profiles[0].Model = "written by another caller"
	first.Profiles[0].Capabilities[keys[0]] = connectorapi.Value{}
	delete(first.Profiles[0].Capabilities, keys[len(keys)-1])

	require.Equal(t, model, second.Profiles[0].Model,
		"one caller's write reached another caller's registry")
	require.Equal(t, original, second.Profiles[0].Capabilities[keys[0]],
		"one caller's write reached another caller's capability set")
	require.Contains(t, second.Profiles[0].Capabilities, keys[len(keys)-1],
		"one caller's delete reached another caller's capability set")

	third, err := profile.Builtin()
	require.NoError(t, err)
	require.Equal(t, model, third.Profiles[0].Model,
		"a write reached the registry every later caller is copied from")
}

// TestTheEmbeddedProfilesAreParsedOnce holds the cache itself. Decoding and
// validating the file is the expensive half and it does not depend on who is
// asking, so a second call copies rather than parses. The bound is a tenth of
// what one parse allocates rather than a number: measured here, a parse is 4,921
// allocations and a copy is 53.
func TestTheEmbeddedProfilesAreParsedOnce(t *testing.T) {
	data := profilesJSON(t)

	parse := testing.AllocsPerRun(3, func() {
		if _, err := profile.Load(data); err != nil {
			t.Fatal(err)
		}
	})

	copied := testing.AllocsPerRun(3, func() {
		if _, err := profile.Builtin(); err != nil {
			t.Fatal(err)
		}
	})

	require.Less(t, copied, parse/10,
		"Builtin allocates like a parse, so the embedded file is being read on every call")
}

func BenchmarkBuiltin(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		registry, err := profile.Builtin()
		if err != nil {
			b.Fatal(err)
		}

		if len(registry.Profiles) == 0 {
			b.Fatal("the builtin registry is empty, so this measures nothing")
		}
	}
}

func BenchmarkLoad(b *testing.B) {
	data := profilesJSON(b)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		registry, err := profile.Load(data)
		if err != nil {
			b.Fatal(err)
		}

		if len(registry.Profiles) == 0 {
			b.Fatal("the parsed registry is empty, so this measures nothing")
		}
	}
}
