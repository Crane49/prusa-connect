package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/moduledeps"
	"github.com/prusa3d/prusa-connect/internal/app/resource"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpapi"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The checks between an approved plan, the install that carries it and the
// lock it leaves (ADR-20261027-e6k rules 15, 16, 21 and 26).

func TestAPlanIsTheSameOnlyWithTheSameBytes(t *testing.T) {
	t.Parallel()

	steps := []moduledeps.Step{{Release: moduledeps.Release{Module: readerPath, Version: "1.2.0", Digest: "AA"}}}

	require.True(t, samePlan(steps, []httpapi.ModuleReleaseRef{{Module: readerPath, Version: "1.2.0", Sha256: "aa"}}))
	require.False(t, samePlan(steps, []httpapi.ModuleReleaseRef{{Module: readerPath, Version: "1.2.0", Sha256: "bb"}}),
		"the index pointed the same version at other bytes after the plan was read")
	require.False(t, samePlan(steps, []httpapi.ModuleReleaseRef{{Module: readerPath, Version: "1.2.0"}}))
}

func TestAnApprovedPlanThatShowedNothingForAModuleInstallsNothing(t *testing.T) {
	t.Parallel()

	installer, configPath := theInstaller(t, true)
	offerReader(t, installer.core.Modules, "1.2.0")

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: viewerPath, Version: "1.0.0", From: aRelease(t, viewerManifest("^1.0"), false), Approve: true,
		Dependencies: []httpapi.ModuleReleaseRef{{Module: readerPath, Version: "1.2.0"}},
	})
	require.True(t, fault.HasCode(err, httpfault.CodeModuleRequestsChanged()), "got %v", err)

	doc, err := resource.Load(resource.Paths{File: configPath, Overlay: installer.core.Overlay})
	require.NoError(t, err)
	require.Empty(t, doc.Instances, "neither the module the plan adds nor the one it was for is written")
}

func TestAModuleInstalledOutOfRangeIsResolvedAndNotTakenAsMet(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: readerPath, Version: "1.0.0", From: aRelease(t, readerManifest("1.0.0"), false), Approve: true,
	})
	require.NoError(t, err)

	offerReader(t, installer.core.Modules, "1.2.0")

	_, err = installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: viewerPath, Version: "1.0.0", From: aRelease(t, viewerManifest("^1.2"), false), Approve: true,
	})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "now adds nothing",
		"a reader at 1.0.0 does not meet ^1.2, so the install asks the resolver")
}

func TestTheAPISaysSoWhenTheLockIsNotWritten(t *testing.T) {
	t.Parallel()

	installer, _ := theInstaller(t, true)

	// A directory where the lock file goes: the probe beside it succeeds and
	// the write itself fails, which is the failure after the change.
	require.NoError(t, os.MkdirAll(moduledeps.LockPath(installer.core.Overlay), 0o750))

	_, err := installer.Install(t.Context(), "user:operator", httpapi.ModuleRelease{
		Module: readerPath, Version: "1.0.0", From: aRelease(t, readerManifest("1.0.0"), false), Approve: true,
	})
	require.True(t, fault.HasCode(err, httpfault.CodeModuleLockNotWritten()), "got %v", err)
	require.Contains(t, err.Error(), "the change stands")
}

func TestALockedReleaseIsTheSameOnlyWithItsDigest(t *testing.T) {
	t.Parallel()

	held := moduledeps.Installed{Namespace: "com.github.aux.acme.reader", Version: "1.0.0", Digest: "aa"}

	require.True(t, sameLocked(held, moduledeps.LockedModule{Version: "1.0.0", Digest: "AA"}))
	require.True(t, sameLocked(held, moduledeps.LockedModule{Version: "1.0.0"}))
	require.False(t, sameLocked(held, moduledeps.LockedModule{Version: "1.0.0", Digest: "bb"}),
		"the same version from other bytes is installed again")
	require.False(t, sameLocked(held, moduledeps.LockedModule{Version: "1.1.0", Digest: "aa"}))
}

func TestALockedReleaseOnThisServerIsCheckedAgainstItsDigest(t *testing.T) {
	t.Parallel()

	archive := filepath.Join(t.TempDir(), "reader-1.0.0.tar.gz")
	require.NoError(t, os.WriteFile(archive, []byte("release bytes"), 0o600))

	sum := sha256.Sum256([]byte("release bytes"))
	digest := hex.EncodeToString(sum[:])

	require.NoError(t, verifyLockedSource(archive, digest))
	require.NoError(t, verifyLockedSource(archive, ""), "a lock that records no digest has nothing to check")
	require.Error(t, verifyLockedSource(archive, "00"+digest[2:]), "other bytes than the lock records")
	require.Error(t, verifyLockedSource(t.TempDir(), digest), "a directory has no digest to check")
	require.NoError(t, verifyLockedSource("https://example.invalid/r.tar.gz", digest), "a URL is checked when fetched")
}
