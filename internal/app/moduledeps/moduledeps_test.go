package moduledeps_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prusa3d/prusa-connect/internal/app/config/install"
	"github.com/prusa3d/prusa-connect/internal/app/moduledeps"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The resolver of ADR-20261027-e6k, with Composer's semantics.

const prefix = "com.github.aux.acme."

// release is one release of name, requiring what the pairs name.
func release(name, version string, requires ...string) moduledeps.Release {
	held := moduledeps.Release{Module: "github.com/acme/" + name, Namespace: prefix + name, Version: version}

	for index := 0; index+1 < len(requires); index += 2 {
		held.Require = append(
			held.Require,
			install.Dependency{Module: prefix + requires[index], Version: requires[index+1]},
		)
	}

	return held
}

// installed is one module a server has, requiring what the pairs name.
func installed(name, version string, requires ...string) moduledeps.Installed {
	held := moduledeps.Installed{Namespace: prefix + name, Version: version, Constraint: "^" + version}

	for index := 0; index+1 < len(requires); index += 2 {
		held.Require = append(
			held.Require,
			install.Dependency{Module: prefix + requires[index], Version: requires[index+1]},
		)
	}

	return held
}

func versions(plan moduledeps.Plan) string {
	lines := make([]string, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		lines = append(lines, strings.TrimPrefix(step.Release.Namespace, prefix)+"@"+step.Release.Version)
	}

	return strings.Join(lines, ", ")
}

func TestTheNewestReleaseInEveryRangeIsChosen(t *testing.T) {
	t.Parallel()

	// A wants b ^1.2, the installed c wants b ^1.4, and b 2.0.0 fits neither.
	plan, err := moduledeps.Resolve(moduledeps.Request{
		Installed: []moduledeps.Installed{installed("c", "1.0.0", "b", "^1.4")},
		Available: []moduledeps.Release{
			release("b", "1.2.0"), release("b", "1.9.0"), release("b", "2.0.0"), release("b", "1.5.0"),
		},
		Add: []moduledeps.Release{release("a", "1.0.0", "b", "^1.2")},
	})
	require.NoError(t, err)
	require.Equal(t, "b@1.9.0, a@1.0.0", versions(plan))
	require.Equal(t, []string{prefix + "a ^1.2", prefix + "c ^1.4"}, plan.Steps[0].RequiredBy)
	require.True(t, plan.Steps[0].Pulled)
	require.False(t, plan.Steps[1].Pulled)
}

func TestTwoRangesNoReleaseMeetsAreRefused(t *testing.T) {
	t.Parallel()

	_, err := moduledeps.Resolve(moduledeps.Request{
		Installed: []moduledeps.Installed{installed("c", "1.0.0", "b", "^2")},
		Available: []moduledeps.Release{release("b", "1.3.0"), release("b", "2.1.0")},
		Add:       []moduledeps.Release{release("a", "1.0.0", "b", "^1")},
	})
	require.True(t, fault.HasCode(err, httpfault.CodeModuleDependencyConflict()), "got %v", err)
	require.Contains(t, err.Error(), prefix+"c needs "+prefix+"b ^2")
	require.Contains(t, err.Error(), prefix+"a needs "+prefix+"b ^1")

	// An installed module out of the new range is named, to update first.
	_, err = moduledeps.Resolve(moduledeps.Request{
		Installed: []moduledeps.Installed{installed("b", "1.3.0")},
		Add:       []moduledeps.Release{release("a", "1.0.0", "b", "^2")},
	})
	require.True(t, fault.HasCode(err, httpfault.CodeModuleDependencyConflict()), "got %v", err)
	require.Contains(t, err.Error(), prefix+"b is 1.3.0. Update "+prefix+"b first")
}

func TestAPlanBacksOutOfAChoiceALaterOneContradicts(t *testing.T) {
	t.Parallel()

	plan, err := moduledeps.Resolve(moduledeps.Request{
		Available: []moduledeps.Release{
			release("b", "2.0.0", "d", "^3"), release("b", "1.0.0", "d", "^1"), release("d", "1.1.0"),
		},
		Add: []moduledeps.Release{release("a", "1.0.0", "b", ">=1.0")},
	})
	require.NoError(t, err)
	require.Equal(t, "d@1.1.0, b@1.0.0, a@1.0.0", versions(plan))
}

func TestAModuleTheIndexDoesNotOfferIsRefused(t *testing.T) {
	t.Parallel()

	_, err := moduledeps.Resolve(moduledeps.Request{Add: []moduledeps.Release{release("a", "1.0.0", "b", "^1")}})
	require.True(t, fault.HasCode(err, httpfault.CodeModuleDependencyNotOffered()), "got %v", err)
	require.Contains(t, err.Error(), prefix+"a needs "+prefix+"b ^1")
}

func TestTheLockedReleaseIsKeptWhenItFits(t *testing.T) {
	t.Parallel()

	lock := moduledeps.Lock{Modules: []moduledeps.LockedModule{{Namespace: prefix + "b", Version: "1.5.0"}}}
	available := []moduledeps.Release{release("b", "1.9.0"), release("b", "1.5.0")}

	plan, err := moduledeps.Resolve(moduledeps.Request{
		Available: available, Lock: lock, Add: []moduledeps.Release{release("a", "1.0.0", "b", "^1")},
	})
	require.NoError(t, err)
	require.Equal(t, "b@1.5.0, a@1.0.0", versions(plan))

	// An update of b takes the newest one.
	plan, err = moduledeps.Resolve(moduledeps.Request{
		Installed: []moduledeps.Installed{installed("b", "1.5.0")}, Available: available, Lock: lock,
		Update: []string{prefix + "b"},
	})
	require.NoError(t, err)
	require.Equal(t, "b@1.9.0", versions(plan))
	require.Equal(t, "1.5.0", plan.Steps[0].From)
}

func TestStabilityIsHeldToTheMinimumAndTheFlag(t *testing.T) {
	t.Parallel()

	available := []moduledeps.Release{release("b", "1.0.0"), release("b", "1.1.0-beta2")}

	plan, err := moduledeps.Resolve(moduledeps.Request{
		Available: available, Add: []moduledeps.Release{release("a", "1.0.0", "b", "^1")},
	})
	require.NoError(t, err)
	require.Equal(t, "b@1.0.0, a@1.0.0", versions(plan), "a beta was taken under minimum-stability stable")

	plan, err = moduledeps.Resolve(moduledeps.Request{
		Available: available, Add: []moduledeps.Release{release("a", "1.0.0", "b", "^1@beta")},
	})
	require.NoError(t, err)
	require.Equal(t, "b@1.1.0-beta2, a@1.0.0", versions(plan), "the @beta flag did not take the beta")

	plan, err = moduledeps.Resolve(moduledeps.Request{
		Available: available, Add: []moduledeps.Release{release("a", "1.0.0", "b", "^1")},
		Settings: moduledeps.Settings{MinimumStability: "beta", PreferStable: true},
	})
	require.NoError(t, err)
	require.Equal(t, "b@1.0.0, a@1.0.0", versions(plan), "prefer-stable did not prefer the stable release")

	beta := release("b", "1.2.0")
	beta.Channel = "beta"
	require.Equal(t, "beta", beta.Stability().String(), "a release on the beta channel is not beta")
}

func TestAConflictRefusesARelease(t *testing.T) {
	t.Parallel()

	old := installed("c", "1.0.0")
	old.Conflict = []install.Dependency{{Module: prefix + "b", Version: "<1.5"}}

	plan, err := moduledeps.Resolve(moduledeps.Request{
		Installed: []moduledeps.Installed{old},
		Available: []moduledeps.Release{release("b", "1.4.0"), release("b", "1.6.0")},
		Add:       []moduledeps.Release{release("a", "1.0.0", "b", "^1")},
	})
	require.NoError(t, err)
	require.Equal(t, "b@1.6.0, a@1.0.0", versions(plan))

	_, err = moduledeps.Resolve(moduledeps.Request{
		Installed: []moduledeps.Installed{old},
		Available: []moduledeps.Release{release("b", "1.4.0")},
		Add:       []moduledeps.Release{release("a", "1.0.0", "b", "^1")},
	})
	require.True(t, fault.HasCode(err, httpfault.CodeModuleDependencyConflict()), "got %v", err)
}

func TestTheCoreIsAPlatformPackage(t *testing.T) {
	t.Parallel()

	newer := release("b", "2.0.0")
	newer.Core = ">=2.0"

	plan, err := moduledeps.Resolve(moduledeps.Request{
		Available: []moduledeps.Release{release("b", "1.0.0"), newer},
		Add:       []moduledeps.Release{release("a", "1.0.0", "b", "*")},
		Settings:  moduledeps.Settings{Core: "1.5.0"},
	})
	require.NoError(t, err)
	require.Equal(t, "b@1.0.0, a@1.0.0", versions(plan), "a release the core cannot run was taken")
}

func TestACoreUpgradeUpdatesTheModulesItBreaks(t *testing.T) {
	t.Parallel()

	b := installed("b", "1.0.0")
	b.Core = "^1.0"
	a := installed("a", "1.0.0", "b", "^1")
	free := installed("c", "1.0.0")

	nextB := release("b", "1.1.0")
	nextB.Core = "^2.0"
	majorB := release("b", "2.0.0")
	majorB.Core = "^2.0"

	plan, err := moduledeps.CoreImpact([]moduledeps.Installed{a, b, free},
		[]moduledeps.Release{nextB, majorB, release("a", "1.0.0", "b", "^1"), release("a", "2.0.0", "b", "^2")},
		moduledeps.Lock{}, "2.0.0", moduledeps.Settings{})
	require.NoError(t, err)
	require.Equal(t, "b@1.1.0", versions(plan), "a kept its release and b took the newest one that fits")

	_, err = moduledeps.CoreImpact([]moduledeps.Installed{a, b}, []moduledeps.Release{release("a", "1.0.0", "b", "^1")},
		moduledeps.Lock{}, "2.0.0", moduledeps.Settings{})
	require.True(t, fault.HasCode(err, httpfault.CodeModuleDependencyNotOffered()) ||
		fault.HasCode(err, httpfault.CodeModuleDependencyConflict()), "got %v", err)

	plan, err = moduledeps.CoreImpact([]moduledeps.Installed{free}, nil, moduledeps.Lock{}, "2.0.0",
		moduledeps.Settings{})
	require.NoError(t, err)
	require.Empty(t, plan.Steps)
}

// TestACoreUpgradeTakesTheReleasesItBrings is the offline bundle: its
// releases are added with the candidate core, and a bundled release whose
// require the server cannot meet refuses the whole upgrade.
func TestACoreUpgradeTakesTheReleasesItBrings(t *testing.T) {
	t.Parallel()

	b := installed("b", "1.0.0")
	b.Core = "^1.0"
	a := installed("a", "1.0.0", "b", "^1")

	bundledB := release("b", "1.2.0")
	bundledB.Core = "^2.0"

	plan, err := moduledeps.CoreUpgrade(moduledeps.Request{
		Installed: []moduledeps.Installed{a, b},
		Available: []moduledeps.Release{bundledB},
		Add:       []moduledeps.Release{bundledB},
	}, "2.0.0")
	require.NoError(t, err)
	require.Equal(t, "b@1.2.0", versions(plan), "the bundled b replaced the one the core broke")

	needsC := release("d", "1.0.0", "c", "^1")
	_, err = moduledeps.CoreUpgrade(moduledeps.Request{
		Installed: []moduledeps.Installed{a, b},
		Available: []moduledeps.Release{bundledB, needsC},
		Add:       []moduledeps.Release{bundledB, needsC},
	}, "2.0.0")
	require.Error(t, err, "a bundled release that requires a module nothing offers was planned")

	plan, err = moduledeps.CoreUpgrade(moduledeps.Request{
		Installed: []moduledeps.Installed{installed("c", "1.0.0")},
	}, "2.0.0")
	require.NoError(t, err)
	require.Empty(t, plan.Steps, "an upgrade that brings nothing and breaks nothing plans nothing")
}

func TestCoreBrokenFollowsRequiresUpward(t *testing.T) {
	t.Parallel()

	b := installed("b", "1.0.0")
	b.Core = "^1.0"
	a := installed("a", "1.0.0", "b", "^1")
	top := installed("top", "1.0.0", "a", "^1")
	free := installed("c", "1.0.0")

	require.Equal(t, []string{prefix + "a", prefix + "b", prefix + "top"},
		moduledeps.CoreBroken([]moduledeps.Installed{top, a, b, free}, "2.0.0"))
	require.Empty(t, moduledeps.CoreBroken([]moduledeps.Installed{top, a, b, free}, "1.5.0"))
}

func TestTheLockRoundTripsAndKnowsWhenItIsStale(t *testing.T) {
	t.Parallel()

	b := installed("b", "1.4.0")
	b.Pulled, b.Constraint = true, ""
	a := installed("a", "1.0.0", "b", "^1")
	settings := moduledeps.Settings{}

	lock := moduledeps.LockOf([]moduledeps.Installed{a, b}, settings)
	path := filepath.Join(t.TempDir(), moduledeps.LockName)
	require.NoError(t, moduledeps.WriteLock(path, lock))

	read, err := moduledeps.ReadLock(path)
	require.NoError(t, err)
	require.Equal(t, lock, read)
	require.True(t, read.Fresh([]moduledeps.Installed{a, b}, settings))
	require.NoError(t, read.Check(settings))

	// Another constraint of the operator makes the lock stale.
	asked := a
	asked.Constraint = "^1.1"
	require.False(t, read.Fresh([]moduledeps.Installed{asked, b}, settings))

	// A lock whose releases do not satisfy each other is refused.
	broken := moduledeps.LockOf([]moduledeps.Installed{installed("a", "1.0.0", "b", "^2"), b}, settings)
	require.ErrorIs(t, broken.Check(settings), moduledeps.ErrLockStale)

	missing, err := moduledeps.ReadLock(filepath.Join(t.TempDir(), "none.json"))
	require.NoError(t, err)
	require.Empty(t, missing.Modules)
}

func TestWhyWhyNotAndOutdated(t *testing.T) {
	t.Parallel()

	b := installed("b", "1.2.0")
	b.Pulled = true
	a := installed("a", "1.0.0", "b", "^1.2")
	all := []moduledeps.Installed{a, b}
	available := []moduledeps.Release{release("b", "1.4.0"), release("b", "2.0.0")}

	why := moduledeps.Why(all, prefix+"b")
	require.Len(t, why, 1)
	require.Equal(t, prefix+"a 1.0.0 requires "+prefix+"b ^1.2", why[0].Text)

	whyNot := moduledeps.WhyNot(all, available, prefix+"b", "2.0.0", moduledeps.Settings{})
	require.Len(t, whyNot, 1)
	require.Equal(t, prefix+"a", whyNot[0].By)

	require.Empty(t, moduledeps.WhyNot(all, available, prefix+"b", "1.4.0", moduledeps.Settings{}))

	rows := moduledeps.OutdatedOf(all, available, moduledeps.Settings{})
	require.Equal(t, "1.4.0", rows[1].Allowed)
	require.Equal(t, "2.0.0", rows[1].Latest)
	require.Equal(t, "update-possible", rows[1].Status)
}
