package app

import (
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
	sdkextension "github.com/prusa3d/prusa-connect/sdk/extension"
)

// A core upgrade resolves its module releases the way an install does
// (ADR-20261027-e6k rule 3a). The bundle's releases go through the resolver:
// one whose require the server cannot meet refuses the upgrade, and the ones
// that fit are installed in the order their requires put them.
func TestACoreUpgradeResolvesTheReleasesItBrings(t *testing.T) {
	if testing.Short() {
		t.Skip("builds three extension binaries")
	}
	t.Parallel()

	pids := t.TempDir()
	oldSpools := upgradeReleaseNamed(t, "spools", "1.0.0", pids+"/spools-old")
	newSpools := upgradeReleaseNamed(t, "spools", "1.1.0", pids+"/spools-new")
	newQueue := upgradeReleaseNamed(t, "queue", "1.1.0", pids+"/queue-new")

	spools, err := sdkextension.NamespaceOf("example.test/spools")
	require.NoError(t, err)

	deployment := aLiveDeployment(t)
	_, err = deployment.installer.Install(t.Context(), deployment.principal, httpapi.ModuleRelease{
		Module: "example.test/spools", Version: "1.0.0", From: oldSpools, Approve: true,
	})
	require.NoError(t, err)

	holder := &maintenance{configPath: deployment.installer.configPath, core: deployment.installer.core}
	journal := func(extensions ...upgrade.StagedExtension) upgrade.Journal {
		return upgrade.Journal{ToVersion: "1.1.0", StagedBinary: t.TempDir() + "/core", Extensions: extensions}
	}

	t.Run("a bundled release that requires what nobody has refuses the plan", func(t *testing.T) {
		lonely := copyRelease(t, newQueue)
		appendManifest(t, lonely, "require:\n  com.example.missing: \"^1\"\n")

		planned, planErr := holder.planModules(t.Context(), journal(
			testStagedExtension(t, "example.test/queue", "queue", anArchiveOf(t, lonely)),
		))
		require.NoError(t, planErr)
		require.Error(t, planned.refused, "the bundle's unmet require was planned")
	})

	t.Run("the releases are installed in the order their requires put them", func(t *testing.T) {
		queue := copyRelease(t, newQueue)
		appendManifest(t, queue, "require:\n  "+spools.String()+": \"^1.1\"\n")

		// The bundle lists queue first, and queue needs the bundled spools.
		bundle := journal(
			testStagedExtension(t, "example.test/queue", "queue", anArchiveOf(t, queue)),
			testStagedExtension(t, "example.test/spools", "spools", anArchiveOf(t, newSpools)),
		)

		planned, planErr := holder.planModules(t.Context(), bundle)
		require.NoError(t, planErr)
		require.NoError(t, planned.refused)

		staged, stageErr := holder.stagePlan(t.Context(), bundle, planned, nil)
		require.NoError(t, stageErr)

		order := make([]string, 0, len(staged))
		for _, one := range staged {
			order = append(order, one.Module)
		}
		require.Equal(t, []string{"example.test/spools", "example.test/queue"}, order)
		require.True(t, slices.ContainsFunc(staged, func(one upgrade.StagedExtension) bool {
			return one.Module == "example.test/queue" && one.Constraint == "^1.1.0" && !one.Pulled
		}), "the new module records no root constraint: %+v", staged)
	})
}

// copyRelease is a release directory copied, so one test can change its
// manifest without the built binary being built again.
func copyRelease(t *testing.T, from string) string {
	t.Helper()

	to := t.TempDir() + "/release"
	require.NoError(t, os.CopyFS(to, os.DirFS(from)))

	return to
}
