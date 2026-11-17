package hostfeature_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/kernel/hostfeature"
)

// Every declared key must use the syntax sdk/connectorapi gives a capability
// key. A key the manifest gate would refuse is one no module could name, so
// declaring it promises nothing.
func TestEveryDeclaredKeyIsOneAManifestCanWrite(t *testing.T) {
	t.Parallel()

	declared := hostfeature.Declared()
	require.NotEmpty(t, declared, "the host declares no feature at all, so this gate reads nothing")

	seen := map[string]bool{}

	for _, feature := range declared {
		require.Truef(t, feature.Key.Valid(), "%s is not a capability key", feature.Key)
		require.Truef(t, feature.Key.IsCore(), "%s is not in the core namespace", feature.Key)
		require.NotEmptyf(t, feature.Summary, "%s says nothing about what it promises", feature.Key)
		require.NotEmptyf(t, feature.Builds, "%s does not say which builds carry it", feature.Key)
		require.Falsef(t, seen[feature.Key.String()], "%s is declared twice", feature.Key)

		seen[feature.Key.String()] = true
	}
}

// A build with no browser application has no slots to fill, so a module that
// needs them is told this host has none instead of being installed against
// nothing.
func TestTheUIFeatureIsTheFullBuildsAlone(t *testing.T) {
	t.Parallel()

	var found bool

	for _, feature := range hostfeature.Declared() {
		if feature.Key == hostfeature.UI {
			found = true

			require.True(t, feature.FullBuildOnly, "core.feature.ui is declared for every build")
		}
	}

	require.True(t, found, "core.feature.ui is not in the declared set")
}

// The accessors stay separate: the page describes the product, and the running
// server describes itself.
func TestWhatThisBuildAdvertisesIsASubsetOfWhatIsDeclared(t *testing.T) {
	t.Parallel()

	declared := map[string]hostfeature.Feature{}
	for _, feature := range hostfeature.Declared() {
		declared[feature.Key.String()] = feature
	}

	for _, feature := range hostfeature.Advertised() {
		match, known := declared[feature.Key.String()]
		require.Truef(t, known, "%s is advertised and not declared", feature.Key)
		require.Equal(t, match, feature)
	}
}

// The table is a package variable. An accessor returning the slice itself would
// let one caller edit what every other caller reads.
func TestTheDeclaredTableIsNotWritableThroughItsAccessor(t *testing.T) {
	t.Parallel()

	first := hostfeature.Declared()
	first[0].Key = "core.feature.nothing"

	require.NotEqual(t, first[0].Key, hostfeature.Declared()[0].Key)
}
