package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/kernel/ref"
	"github.com/prusa3d/prusa-connect/internal/upgrade"
)

func reviewManifest(version string, permissions ...string) string {
	return strings.Replace(
		strings.Replace(bothHalvesManifest(), "version: 1.0.0", "version: "+version, 1),
		"  - observe\n", "  - "+strings.Join(permissions, "\n  - ")+"\n", 1)
}

// Preparation shows each bundled extension's asks and what is new against the
// installed declaration. Apply refuses a new ask or an older version until
// the operator approves, and grants no more than was shown.
func TestBundledExtensionReviewNamesNewAsksAndDowngrades(t *testing.T) {
	t.Parallel()

	instance, err := ref.New(ref.KindSink, "spools")
	require.NoError(t, err)
	installed := func(version string, asked *install.ModuleAsks) resource.Document {
		declared := resource.Declaration{Ref: instance}
		declared.Spec.Module = &install.ModuleEntry{Version: version, Asked: asked}

		return resource.Document{Instances: []resource.Declaration{declared}}
	}
	staged := func(version string, permissions ...string) upgrade.StagedExtension {
		archive := anArchiveOf(t, aRelease(t, reviewManifest(version, permissions...), true))

		return testStagedExtension(t, theModulePath, "spools", archive)
	}

	same, err := reviewBundledExtension(
		installed("1.0.0", &install.ModuleAsks{Permissions: []string{"observe"}}), "",
		staged("1.1.0", "observe"))
	require.NoError(t, err)
	require.Equal(t, []string{"permission observe"}, same.Asks)
	require.Empty(t, same.NewAsks)
	require.False(t, same.NeedsApproval())

	more, err := reviewBundledExtension(
		installed("1.0.0", &install.ModuleAsks{Permissions: []string{"observe"}}), "",
		staged("1.1.0", "observe", "control"))
	require.NoError(t, err)
	require.Equal(t, []string{"permission control"}, more.NewAsks)

	older, err := reviewBundledExtension(
		installed("2.0.0", &install.ModuleAsks{Permissions: []string{"observe"}}), "",
		staged("1.1.0", "observe"))
	require.NoError(t, err)
	require.True(t, older.Downgrade)
	require.Equal(t, "2.0.0", older.InstalledVersion)

	fresh, err := reviewBundledExtension(resource.Document{}, "", staged("1.1.0", "observe"))
	require.NoError(t, err)
	require.Equal(t, fresh.Asks, fresh.NewAsks)

	for review, permissions := range map[*upgrade.ExtensionReview][]string{
		&more: {"observe", "control"}, &older: {"observe"},
	} {
		journal := upgrade.Journal{ExtensionReviews: []upgrade.ExtensionReview{*review}}
		_, refused := shownByReview(journal, theModulePath)
		require.ErrorIs(t, refused, upgrade.ErrPreflight)

		journal.ExtensionsApproved = true
		shown, allowed := shownByReview(journal, theModulePath)
		require.NoError(t, allowed)
		require.Equal(t, permissions, shown.Permissions)
	}

	_, missing := shownByReview(upgrade.Journal{}, theModulePath)
	require.ErrorIs(t, missing, upgrade.ErrPreflight)
}
